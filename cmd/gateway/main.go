package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/itsnairr/fleet-telemetry-engine/internal/api"
	"github.com/itsnairr/fleet-telemetry-engine/internal/command"
	"github.com/itsnairr/fleet-telemetry-engine/internal/crypto"
	pb "github.com/itsnairr/fleet-telemetry-engine/internal/protocol"
	"github.com/itsnairr/fleet-telemetry-engine/internal/pubsub"
	"github.com/itsnairr/fleet-telemetry-engine/internal/rules"
	"github.com/itsnairr/fleet-telemetry-engine/internal/session"
	"github.com/itsnairr/fleet-telemetry-engine/internal/twin"
	"github.com/itsnairr/fleet-telemetry-engine/internal/wire"
	"github.com/itsnairr/fleet-telemetry-engine/internal/worker"
	"google.golang.org/protobuf/proto"
)

// gateway bundles the shared services every vehicle connection needs
type gateway struct {
	pool       *worker.TelemetryWorkerPool
	sessions   *session.SessionRegistry
	twins      *twin.DigitalTwinRegistry
	dispatcher *command.CommandDispatcher

	// Every open vehicle socket, so shutdown can close them and wait for their goroutines to exit
	connMu   sync.Mutex
	conns    map[net.Conn]struct{}
	connsWg  sync.WaitGroup
	shutdown bool // Set once closeAllConnections starts, late arrivals are turned away
}

// trackConnection registers a new socket. Returns false if the gateway is already shutting down.
func (g *gateway) trackConnection(conn net.Conn) bool {
	g.connMu.Lock()
	defer g.connMu.Unlock()
	if g.shutdown {
		return false
	}
	g.conns[conn] = struct{}{}
	g.connsWg.Add(1)
	return true
}

func (g *gateway) untrackConnection(conn net.Conn) {
	g.connMu.Lock()
	defer g.connMu.Unlock()
	delete(g.conns, conn)
	g.connsWg.Done()
}

// closeAllConnections unblocks every handleConnection read loop and waits until they have all returned.
// Must run before pool.Stop(), otherwise a handler could Enqueue onto the closed job queue and panic.
func (g *gateway) closeAllConnections() {
	g.connMu.Lock()
	g.shutdown = true
	for conn := range g.conns {
		conn.Close()
	}
	g.connMu.Unlock()
	g.connsWg.Wait()
}

func (g *gateway) handleConnection(conn net.Conn) {
	defer g.untrackConnection(conn)
	defer conn.Close()

	var vin string

	defer func() {
		// If we identified the vehicle, clean up its socket when it disconnects.
		// Only mark offline if this was still the car's current socket (it may have already reconnected).
		if vin != "" && g.sessions.Unregister(vin, conn) {
			g.twins.SetOnline(vin, false)
		}
	}()

	fmt.Printf("[Gateway] Vehicle connected from %s.\n", conn.RemoteAddr().String())

	for {
		// Read one length-prefixed, type-tagged frame
		msgType, payload, err := wire.ReadFrame(conn)
		if err != nil {
			if errors.Is(err, io.EOF) {
				fmt.Println("[Gateway] Vehicle disconnected.")
			} else {
				fmt.Printf("[Gateway] Connection read error: %v\n", err)
			}
			return // Exit the goroutine when the vehicle disconnects
		}

		switch msgType {
		case wire.MsgTelemetry:
			// Unmarshal into Protobuf struct
			telemetry := &pb.VehicleTelemetry{}
			if err := proto.Unmarshal(payload, telemetry); err != nil {
				fmt.Printf("[Gateway] Telemetry unmarshal error: %v\n", err)
				continue
			}

			// register to hashmap if vin is empty (first frame on this connection)
			if vin == "" {
				vin = telemetry.GetVin()
				g.sessions.Register(vin, conn)
				g.twins.SetOnline(vin, true)
			}
			g.pool.Enqueue(telemetry)

		case wire.MsgCommandResponse:
			if vin == "" {
				continue // Vehicle hasn't identified itself yet
			}
			resp := &pb.CommandResponse{}
			if err := proto.Unmarshal(payload, resp); err != nil {
				fmt.Printf("[Gateway] Command response unmarshal error: %v\n", err)
				continue
			}
			g.dispatcher.HandleResponse(vin, resp)

		default:
			fmt.Printf("[Gateway] Unknown frame type %d from %s ignored.\n", msgType, conn.RemoteAddr())
		}
	}

}

func main() {
	sigChan := make(chan os.Signal, 1) //Used to catch the Ctrl+C or other termination signals
	//Buffer of 1 means that it can hold one signal without blocking the main thread. If we get a second signal before processing the first one, it would block. Not that it matters too much in this case, but it's good practice to know.

	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	twinRegistry := twin.NewDigitalTwinRegistry() //Twin Init to connect both the sim and the car OS
	rulesEngine := rules.NewRulesEngine() 
	alertBroker := pubsub.NewBroker[*pb.VehicleAlert](64) //Dashboards subscribe here for live alerts
	pool := worker.NewTelemetryWorkerPool(10, 100, twinRegistry, rulesEngine, alertBroker)
	pool.Start()


	sessionRegistry := session.NewSessionRegistry()


	//Initialize Root of Trust / Fleet ECDSA Keypair
	privKey, _, err := crypto.EnsureFleetKeys("certs")
	if err != nil {
		fmt.Printf("[Gateway] ERROR: Failed to initialize fleet cryptographic keys: %v\n", err)
		return
	}

	//Initialize Command Dispatcher with Gateway Private Key & Session Registry
	dispatcher := command.NewCommandDispatcher(privKey, sessionRegistry)

	gw := &gateway{
		pool:       pool,
		sessions:   sessionRegistry,
		twins:      twinRegistry,
		dispatcher: dispatcher,
		conns:      make(map[net.Conn]struct{}),
	}

	//HTTP + WebSocket API for the Mission Control dashboard
	apiServer := api.NewServer(api.Config{
		Addr:           envOr("FLEET_API_ADDR", "localhost:8081"),
		AllowedOrigins: strings.Split(envOr("FLEET_ALLOWED_ORIGINS", "http://localhost:5173"), ","),
	}, twinRegistry, dispatcher, alertBroker)

	go func() {
		if err := apiServer.ListenAndServe(); err != nil {
			fmt.Printf("[Gateway] API server error: %v\n", err)
		}
	}()


	//Setup TCP connection
	listener, err := net.Listen("tcp", ":8080")
	if err != nil {
		fmt.Printf("[Gateway] ERROR: Failed to bind TCP listener to port 8080: %v\n", err)
		return
	}

	fmt.Println("[Gateway] TCP server listening on port 8080.")

	go func() { //goroutine to not run on main thread
		for {
			conn, err := listener.Accept()
			if err != nil {
				// When we shutdown, listener.Close() is called, which causes Accept() to return net.ErrClosed.
				// This is a normal, clean shutdown—not a crash.
				if errors.Is(err, net.ErrClosed) {
					fmt.Println("[Gateway] TCP listener closed.")
					return
				}
				fmt.Printf("[Gateway] TCP accept error: %v\n", err)
				continue
			}

			// Handle this specific vehicle concurrently without blocking other cars
			if !gw.trackConnection(conn) {
				conn.Close() // Arrived mid-shutdown
				continue
			}
			go gw.handleConnection(conn)
		}
	}()

	fmt.Println("[Gateway] Server running. Send SIGINT or SIGTERM to stop.")

	//Channel (frozen as there is not a val in sigChan)
	sig := <-sigChan //Main freezes until Ctrl+C
	fmt.Printf("\n[Gateway] Received signal: %s. Initiating shutdown.\n", sig)

	// Shut down from the outside in: dashboards first, then vehicles, then the workers they feed
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := apiServer.Shutdown(shutdownCtx); err != nil {
		fmt.Printf("[Gateway] API shutdown error: %v\n", err)
	}

	listener.Close()           // Stop accepting new vehicles
	gw.closeAllConnections()   // Disconnect existing ones and wait for their handlers to exit
	pool.Stop()                // Now nothing can Enqueue, so draining the queue is safe

	fmt.Println("[Gateway] Shutdown complete.")
}

// envOr reads an environment variable, falling back to def when unset
func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
