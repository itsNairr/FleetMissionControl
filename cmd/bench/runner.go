package main

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

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
// High-Resolution Timer for Windows
// -----------------------------------------------------------------------------

var (
	qpcProc *syscall.LazyProc
	qpfFreq int64
)

func init() {
	if runtime.GOOS == "windows" {
		k32 := syscall.NewLazyDLL("kernel32.dll")
		qpcProc = k32.NewProc("QueryPerformanceCounter")
		qpfProc := k32.NewProc("QueryPerformanceFrequency")
		var freq int64
		qpfProc.Call(uintptr(unsafe.Pointer(&freq)))
		qpfFreq = freq
	}
}

func highResTime() int64 {
	if qpcProc != nil && qpfFreq > 0 {
		var count int64
		qpcProc.Call(uintptr(unsafe.Pointer(&count)))
		return count
	}
	return time.Now().UnixNano()
}

func highResDuration(start, end int64) time.Duration {
	if qpcProc != nil && qpfFreq > 0 {
		return time.Duration((end - start) * int64(time.Second) / qpfFreq)
	}
	return time.Duration(end - start)
}

// -----------------------------------------------------------------------------
// Ingestion Benchmark Pipeline (1 Hz Fleet Simulation at Scale)
// -----------------------------------------------------------------------------

type IngestionResult struct {
	NumVehicles     int
	Duration        time.Duration
	TotalMessages   uint64
	MessagesPerSec  float64
	MaxQueueDepth   int
	DroppedMessages uint64
}

func runIngestion1Hz(numVehicles int, duration time.Duration) IngestionResult {
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
		panic(fmt.Sprintf("failed to listen on TCP: %v", err))
	}
	defer listener.Close()

	var connWg sync.WaitGroup
	var activeConnsMu sync.Mutex
	activeConns := make([]net.Conn, 0, numVehicles)

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			activeConnsMu.Lock()
			activeConns = append(activeConns, conn)
			activeConnsMu.Unlock()

			connWg.Add(1)
			go func(c net.Conn) {
				defer connWg.Done()
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
	clients := make([]*vehicleClient, numVehicles)
	for i := 0; i < numVehicles; i++ {
		vin := fmt.Sprintf("BENCH-%04d", i+1)
		scenario := simulator.ScenarioHighwayCruising
		model := pb.VehicleModel_VEHICLE_MODEL_RIVIAN_R1T
		v := simulator.NewSimulatedVehicle(vin, model, scenario)
		v.Quiet = true

		conn, err := net.Dial("tcp", listener.Addr().String())
		if err != nil {
			panic(fmt.Sprintf("failed to dial gateway: %v", err))
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
		activeConnsMu.Lock()
		for _, c := range activeConns {
			c.Close()
		}
		activeConnsMu.Unlock()
		connWg.Wait()
	}()

	// Warmup: Send 1 frame per vehicle paced to register sessions cleanly without queue burst
	for _, c := range clients {
		c.vehicle.Tick()
		_ = c.writer.WriteMessage(wire.MsgTelemetry, c.vehicle.ToTelemetry())
		time.Sleep(2 * time.Millisecond)
	}
	// Drain any warmup backlog
	for pool.QueueDepth() > 0 {
		time.Sleep(10 * time.Millisecond)
	}

	startDropped := pool.Dropped()

	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	var totalSent sync.WaitGroup
	var sentCount atomic.Uint64
	var maxQueueDepth int
	var qDepthMu sync.Mutex

	// Monitor queue depth every 20ms during benchmark
	go func() {
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				depth := pool.QueueDepth()
				qDepthMu.Lock()
				if depth > maxQueueDepth {
					maxQueueDepth = depth
				}
				qDepthMu.Unlock()
			}
		}
	}()

	start := time.Now()
	staggerStep := time.Second / time.Duration(numVehicles)

	for i, c := range clients {
		totalSent.Add(1)
		staggerDelay := time.Duration(i) * staggerStep
		go func(vc *vehicleClient, delay time.Duration) {
			defer totalSent.Done()
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}

			// Fire first tick immediately upon staggered schedule
			vc.vehicle.Tick()
			if err := vc.writer.WriteMessage(wire.MsgTelemetry, vc.vehicle.ToTelemetry()); err == nil {
				sentCount.Add(1)
			}

			ticker := time.NewTicker(1 * time.Second)
			defer ticker.Stop()

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					vc.vehicle.Tick()
					msg := vc.vehicle.ToTelemetry()
					if err := vc.writer.WriteMessage(wire.MsgTelemetry, msg); err == nil {
						sentCount.Add(1)
					}
				}
			}
		}(c, staggerDelay)
	}

	totalSent.Wait()
	elapsed := time.Since(start)
	time.Sleep(50 * time.Millisecond) // Drain wait

	qDepthMu.Lock()
	finalMaxDepth := maxQueueDepth
	qDepthMu.Unlock()

	droppedDuringRun := pool.Dropped() - startDropped

	return IngestionResult{
		NumVehicles:     numVehicles,
		Duration:        elapsed,
		TotalMessages:   sentCount.Load(),
		MessagesPerSec:  float64(sentCount.Load()) / elapsed.Seconds(),
		MaxQueueDepth:   finalMaxDepth,
		DroppedMessages: droppedDuringRun,
	}
}

// -----------------------------------------------------------------------------
// Command Round-Trip Latency Benchmark
// -----------------------------------------------------------------------------

type LatencyResult struct {
	Count int
	Min   time.Duration
	Mean  time.Duration
	P50   time.Duration
	P90   time.Duration
	P99   time.Duration
	Max   time.Duration
}

func runCommandLatencyBenchmark(duration time.Duration) LatencyResult {
	privKey, pubKey, err := crypto.GenerateKeyPair()
	if err != nil {
		panic(err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	defer listener.Close()

	sessions := session.NewSessionRegistry()
	sessions.Quiet = true
	dispatcher := command.NewCommandDispatcher(privKey, sessions)
	dispatcher.Quiet = true

	const vin = "BENCH-CMD-01"
	vehicle := simulator.NewSimulatedVehicle(vin, pb.VehicleModel_VEHICLE_MODEL_RIVIAN_R1T, simulator.ScenarioParkedSentry)
	vehicle.Quiet = true

	acceptDone := make(chan struct{})
	var gwConn net.Conn
	go func() {
		gwConn, _ = listener.Accept()
		close(acceptDone)
	}()

	clientConn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		panic(err)
	}
	defer clientConn.Close()
	<-acceptDone
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

	// Warmup
	warmCtx, warmCancel := context.WithTimeout(context.Background(), 2*time.Second)
	_, _ = dispatcher.DispatchAndWait(warmCtx, vin, pb.CommandType_COMMAND_TYPE_LOCK_DOORS, 0)
	warmCancel()

	durations := make([]time.Duration, 0, 10000)
	deadline := time.Now().Add(duration)

	for time.Now().Before(deadline) {
		cmdCtx, cmdCancel := context.WithTimeout(context.Background(), 2*time.Second)
		start := highResTime()
		resp, err := dispatcher.DispatchAndWait(cmdCtx, vin, pb.CommandType_COMMAND_TYPE_LOCK_DOORS, 0)
		dur := highResDuration(start, highResTime())
		cmdCancel()

		if err != nil || !resp.GetSuccess() {
			panic(fmt.Sprintf("command dispatch failed: %v", err))
		}
		durations = append(durations, dur)
	}

	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	n := len(durations)
	var sum time.Duration
	for _, d := range durations {
		sum += d
	}

	return LatencyResult{
		Count: n,
		Min:   durations[0],
		Mean:  sum / time.Duration(n),
		P50:   durations[n*50/100],
		P90:   durations[n*90/100],
		P99:   durations[n*99/100],
		Max:   durations[n-1],
	}
}

// -----------------------------------------------------------------------------
// Payload Efficiency Measurement
// -----------------------------------------------------------------------------

type PayloadMetrics struct {
	ProtoWireBytes int
	JSONBodyBytes  int
	HTTPWireBytes  int
	Ratio          float64
	ReductionPct   float64
}

func measurePayload() PayloadMetrics {
	v := simulator.NewSimulatedVehicle("SIM-0001", pb.VehicleModel_VEHICLE_MODEL_RIVIAN_R1T, simulator.ScenarioHighwayCruising)
	v.Tick()
	t := v.ToTelemetry()

	pbPayload, err := proto.Marshal(t)
	if err != nil {
		panic(err)
	}
	protoWireBytes := 5 + len(pbPayload) // 4 bytes length + 1 byte type

	jsonPayload, err := protojson.Marshal(t)
	if err != nil {
		panic(err)
	}
	jsonBodyBytes := len(jsonPayload)

	req, err := http.NewRequest("POST", "http://gateway.fleet.internal:8080/api/v1/telemetry", bytes.NewReader(jsonPayload))
	if err != nil {
		panic(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "FleetVehicleTelemetry/1.0")
	req.Header.Set("Connection", "keep-alive")
	req.ContentLength = int64(len(jsonPayload))

	var buf bytes.Buffer
	if err := req.Write(&buf); err != nil {
		panic(err)
	}
	httpWireBytes := buf.Len()
	ratio := float64(httpWireBytes) / float64(protoWireBytes)
	reduction := (1.0 - float64(protoWireBytes)/float64(httpWireBytes)) * 100.0

	return PayloadMetrics{
		ProtoWireBytes: protoWireBytes,
		JSONBodyBytes:  jsonBodyBytes,
		HTTPWireBytes:  httpWireBytes,
		Ratio:          ratio,
		ReductionPct:   reduction,
	}
}
