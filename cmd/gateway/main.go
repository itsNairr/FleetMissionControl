package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"syscall"

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
}

func (g *gateway) handleConnection(conn net.Conn) {
	defer conn.Close()

	var vin string

	defer func() {
		// If we identified the vehicle, clean up its socket when it disconnects.
		// Only mark offline if this was still the car's current socket (it may have already reconnected).
		if vin != "" && g.sessions.Unregister(vin, conn) {
			g.twins.SetOnline(vin, false)
		}
	}()

	fmt.Printf("New vehicle connected: %s\n", conn.RemoteAddr().String())

	for {
		// Read one length-prefixed, type-tagged frame
		msgType, payload, err := wire.ReadFrame(conn)
		if err != nil {
			if errors.Is(err, io.EOF) {
				fmt.Println("Vehicle disconnected cleanly.")
			} else {
				fmt.Printf("Connection read error: %v\n", err)
			}
			return // Exit the goroutine when the vehicle disconnects
		}

		switch msgType {
		case wire.MsgTelemetry:
			// Unmarshal into Protobuf struct
			telemetry := &pb.VehicleTelemetry{}
			if err := proto.Unmarshal(payload, telemetry); err != nil {
				fmt.Printf("Failed to unmarshal telemetry: %v\n", err)
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
				fmt.Printf("Failed to unmarshal command response: %v\n", err)
				continue
			}
			g.dispatcher.HandleResponse(vin, resp)

		default:
			fmt.Printf("Ignoring unknown frame type %d from %s\n", msgType, conn.RemoteAddr())
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
		fmt.Printf("Fatal: failed to initialize fleet crypto keys: %v\n", err)
		return
	}

	//Initialize Command Dispatcher with Gateway Private Key & Session Registry
	dispatcher := command.NewCommandDispatcher(privKey, sessionRegistry)

	gw := &gateway{
		pool:       pool,
		sessions:   sessionRegistry,
		twins:      twinRegistry,
		dispatcher: dispatcher,
	}


	//Setup TCP connection
	listener, err := net.Listen("tcp", ":8080")
	if err != nil {
		fmt.Printf("Failed to bind to port 8080: %v\n", err)
		return
	}

	defer listener.Close() //Close at the end
	fmt.Println("Gateway TCP server listening on :8080...")

	go func() { //goroutine to not run on main thread
		for {
			conn, err := listener.Accept()
			if err != nil {
				// When we shutdown, listener.Close() is called, which causes Accept() to return net.ErrClosed.
				// This is a normal, clean shutdown—not a crash.
				if errors.Is(err, net.ErrClosed) {
					fmt.Println("TCP listener closed cleanly.")
					return
				}
				fmt.Printf("Accept error: %v\n", err)
				continue
			}

			// Handle this specific vehicle concurrently without blocking other cars
			go gw.handleConnection(conn)
		}
	}()

	fmt.Println("Gateway running. Press Ctrl+C to shut down gracefully...")

	//Channel (frozen as there is not a val in sigChan)
	sig := <-sigChan //Main freezes until Ctrl+C
	fmt.Printf("\nReceived signal: %s. Initiating graceful shutdown...\n", sig)

	pool.Stop()

	fmt.Println("Shutdown complete. Exiting cleanly.")
}
