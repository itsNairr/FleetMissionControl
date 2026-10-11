package main

import (
	"context"
	"crypto/ecdsa"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/itsnairr/fleet-telemetry-engine/internal/crypto"
	pb "github.com/itsnairr/fleet-telemetry-engine/internal/protocol"
	"github.com/itsnairr/fleet-telemetry-engine/internal/simulator"
	"github.com/itsnairr/fleet-telemetry-engine/internal/wire"
	"google.golang.org/protobuf/proto"
)


func main() {
	numVehiclesFlag := flag.Int("n", 1, "Number of fleet vehicles to simulate")           //-n
	serverAddrFlag := flag.String("addr", "localhost:8080", "Gateway TCP server address") //-addr
	flag.Parse()

	count := *numVehiclesFlag
	if flag.NArg() > 0 {
		if parsed, err := strconv.Atoi(flag.Arg(0)); err == nil && parsed > 0 {
			count = parsed
		}
	}	
	
	// Load fleet public key burned into simulated vehicle ECU
	pubKey, err := crypto.LoadPublicKeyPEM("certs/fleet_public.pem")
	if err != nil {
		fmt.Printf("⚠️ Warning: Could not load certs/fleet_public.pem: %v\n", err)
		fmt.Println("Please run the Gateway first so it generates the fleet keys!")
		return
	}
	fmt.Print("🔑 Loaded fleet ECDSA public key. Ready to verify cloud commands.\n\n")




	fmt.Printf("🚗 Initializing EV Fleet Simulator: %d vehicles\n", count)
	fmt.Printf("📍 Telemetry Distribution: California Roads & Metros\n")
	fmt.Printf("📡 Connecting to Gateway: %s\n\n", *serverAddrFlag)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var wg sync.WaitGroup

	for i := 0; i < count; i++ {
		wg.Add(1)
		vin := fmt.Sprintf("SIM-%04d", i+1)

		// Probabilistic scenario assignment (mirroring real-world fleet distribution)
		scenario := simulator.SelectRandomScenario()
		// Select compatible vehicle model (EDV for delivery, pickups for towing/V2L, etc.)
		model := simulator.SelectCompatibleModel(scenario)

		vehicle := simulator.NewSimulatedVehicle(vin, model, scenario)

		// Subtle 5ms stagger to avoid TCP thundering herd on gateway connection accept
		time.Sleep(5 * time.Millisecond)
		go simulateVehicle(ctx, vehicle, *serverAddrFlag, &wg, pubKey)
	}

	<-ctx.Done()
	fmt.Println("\n🛑 Shutdown signal received. Disconnecting fleet cleanly...")
	wg.Wait()
	fmt.Println("All simulated vehicles stopped. Exiting.")

}

func simulateVehicle(ctx context.Context, v *simulator.SimulatedVehicle, serverAddr string, wg *sync.WaitGroup, pubKey *ecdsa.PublicKey) {
	defer wg.Done()

	backoff := 1 * time.Second
	maxBackoff := 10 * time.Second

	for {
		// Check if operator pressed Ctrl+C before trying to connect
		select {
		case <-ctx.Done():
			return
		default:
		}

		conn, err := net.Dial("tcp", serverAddr)
		if err != nil {
			fmt.Printf("[%s] 📡 Modem searching for gateway... retry in %v\n", v.Vin, backoff)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
				// Exponential backoff
				backoff *= 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
				continue
			}
		}

		// Connected, Reset backoff
		backoff = 1 * time.Second
		fmt.Printf("[%s] 📶 Connected to gateway\n", v.Vin)

		// Create a per-connection cancelable context:
		// If either the reader or writer socket breaks, both exit cleanly so the outer loop can reconnect.
		connCtx, cancelConn := context.WithCancel(ctx)
		// Both goroutines below write to this socket (telemetry + command acks), so they share one locked writer
		writer := wire.NewFrameWriter(conn)
		go func() {
			listenForCommands(connCtx, conn, writer, v, pubKey)
			cancelConn() // If socket reads fail/disconnect, cancel connection
		}()

		// Stream telemetry until socket breaks or Ctrl+C is pressed
		streamTelemetry(connCtx, writer, v)
		cancelConn()
		conn.Close()
	}
}

func streamTelemetry(ctx context.Context, writer *wire.FrameWriter, v *simulator.SimulatedVehicle) {
	// Initialize a 1 Hz ticker
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	// Loop blocks on ticker channel
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:

			//Advance vehicle physics & snapshot telemetry
			v.Tick()
			msg := v.ToTelemetry()

			// Serialize Protobuf + length-prefixed framing in one locked write
			if err := writer.WriteMessage(wire.MsgTelemetry, msg); err != nil {
				return
			}
		}
	}
}

// listenForCommands reads framed Protobuf commands from the Gateway, dispatches them to the vehicle ECU,
// and reports the outcome back so the operator sees accepted/rejected
func listenForCommands(ctx context.Context, conn net.Conn, writer *wire.FrameWriter, v *simulator.SimulatedVehicle, pubKey *ecdsa.PublicKey) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// 1. Read one length-prefixed, type-tagged frame
		msgType, payload, err := wire.ReadFrame(conn)
		if err != nil {
			return // Socket closed, EOF or corrupt stream
		}
		if msgType != wire.MsgCommand {
			continue
		}

		// 2. Unmarshal Protobuf command
		cmd := &pb.VehicleCommand{}
		if err := proto.Unmarshal(payload, cmd); err != nil {
			fmt.Printf("[%s] Malformed command protobuf: %v\n", v.Vin, err)
			continue
		}

		// 3. Verify signature, check replay window, enforce ISO-26262 safety interlocks & execute!
		resp := &pb.CommandResponse{
			CommandId: cmd.GetCommandId(),
			Vin:       v.Vin,
			Success:   true,
		}
		if err := v.ExecuteProtoCommand(cmd, pubKey); err != nil {
			fmt.Printf("[%s] ❌ Command rejected: %v\n", v.Vin, err)
			resp.Success = false
			resp.ErrorMessage = err.Error()
		} else {
			// Immediately push updated telemetry so state synchronizes with zero latency
			_ = writer.WriteMessage(wire.MsgTelemetry, v.ToTelemetry())
		}
		resp.TimestampMs = time.Now().UnixMilli()

		// 4. Acknowledge back to the gateway
		if err := writer.WriteMessage(wire.MsgCommandResponse, resp); err != nil {
			return
		}
	}
}
