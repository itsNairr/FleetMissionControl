package worker

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	pb "github.com/itsnairr/fleet-telemetry-engine/internal/protocol"
	"github.com/itsnairr/fleet-telemetry-engine/internal/pubsub"
	"github.com/itsnairr/fleet-telemetry-engine/internal/rules"
	"github.com/itsnairr/fleet-telemetry-engine/internal/twin"
)

type TelemetryWorkerPool struct {
	numWorkers   int
	jobQueue     chan *pb.VehicleTelemetry
	workerWg     sync.WaitGroup
	twinRegistry *twin.DigitalTwinRegistry
	rulesEngine  *rules.RulesEngine
	alerts       *pubsub.Broker[*pb.VehicleAlert] // Fan-out of triggered alerts to dashboards

	Quiet          bool // If true, suppresses stdout console printing (useful for high-frequency benchmarks)
	droppedCount   atomic.Uint64
	processedCount atomic.Uint64
}

// Essentially a python __init__ function to create a telemetry worker pool
func NewTelemetryWorkerPool(numWorkers int, queueCapacity int, twinRegistry *twin.DigitalTwinRegistry, rulesEngine *rules.RulesEngine, alerts *pubsub.Broker[*pb.VehicleAlert]) *TelemetryWorkerPool {
	return &TelemetryWorkerPool{
		numWorkers: numWorkers,
		jobQueue:   make(chan *pb.VehicleTelemetry, queueCapacity),
		//All numWorkers worker goroutines are actively listening to that single shared jobQueue channel
		twinRegistry: twinRegistry,
		rulesEngine:  rulesEngine,
		alerts:       alerts,
	}
}

func (p *TelemetryWorkerPool) worker(workerID int) {
	defer p.workerWg.Done()

	for t := range p.jobQueue {
		// Clean up Model Name
		modelName := strings.TrimPrefix(t.GetModel().String(), "VEHICLE_MODEL_TESLA_")
		modelName = strings.TrimPrefix(modelName, "VEHICLE_MODEL_RIVIAN_")
		modelName = strings.TrimPrefix(modelName, "VEHICLE_MODEL_")

		// Battery & Power Formatting
		soc := float32(0.0)
		powerKw := float32(0.0)
		if t.GetBatteryState() != nil {
			soc = t.GetBatteryState().GetStateOfCharge()
			powerKw = t.GetBatteryState().GetPackPower()
		}

		// Status state and details
		statusState := "CRUISING"
		details := ""

		if t.GetChargingState() != nil && t.GetChargingState().GetState() == pb.ChargingState_CHARGE_STATE_CHARGING {
			statusState = "CHARGING"
			details = fmt.Sprintf("Power: +%.1f kW", t.GetChargingState().GetChargingPowerKw())
		} else if t.GetTruckState() != nil && t.GetTruckState().GetTowModeActive() {
			statusState = "TOWING"
			details = fmt.Sprintf("Trailer: %.0f kg", t.GetTruckState().GetEstimatedTrailerWeightKg())
		} else if t.GetTruckState().GetTailgateOpen() {
			statusState = "SERVICE"
			details = "Tailgate open"
		} else if t.GetVehicleMode() == "OFF_ROAD" {
			statusState = "OFF-ROAD"
			details = "Trail navigation"
		}

		// Alert Flagging
		if len(t.GetActiveAlertCodes()) > 0 {
			statusState = "ALERT"
			details += fmt.Sprintf(" Alert codes: %v", t.GetActiveAlertCodes())
		}

		// Update live in-memory digital twin
		if p.twinRegistry != nil {
			p.twinRegistry.Update(t)
		}

		// Evaluate physical safety & anomaly rules
		if p.rulesEngine != nil {
			alerts := p.rulesEngine.Evaluate(t)
			for _, alert := range alerts {
				if !p.Quiet {
					fmt.Printf("[WorkerPool] ALERT [%s] Code=%s: %s\n", alert.GetLevel().String(), alert.GetCode(), alert.GetDescription())
				}
				if p.alerts != nil {
					p.alerts.Publish(alert)
				}
			}
		}

		// Live Mission Control Print
		if !p.Quiet {
			fmt.Printf("[Worker %2d] [%-8s] %-8s (%-12s) | %-16s | %5.1f km/h | SoC: %4.1f%% (%+5.1f kW) | %s\n",
				workerID,
				statusState,
				t.GetVin(),
				modelName,
				t.GetVehicleMode(),
				t.GetSpeedKmh(),
				soc,
				powerKw,
				details,
			)
		}

		// Simulate 5ms ingestion processing (DB write, anomaly check)
		time.Sleep(5 * time.Millisecond)
		p.processedCount.Add(1)
	}
}

func (p *TelemetryWorkerPool) Start() {
	if !p.Quiet {
		fmt.Printf("[WorkerPool] Starting %d telemetry workers.\n", p.numWorkers)
	}
	for i := 1; i <= p.numWorkers; i++ {
		p.workerWg.Add(1)
		go p.worker(i)
	}
}

func (p *TelemetryWorkerPool) Stop() {
	if !p.Quiet {
		fmt.Println("[WorkerPool] Stopping all telemetry workers.")
	}
	close(p.jobQueue)
	p.workerWg.Wait()
	if !p.Quiet {
		fmt.Println("[WorkerPool] All telemetry workers stopped.")
	}
}

func (p *TelemetryWorkerPool) Enqueue(telemetry *pb.VehicleTelemetry) bool {
	select {
	case p.jobQueue <- telemetry: // Attempt to push telemetry into the job queue
		return true // Message successfully enqueued
	default: // If the job queue is full, the default case is executed
		p.droppedCount.Add(1)
		if !p.Quiet {
			fmt.Printf("[WorkerPool] WARNING: Queue full. Dropping message for VIN: %s\n", telemetry.GetVin())
		}
		return false // Message dropped
	}
}

// Dropped returns the cumulative number of dropped telemetry frames
func (p *TelemetryWorkerPool) Dropped() uint64 {
	return p.droppedCount.Load()
}

// Processed returns the cumulative number of processed telemetry frames
func (p *TelemetryWorkerPool) Processed() uint64 {
	return p.processedCount.Load()
}

// QueueDepth returns the current number of pending items in the job queue
func (p *TelemetryWorkerPool) QueueDepth() int {
	return len(p.jobQueue)
}
