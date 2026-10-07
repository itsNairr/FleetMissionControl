package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"syscall"

	pb "github.com/itsnairr/fleet-telemetry-engine/internal/protocol"
	"github.com/itsnairr/fleet-telemetry-engine/internal/session"
	"github.com/itsnairr/fleet-telemetry-engine/internal/worker"
	"google.golang.org/protobuf/proto"
	"github.com/itsnairr/fleet-telemetry-engine/internal/twin"
	"github.com/itsnairr/fleet-telemetry-engine/internal/rules"
)

func handleConnection(conn net.Conn, pool *worker.TelemetryWorkerPool, registry *session.SessionRegistry) {
	defer conn.Close()

	var vin string

	defer func() {
		// If we identified the vehicle, clean up its socket when it disconnects
		if vin != "" {
			registry.Unregister(vin)
		}
	}()

	fmt.Printf("New vehicle connected: %s\n", conn.RemoteAddr().String())

	for {
		// Read the 4-byte header
		header := make([]byte, 4)
		_, err := io.ReadFull(conn, header)
		if err != nil {
			if errors.Is(err, io.EOF) {
				fmt.Println("Vehicle disconnected cleanly.")
			} else {
				fmt.Printf("Connection read error: %v\n", err)
			}
			return // Exit the goroutine when the vehicle disconnects
		}
		// Decode the 4 bytes into a uint32 integer (Big-Endian is network standard):
		messageLength := binary.BigEndian.Uint32(header)
		// Read EXACTLY that many bytes for the protobuf payload
		payload := make([]byte, messageLength)
		_, err = io.ReadFull(conn, payload)
		if err != nil {
			fmt.Printf("Failed to read payload: %v\n", err)
			return
		}

		// Unmarshal into Protobuf struct
		telemetry := &pb.VehicleTelemetry{}
		if err := proto.Unmarshal(payload, telemetry); err != nil {
			fmt.Printf("Failed to unmarshal protobuf: %v\n", err)
			continue
		}

		// register to hashmap if vin is empty (first connection)
		if vin == "" {
			vin = telemetry.GetVin()
			registry.Register(vin, conn)
		}
		pool.Enqueue(telemetry)
	}

}

func main() {
	sigChan := make(chan os.Signal, 1) //Used to catch the Ctrl+C or other termination signals
	//Buffer of 1 means that it can hold one signal without blocking the main thread. If we get a second signal before processing the first one, it would block. Not that it matters too much in this case, but it's good practice to know.

	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	twinRegistry := twin.NewDigitalTwinRegistry() //Twin Init to connect both the sim and the car OS
	rulesEngine := rules.NewRulesEngine() 
	pool := worker.NewTelemetryWorkerPool(10, 100, twinRegistry, rulesEngine)
	pool.Start()


	sessionRegistry := session.NewSessionRegistry()

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
			go handleConnection(conn, pool, sessionRegistry)
		}
	}()

	fmt.Println("Gateway running. Press Ctrl+C to shut down gracefully...")

	//Channel (frozen as there is not a val in sigChan)
	sig := <-sigChan //Main freezes until Ctrl+C
	fmt.Printf("\nReceived signal: %s. Initiating graceful shutdown...\n", sig)

	pool.Stop()

	fmt.Println("Shutdown complete. Exiting cleanly.")
}
