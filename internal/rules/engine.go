package rules

import (
	"fmt"
	"time"

	pb "github.com/itsnairr/fleet-telemetry-engine/internal/protocol"
)

// RulesEngine evaluates incoming vehicle telemetry for physical and safety anomalies.
type RulesEngine struct{}

func NewRulesEngine() *RulesEngine {
	return &RulesEngine{}
}

// Evaluate runs all safety rules against a telemetry frame and returns any triggered alerts.
func (e *RulesEngine) Evaluate(t *pb.VehicleTelemetry) []*pb.VehicleAlert {
	if t == nil {
		return nil
	}

	var alerts []*pb.VehicleAlert
	nowMs := time.Now().UnixMilli()

	// 1. Battery Thermal Hazard Check (ISO-6469-1)
	if b := t.GetBatteryState(); b != nil {
		if b.GetMaxCellTemperature() > 55.0 || b.GetPackTemperature() > 55.0 {
			alerts = append(alerts, &pb.VehicleAlert{
				Vin:         t.GetVin(),
				TimestampMs: nowMs,
				Location:    t.GetLocation(),
				Level:       pb.VehicleAlert_ALERT_LEVEL_CRITICAL,
				Code:        "BMS_a066_ThermalRunawayImminent",
				Description: fmt.Sprintf("Critical battery temperature (Pack: %.1f°C, MaxCell: %.1f°C)", b.GetPackTemperature(), b.GetMaxCellTemperature()),
			})
		}

		// 2. Cell Voltage Delta Imbalance
		deltaV := b.GetMaxCellVoltage() - b.GetMinCellVoltage()
		if deltaV > 0.10 && b.GetMinCellVoltage() > 0 {
			alerts = append(alerts, &pb.VehicleAlert{
				Vin:         t.GetVin(),
				TimestampMs: nowMs,
				Location:    t.GetLocation(),
				Level:       pb.VehicleAlert_ALERT_LEVEL_WARNING,
				Code:        "BMS_w042_CellImbalance",
				Description: fmt.Sprintf("High cell voltage delta (Δ%.3fV: Min %.3fV, Max %.3fV)", deltaV, b.GetMinCellVoltage(), b.GetMaxCellVoltage()),
			})
		}
	}

	// 3. Low Tire Pressure Check (TPMS)
	if tp := t.GetTirePressure(); tp != nil {
		minPressure := float32(1.8) // 1.8 bar ~ 26 PSI (standard ~2.4 bar)
		if tp.GetFrontLeftBar() < minPressure || tp.GetFrontRightBar() < minPressure ||
			tp.GetRearLeftBar() < minPressure || tp.GetRearRightBar() < minPressure {
			alerts = append(alerts, &pb.VehicleAlert{
				Vin:         t.GetVin(),
				TimestampMs: nowMs,
				Location:    t.GetLocation(),
				Level:       pb.VehicleAlert_ALERT_LEVEL_WARNING,
				Code:        "TPMS_w012_TirePressureLow",
				Description: fmt.Sprintf("Tire pressure low (FL: %.1f, FR: %.1f, RL: %.1f, RR: %.1f bar)",
					tp.GetFrontLeftBar(), tp.GetFrontRightBar(), tp.GetRearLeftBar(), tp.GetRearRightBar()),
			})
		}
	}

	// 4. Fleet Overspeed Violation (> 135 km/h)
	if t.GetSpeedKmh() > 135.0 {
		alerts = append(alerts, &pb.VehicleAlert{
			Vin:         t.GetVin(),
			TimestampMs: nowMs,
			Location:    t.GetLocation(),
			Level:       pb.VehicleAlert_ALERT_LEVEL_INFO,
			Code:        "FLEET_w005_SpeedViolation",
			Description: fmt.Sprintf("Fleet speed policy exceeded: %.1f km/h", t.GetSpeedKmh()),
		})
	}

	return alerts
}
