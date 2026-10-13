package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"net/http"
	"sort"
	"testing"
	"time"

	"github.com/itsnairr/fleet-telemetry-engine/internal/command"
	"github.com/itsnairr/fleet-telemetry-engine/internal/crypto"
	pb "github.com/itsnairr/fleet-telemetry-engine/internal/protocol"
	"github.com/itsnairr/fleet-telemetry-engine/internal/pubsub"
	"github.com/itsnairr/fleet-telemetry-engine/internal/rules"
	"github.com/itsnairr/fleet-telemetry-engine/internal/session"
	"github.com/itsnairr/fleet-telemetry-engine/internal/simulator"
	"github.com/itsnairr/fleet-telemetry-engine/internal/twin"
	"github.com/itsnairr/fleet-telemetry-engine/internal/wire"
	"github.com/itsnairr/fleet-telemetry-engine/internal/worker"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// -----------------------------------------------------------------------------
// testing.B Benchmarks
// -----------------------------------------------------------------------------

func benchmarkIngestionWrapper(b *testing.B, n int) {
	twinRegistry := twin.NewDigitalTwinRegistry()
	rulesEngine := rules.NewRulesEngine()
	alertBroker := pubsub.NewBroker[*pb.VehicleAlert](64)
	pool := worker.NewTelemetryWorkerPool(10, 100, twinRegistry, rulesEngine, alertBroker)
	pool.Quiet = true
	pool.Start()
	defer pool.Stop()

	sessionRegistry := session.NewSessionRegistry()
	sessionRegistry.Quiet = true

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatalf("failed to listen: %v", err)
	}
	defer listener.Close()

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				var vin string
				defer func() {
					if vin != "" && sessionRegistry.Unregister(vin, c) {
						twinRegistry.SetOnline(vin, false)
					}
					c.Close()
				}()
				for {
					msgType, payload, err := wire.ReadFrame(c)
					if err != nil {
						return
					}
					if msgType == wire.MsgTelemetry {
						telemetry := &pb.VehicleTelemetry{}
						if err := proto.Unmarshal(payload, telemetry); err != nil {
							continue
						}
						if vin == "" {
							vin = telemetry.GetVin()
							sessionRegistry.Register(vin, c)
							twinRegistry.SetOnline(vin, true)
						}
						pool.Enqueue(telemetry)
					}
				}
			}(conn)
		}
	}()

	type vehicleClient struct {
		vehicle *simulator.SimulatedVehicle
		writer  *wire.FrameWriter
		conn    net.Conn
	}
	clients := make([]*vehicleClient, n)
	for i := 0; i < n; i++ {
		vin := fmt.Sprintf("BENCH-%04d", i+1)
		v := simulator.NewSimulatedVehicle(vin, pb.VehicleModel_VEHICLE_MODEL_RIVIAN_R1T, simulator.ScenarioHighwayCruising)
		v.Quiet = true
		conn, err := net.Dial("tcp", listener.Addr().String())
		if err != nil {
			b.Fatalf("dial failed: %v", err)
		}
		clients[i] = &vehicleClient{
			vehicle: v,
			writer:  wire.NewFrameWriter(conn),
			conn:    conn,
		}
	}
	defer func() {
		for _, c := range clients {
			c.conn.Close()
		}
	}()

	// Warmup
	for _, c := range clients {
		c.vehicle.Tick()
		_ = c.writer.WriteMessage(wire.MsgTelemetry, c.vehicle.ToTelemetry())
	}
	time.Sleep(50 * time.Millisecond)

	b.ResetTimer()
	start := time.Now()
	var totalSent uint64
	var maxDepth int

	for i := 0; i < b.N; i++ {
		tickStart := time.Now()
		for _, c := range clients {
			c.vehicle.Tick()
			msg := c.vehicle.ToTelemetry()
			if err := c.writer.WriteMessage(wire.MsgTelemetry, msg); err == nil {
				totalSent++
			}
		}
		if d := pool.QueueDepth(); d > maxDepth {
			maxDepth = d
		}
		elapsedTick := time.Since(tickStart)
		if elapsedTick < time.Second {
			time.Sleep(time.Second - elapsedTick)
		}
	}
	b.StopTimer()
	elapsed := time.Since(start)

	time.Sleep(50 * time.Millisecond)
	msgsPerSec := float64(totalSent) / elapsed.Seconds()
	dropped := pool.Dropped()

	b.ReportMetric(msgsPerSec, "msgs/sec")
	b.ReportMetric(float64(maxDepth), "max_qdepth")
	b.ReportMetric(float64(dropped), "dropped_msgs")
}

func BenchmarkIngestionThroughput_N10(b *testing.B)  { benchmarkIngestionWrapper(b, 10) }
func BenchmarkIngestionThroughput_N50(b *testing.B)  { benchmarkIngestionWrapper(b, 50) }
func BenchmarkIngestionThroughput_N100(b *testing.B) { benchmarkIngestionWrapper(b, 100) }
func BenchmarkIngestionThroughput_N200(b *testing.B) { benchmarkIngestionWrapper(b, 200) }

func BenchmarkCommandRoundTripLatency(b *testing.B) {
	privKey, pubKey, err := crypto.GenerateKeyPair()
	if err != nil {
		b.Fatalf("keygen failed: %v", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatalf("listen failed: %v", err)
	}
	defer listener.Close()

	sessions := session.NewSessionRegistry()
	sessions.Quiet = true
	dispatcher := command.NewCommandDispatcher(privKey, sessions)
	dispatcher.Quiet = true

	const vin = "BENCH-CMD-01"
	vehicle := simulator.NewSimulatedVehicle(vin, pb.VehicleModel_VEHICLE_MODEL_RIVIAN_R1T, simulator.ScenarioParkedSentry)
	vehicle.Quiet = true

	var gwConn net.Conn
	var connErr error
	acceptDone := make(chan struct{})
	go func() {
		gwConn, connErr = listener.Accept()
		close(acceptDone)
	}()

	clientConn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		b.Fatalf("dial failed: %v", err)
	}
	defer clientConn.Close()
	<-acceptDone
	if connErr != nil {
		b.Fatalf("accept failed: %v", connErr)
	}
	defer gwConn.Close()

	sessions.Register(vin, gwConn)

	vehicleWriter := wire.NewFrameWriter(clientConn)
	go func() {
		for {
			msgType, payload, err := wire.ReadFrame(clientConn)
			if err != nil {
				return
			}
			if msgType == wire.MsgCommand {
				cmd := &pb.VehicleCommand{}
				if err := proto.Unmarshal(payload, cmd); err == nil {
					resp := &pb.CommandResponse{
						CommandId:   cmd.GetCommandId(),
						Vin:         vin,
						Success:     true,
						TimestampMs: time.Now().UnixMilli(),
					}
					if err := vehicle.ExecuteProtoCommand(cmd, pubKey); err != nil {
						resp.Success = false
						resp.ErrorMessage = err.Error()
					}
					_ = vehicleWriter.WriteMessage(wire.MsgCommandResponse, resp)
				}
			}
		}
	}()

	go func() {
		for {
			msgType, payload, err := wire.ReadFrame(gwConn)
			if err != nil {
				return
			}
			if msgType == wire.MsgCommandResponse {
				resp := &pb.CommandResponse{}
				if err := proto.Unmarshal(payload, resp); err == nil {
					dispatcher.HandleResponse(vin, resp)
				}
			}
		}
	}()

	warmCtx, warmCancel := context.WithTimeout(context.Background(), 2*time.Second)
	_, _ = dispatcher.DispatchAndWait(warmCtx, vin, pb.CommandType_COMMAND_TYPE_LOCK_DOORS, 0)
	warmCancel()

	durations := make([]time.Duration, 0, b.N)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cmdCtx, cmdCancel := context.WithTimeout(context.Background(), 2*time.Second)
		start := highResTime()
		resp, err := dispatcher.DispatchAndWait(cmdCtx, vin, pb.CommandType_COMMAND_TYPE_LOCK_DOORS, 0)
		dur := highResDuration(start, highResTime())
		cmdCancel()

		if err != nil || !resp.GetSuccess() {
			b.Fatalf("dispatch failed: %v (resp=%v)", err, resp)
		}
		durations = append(durations, dur)
	}
	b.StopTimer()

	if len(durations) > 0 {
		sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
		n := len(durations)
		p50 := durations[n*50/100]
		p90 := durations[n*90/100]
		p99 := durations[n*99/100]

		p50Ms := float64(p50.Nanoseconds()) / 1e6
		p90Ms := float64(p90.Nanoseconds()) / 1e6
		p99Ms := float64(p99.Nanoseconds()) / 1e6

		b.ReportMetric(p50Ms, "p50_ms")
		b.ReportMetric(p90Ms, "p90_ms")
		b.ReportMetric(p99Ms, "p99_ms")
	}
}

func BenchmarkPayloadEfficiency_Protobuf(b *testing.B) {
	v := simulator.NewSimulatedVehicle("SIM-0001", pb.VehicleModel_VEHICLE_MODEL_RIVIAN_R1T, simulator.ScenarioHighwayCruising)
	v.Tick()
	msg := v.ToTelemetry()
	metrics := measurePayload()

	b.SetBytes(int64(metrics.ProtoWireBytes))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		payload, _ := proto.Marshal(msg)
		frame := make([]byte, 5+len(payload))
		binary.BigEndian.PutUint32(frame[:4], uint32(1+len(payload)))
		frame[4] = byte(wire.MsgTelemetry)
		copy(frame[5:], payload)
	}
}

func BenchmarkPayloadEfficiency_JSONOverHTTP(b *testing.B) {
	v := simulator.NewSimulatedVehicle("SIM-0001", pb.VehicleModel_VEHICLE_MODEL_RIVIAN_R1T, simulator.ScenarioHighwayCruising)
	v.Tick()
	msg := v.ToTelemetry()
	metrics := measurePayload()

	b.SetBytes(int64(metrics.HTTPWireBytes))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		jsonPayload, _ := protojson.Marshal(msg)
		req, _ := http.NewRequest("POST", "http://gateway.fleet.internal:8080/api/v1/telemetry", bytes.NewReader(jsonPayload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "FleetVehicleTelemetry/1.0")
		req.Header.Set("Connection", "keep-alive")
		req.ContentLength = int64(len(jsonPayload))
		var buf bytes.Buffer
		_ = req.Write(&buf)
	}
}

func TestPayloadEfficiencyMeasurement(t *testing.T) {
	metrics := measurePayload()

	t.Logf("=== PAYLOAD EFFICIENCY MEASUREMENT ===")
	t.Logf("Protobuf Wire Bytes (5-byte frame + payload): %d bytes", metrics.ProtoWireBytes)
	t.Logf("JSON Body Bytes:                              %d bytes", metrics.JSONBodyBytes)
	t.Logf("JSON over HTTP Total Wire Bytes:              %d bytes", metrics.HTTPWireBytes)
	t.Logf("Measured Ratio (HTTP+JSON / Protobuf):        %.2fx", metrics.Ratio)
	t.Logf("Wire Size Reduction:                          %.1f%%", metrics.ReductionPct)
}
