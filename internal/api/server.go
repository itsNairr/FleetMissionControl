package api

// HTTP + WebSocket gateway for the Mission Control dashboard.
//
//	GET  /api/v1/vehicles               Snapshot of every known vehicle (online + offline)
//	GET  /api/v1/vehicles/{vin}         Snapshot of one vehicle
//	WS   /api/v1/vehicles/{vin}/stream  Live pushes whenever that vehicle's twin changes
//	POST /api/v1/vehicles/{vin}/command Sign + send a command, wait for the vehicle's verdict
//	WS   /api/v1/fleet/stream           Live pushes for every vehicle (drives the fleet map)
//	WS   /api/v1/alerts/stream          Live feed of rules-engine alerts for the whole fleet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/itsnairr/fleet-telemetry-engine/internal/command"
	pb "github.com/itsnairr/fleet-telemetry-engine/internal/protocol"
	"github.com/itsnairr/fleet-telemetry-engine/internal/pubsub"
	"github.com/itsnairr/fleet-telemetry-engine/internal/twin"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

const (
	commandTimeout = 3 * time.Second // How long POST /command waits for the vehicle to ack
	wsWriteTimeout = 5 * time.Second // Drop a WebSocket client that can't accept a message in time
	maxCommandBody = 4 << 10         // 4 KiB is plenty for a command request
)

// CommandSender is the slice of the dispatcher the API needs (an interface so tests can fake it)
type CommandSender interface {
	DispatchAndWait(ctx context.Context, vin string, cmdType pb.CommandType, speedLimitMph float64) (*pb.CommandResponse, error)
}

type Config struct {
	Addr           string   // e.g. "localhost:8081"
	AllowedOrigins []string // Browser origins allowed to call the API, e.g. "http://localhost:5173"
}

type Server struct {
	twins    *twin.DigitalTwinRegistry
	commands CommandSender
	alerts   *pubsub.Broker[*pb.VehicleAlert]

	allowedOrigins map[string]bool
	wsOrigins      []string // host[:port] patterns for the WebSocket origin check

	httpServer *http.Server

	// WebSocket handlers watch shutdownCtx: http.Server.Shutdown doesn't wait for or close upgraded connections
	shutdownCtx context.Context
	cancel      context.CancelFunc
	wsWg        sync.WaitGroup
}

func NewServer(cfg Config, twins *twin.DigitalTwinRegistry, commands CommandSender, alerts *pubsub.Broker[*pb.VehicleAlert]) *Server {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{
		twins:          twins,
		commands:       commands,
		alerts:         alerts,
		allowedOrigins: make(map[string]bool),
		shutdownCtx:    ctx,
		cancel:         cancel,
	}

	for _, origin := range cfg.AllowedOrigins {
		s.allowedOrigins[origin] = true
		if u, err := url.Parse(origin); err == nil && u.Host != "" {
			s.wsOrigins = append(s.wsOrigins, u.Host)
		}
	}

	s.httpServer = &http.Server{
		Addr:              cfg.Addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	return s
}

// Handler builds the router. Exposed separately so tests can serve it with httptest.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/vehicles", s.handleListVehicles)
	mux.HandleFunc("GET /api/v1/vehicles/{vin}", s.handleGetVehicle)
	mux.HandleFunc("GET /api/v1/vehicles/{vin}/stream", s.handleVehicleStream)
	mux.HandleFunc("POST /api/v1/vehicles/{vin}/command", s.handleCommand)
	mux.HandleFunc("GET /api/v1/fleet/stream", s.handleFleetStream)
	mux.HandleFunc("GET /api/v1/alerts/stream", s.handleAlertStream)
	return s.withCORS(mux)
}

// ListenAndServe blocks until the server stops. Returns nil after a clean Shutdown.
func (s *Server) ListenAndServe() error {
	fmt.Printf("[API] Server listening on http://%s/api/v1\n", s.httpServer.Addr)
	if err := s.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Shutdown stops accepting requests, closes live WebSockets and waits for in-flight requests to finish
func (s *Server) Shutdown(ctx context.Context) error {
	s.cancel() // Tell every WebSocket loop to say goodbye and exit
	err := s.httpServer.Shutdown(ctx)
	s.wsWg.Wait()
	return err
}

// ---------- REST ----------

// vehicleJSON is the wire shape the dashboard receives for one vehicle
type vehicleJSON struct {
	Vin       string          `json:"vin"`
	Online    bool            `json:"online"`
	Telemetry json.RawMessage `json:"telemetry"`
}

// protobuf -> JSON: camelCase field names, enums as strings, and zero values included
// (otherwise `frunkOpen: false` would be omitted entirely, which is awkward for a UI)
var protoJSON = protojson.MarshalOptions{EmitUnpopulated: true}

func toVehicleJSON(snap twin.Snapshot) (vehicleJSON, error) {
	telemetry, err := protoJSON.Marshal(snap.Telemetry)
	if err != nil {
		return vehicleJSON{}, err
	}
	return vehicleJSON{Vin: snap.Vin, Online: snap.Online, Telemetry: telemetry}, nil
}

func (s *Server) handleListVehicles(w http.ResponseWriter, r *http.Request) {
	snaps := s.twins.Snapshots()
	vehicles := make([]vehicleJSON, 0, len(snaps))
	for _, snap := range snaps {
		v, err := toVehicleJSON(snap)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to encode telemetry")
			return
		}
		vehicles = append(vehicles, v)
	}
	writeJSON(w, http.StatusOK, vehicles)
}

func (s *Server) handleGetVehicle(w http.ResponseWriter, r *http.Request) {
	snap, ok := s.twins.GetSnapshot(r.PathValue("vin"))
	if !ok {
		writeError(w, http.StatusNotFound, "unknown vehicle")
		return
	}
	v, err := toVehicleJSON(snap)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode telemetry")
		return
	}
	writeJSON(w, http.StatusOK, v)
}

type commandRequest struct {
	Type          string  `json:"type"`            // "OPEN_FRUNK" or "COMMAND_TYPE_OPEN_FRUNK"
	SpeedLimitMph float64 `json:"speed_limit_mph"` // Only for SET_SPEED_LIMIT
}

type commandResult struct {
	CommandID string `json:"command_id"`
	Success   bool   `json:"success"`
	Error     string `json:"error,omitempty"`
}

// parseCommandType accepts the short ("UNLOCK_DOORS") or full ("COMMAND_TYPE_UNLOCK_DOORS") enum name
func parseCommandType(name string) (pb.CommandType, bool) {
	name = strings.ToUpper(strings.TrimSpace(name))
	if !strings.HasPrefix(name, "COMMAND_TYPE_") {
		name = "COMMAND_TYPE_" + name
	}
	value, ok := pb.CommandType_value[name]
	if !ok || pb.CommandType(value) == pb.CommandType_COMMAND_TYPE_UNSPECIFIED {
		return 0, false
	}
	return pb.CommandType(value), true
}

func (s *Server) handleCommand(w http.ResponseWriter, r *http.Request) {
	vin := r.PathValue("vin")

	var req commandRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxCommandBody)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "body must be JSON like {\"type\": \"UNLOCK_DOORS\"}")
		return
	}

	cmdType, ok := parseCommandType(req.Type)
	if !ok {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown command type %q", req.Type))
		return
	}
	if cmdType == pb.CommandType_COMMAND_TYPE_SET_SPEED_LIMIT && req.SpeedLimitMph <= 0 {
		writeError(w, http.StatusBadRequest, "SET_SPEED_LIMIT requires a positive speed_limit_mph")
		return
	}

	// r.Context() is cancelled if the browser gives up, so we stop waiting too
	ctx, cancel := context.WithTimeout(r.Context(), commandTimeout)
	defer cancel()

	resp, err := s.commands.DispatchAndWait(ctx, vin, cmdType, req.SpeedLimitMph)
	switch {
	case errors.Is(err, command.ErrVehicleOffline):
		writeError(w, http.StatusServiceUnavailable, err.Error())
	case errors.Is(err, context.DeadlineExceeded):
		writeError(w, http.StatusGatewayTimeout, err.Error())
	case err != nil:
		writeError(w, http.StatusBadGateway, err.Error())
	default:
		// The vehicle answered. A safety/signature rejection is still a successful round trip,
		// so it's 200 with success=false and the vehicle's reason.
		writeJSON(w, http.StatusOK, commandResult{
			CommandID: resp.GetCommandId(),
			Success:   resp.GetSuccess(),
			Error:     resp.GetErrorMessage(),
		})
	}
}

// ---------- WebSockets ----------

func (s *Server) handleVehicleStream(w http.ResponseWriter, r *http.Request) {
	vin := r.PathValue("vin")
	if _, ok := s.twins.GetSnapshot(vin); !ok {
		writeError(w, http.StatusNotFound, "unknown vehicle")
		return
	}
	s.streamSnapshots(w, r, func(snap twin.Snapshot) bool { return snap.Vin == vin })
}

func (s *Server) handleFleetStream(w http.ResponseWriter, r *http.Request) {
	s.streamSnapshots(w, r, func(twin.Snapshot) bool { return true })
}

// streamSnapshots sends the current state of every matching vehicle, then pushes each matching change
func (s *Server) streamSnapshots(w http.ResponseWriter, r *http.Request, match func(twin.Snapshot) bool) {
	// Subscribe BEFORE reading the current state so no update can slip through the gap
	updates, unsubscribe := s.twins.Updates().Subscribe()
	defer unsubscribe()

	s.serveWebSocket(w, r, func(ctx context.Context, conn *websocket.Conn) error {
		// Send the current state immediately so the UI doesn't wait up to a second for the first frame
		for _, snap := range s.twins.Snapshots() {
			if match(snap) {
				if err := writeSnapshot(ctx, conn, snap); err != nil {
					return err
				}
			}
		}
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case snap, ok := <-updates:
				if !ok {
					return nil
				}
				if !match(snap) {
					continue // The feed carries the whole fleet; this client may only want one car
				}
				if err := writeSnapshot(ctx, conn, snap); err != nil {
					return err
				}
			}
		}
	})
}

func (s *Server) handleAlertStream(w http.ResponseWriter, r *http.Request) {
	alerts, unsubscribe := s.alerts.Subscribe()
	defer unsubscribe()

	s.serveWebSocket(w, r, func(ctx context.Context, conn *websocket.Conn) error {
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case alert, ok := <-alerts:
				if !ok {
					return nil
				}
				if err := writeProto(ctx, conn, alert); err != nil {
					return err
				}
			}
		}
	})
}

// serveWebSocket upgrades the request and runs pump until the client leaves or the server shuts down
func (s *Server) serveWebSocket(w http.ResponseWriter, r *http.Request, pump func(ctx context.Context, conn *websocket.Conn) error) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: s.wsOrigins})
	if err != nil {
		return // Accept already wrote the HTTP error (e.g. disallowed origin)
	}
	defer conn.CloseNow()

	s.wsWg.Add(1)
	defer s.wsWg.Done()

	// These streams are server -> client only. CloseRead discards anything the client sends and
	// gives us a context that is cancelled the moment the client disconnects.
	ctx := conn.CloseRead(r.Context())

	// Also stop when the whole server is shutting down
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		select {
		case <-s.shutdownCtx.Done():
			cancel()
		case <-ctx.Done():
		}
	}()

	pump(ctx, conn)

	if s.shutdownCtx.Err() != nil {
		conn.Close(websocket.StatusGoingAway, "server shutting down")
	}
}

func writeSnapshot(ctx context.Context, conn *websocket.Conn, snap twin.Snapshot) error {
	v, err := toVehicleJSON(snap)
	if err != nil {
		return err
	}
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return writeText(ctx, conn, data)
}

func writeProto(ctx context.Context, conn *websocket.Conn, msg proto.Message) error {
	data, err := protoJSON.Marshal(msg)
	if err != nil {
		return err
	}
	return writeText(ctx, conn, data)
}

func writeText(ctx context.Context, conn *websocket.Conn, data []byte) error {
	ctx, cancel := context.WithTimeout(ctx, wsWriteTimeout)
	defer cancel()
	return conn.Write(ctx, websocket.MessageText, data)
}

// ---------- Helpers ----------

// withCORS lets the Vite dev server (a different origin) call the API from the browser
func (s *Server) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); s.allowedOrigins[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.Header().Add("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent) // CORS preflight
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
