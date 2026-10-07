package simulator

import (
	"math"
	"math/rand"
	"time"

	pb "github.com/itsnairr/fleet-telemetry-engine/internal/protocol"
)

type Scenario string

const (
	ScenarioHighwayCruising     Scenario = "HIGHWAY_CRUISING"
	ScenarioUrbanCommute        Scenario = "URBAN_COMMUTE"
	ScenarioSupercharging       Scenario = "SUPERCHARGING"
	ScenarioACCharging          Scenario = "AC_CHARGING"
	ScenarioParkedSentry        Scenario = "PARKED_SENTRY"
	ScenarioOffRoadAdventure    Scenario = "OFF_ROAD"
	ScenarioHeavyTowing         Scenario = "HEAVY_TOWING"
	ScenarioV2LWorksite         Scenario = "V2L_WORKSITE"
	ScenarioDeliveryStopCycle   Scenario = "DELIVERY_CYCLE"
	ScenarioWinterColdSoak      Scenario = "WINTER_COLD_SOAK"
	ScenarioAuxBatterySag       Scenario = "AUX_BATTERY_SAG"
	ScenarioThermalHazard       Scenario = "THERMAL_HAZARD"
	ScenarioTirePuncture        Scenario = "TIRE_PUNCTURE"
	ScenarioLowBatteryTurtle    Scenario = "TURTLE_MODE"
)

// California geographical clusters for fleet routing
type CaliforniaRegion struct {
	Name   string
	MinLat float64
	MaxLat float64
	MinLon float64
	MaxLon float64
}

var CaliforniaRegions = []CaliforniaRegion{
	// 1. San Francisco Bay Area & Silicon Valley (Tesla Fremont, Rivian Palo Alto)
	{Name: "Bay Area / Silicon Valley", MinLat: 37.25, MaxLat: 37.95, MinLon: -122.50, MaxLon: -121.80},
	// 2. Greater Los Angeles & Orange County (Urban commute, Rivian Irvine)
	{Name: "Greater LA & Orange County", MinLat: 33.65, MaxLat: 34.25, MinLon: -118.45, MaxLon: -117.75},
	// 3. San Diego Metro & Coastal Corridor
	{Name: "San Diego Metro", MinLat: 32.65, MaxLat: 33.15, MinLon: -117.28, MaxLon: -116.95},
	// 4. Central Valley / I-5 Corridor (Bakersfield, Fresno, Modesto, Sacramento)
	{Name: "Central Valley (I-5 / CA-99)", MinLat: 35.35, MaxLat: 38.60, MinLon: -121.45, MaxLon: -119.70},
	// 5. Sierra Nevada / Lake Tahoe (High altitude, snow, off-road)
	{Name: "Sierra Nevada / Lake Tahoe", MinLat: 38.90, MaxLat: 39.30, MinLon: -120.25, MaxLon: -119.90},
	// 6. Central Coast / Hwy 101 (Monterey, San Luis Obispo, Santa Barbara)
	{Name: "Central Coast (Hwy 101)", MinLat: 34.40, MaxLat: 36.65, MinLon: -121.85, MaxLon: -119.75},
}


func RandomCaliforniaLocation(scenario Scenario) (lat, lon, heading float64) {
	var region CaliforniaRegion
	switch scenario {
	case ScenarioWinterColdSoak, ScenarioOffRoadAdventure:
		// Mountain trails & snow tests in the Sierra / Tahoe passes
		region = CaliforniaRegions[4]
	case ScenarioDeliveryStopCycle:
		// Urban delivery in high-density metros
		if rand.Float64() < 0.55 {
			region = CaliforniaRegions[0] // Bay Area
		} else {
			region = CaliforniaRegions[1] // LA Metro
		}
	case ScenarioHighwayCruising, ScenarioHeavyTowing:
		// Long-distance transport on I-5 or Hwy 101
		if rand.Float64() < 0.65 {
			region = CaliforniaRegions[3] // Central Valley I-5
		} else {
			region = CaliforniaRegions[5] // Central Coast Hwy 101
		}
	default:
		// Distributed across California population centers
		r := rand.Float64()
		switch {
		case r < 0.40:
			region = CaliforniaRegions[0] // 40% Bay Area
		case r < 0.75:
			region = CaliforniaRegions[1] // 35% Greater LA
		case r < 0.90:
			region = CaliforniaRegions[2] // 15% San Diego
		default:
			region = CaliforniaRegions[3] // 10% Central Valley
		}
	}

	lat = region.MinLat + rand.Float64()*(region.MaxLat-region.MinLat)
	lon = region.MinLon + rand.Float64()*(region.MaxLon-region.MinLon)
	heading = rand.Float64() * 360.0
	return
}

type ScenarioWeight struct {
	Scenario Scenario
	Weight   int
}

// DefaultScenarioWeights mirrors a real EV fleet operation
var DefaultScenarioWeights = []ScenarioWeight{
	{Scenario: ScenarioHighwayCruising, Weight: 25},   // 25% Freeway driving
	{Scenario: ScenarioUrbanCommute, Weight: 25},      // 25% Stop-and-go city traffic
	{Scenario: ScenarioSupercharging, Weight: 15},     // 15% Fast DC fast charge
	{Scenario: ScenarioACCharging, Weight: 10},        // 10% Level 2 AC charge
	{Scenario: ScenarioDeliveryStopCycle, Weight: 7},  // 7% Commercial van deliveries
	{Scenario: ScenarioParkedSentry, Weight: 6},       // 6% Guard/Sentry mode active
	{Scenario: ScenarioHeavyTowing, Weight: 4},        // 4% Pickup pulling trailer
	{Scenario: ScenarioOffRoadAdventure, Weight: 3},   // 3% Mountain / trail driving
	{Scenario: ScenarioV2LWorksite, Weight: 2},        // 2% Generator / V2L export
	{Scenario: ScenarioTirePuncture, Weight: 1},       // 1% Low tire pressure alert
	{Scenario: ScenarioLowBatteryTurtle, Weight: 1},   // 1% Critical low battery
	{Scenario: ScenarioAuxBatterySag, Weight: 1},      // 1% 12V auxiliary system sag
}

// SelectRandomScenario samples a scenario according to real-world probability distribution
func SelectRandomScenario() Scenario {
	totalWeight := 0
	for _, sw := range DefaultScenarioWeights {
		totalWeight += sw.Weight
	}
	r := rand.Intn(totalWeight)
	for _, sw := range DefaultScenarioWeights {
		if r < sw.Weight {
			return sw.Scenario
		}
		r -= sw.Weight
	}
	return ScenarioHighwayCruising
}

// SelectCompatibleModel pairs the selected scenario with a realistic vehicle model
func SelectCompatibleModel(scenario Scenario) pb.VehicleModel {
	switch scenario {
	case ScenarioDeliveryStopCycle:
		return pb.VehicleModel_VEHICLE_MODEL_RIVIAN_EDV
	case ScenarioHeavyTowing, ScenarioV2LWorksite:
		if rand.Float64() < 0.5 {
			return pb.VehicleModel_VEHICLE_MODEL_RIVIAN_R1T
		}
		return pb.VehicleModel_VEHICLE_MODEL_TESLA_CYBERTRUCK
	case ScenarioOffRoadAdventure:
		pick := rand.Intn(3)
		switch pick {
		case 0:
			return pb.VehicleModel_VEHICLE_MODEL_RIVIAN_R1T
		case 1:
			return pb.VehicleModel_VEHICLE_MODEL_RIVIAN_R1S
		default:
			return pb.VehicleModel_VEHICLE_MODEL_TESLA_CYBERTRUCK
		}
	default:
		passengerModels := []pb.VehicleModel{
			pb.VehicleModel_VEHICLE_MODEL_TESLA_MODEL_3,
			pb.VehicleModel_VEHICLE_MODEL_TESLA_MODEL_Y,
			pb.VehicleModel_VEHICLE_MODEL_TESLA_MODEL_S,
			pb.VehicleModel_VEHICLE_MODEL_TESLA_MODEL_X,
			pb.VehicleModel_VEHICLE_MODEL_TESLA_CYBERTRUCK,
			pb.VehicleModel_VEHICLE_MODEL_RIVIAN_R1T,
			pb.VehicleModel_VEHICLE_MODEL_RIVIAN_R1S,
		}
		return passengerModels[rand.Intn(len(passengerModels))]
	}
}

type SimulatedVehicle struct {
	Vin          string
	Model        pb.VehicleModel
	Scenario     Scenario
	CurrentSpeed float32
	BatterySoC   float32 // 0.0 to 100.0%
	OdometerKm   float32
	Latitude     float64
	Longitude    float64
	HeadingDeg   float64 // 0.0 - 360.0 degrees
	TireFLBar    float32
	TireFRBar    float32
	TireRLBar    float32
	TireRRBar    float32
	Aux12VVolts  float32
	IsLocked     bool
	FrunkOpen    bool
	TailgateOpen bool
}

// Creates the simulated car and returns the address
func NewSimulatedVehicle(vin string, model pb.VehicleModel, scenario Scenario) *SimulatedVehicle {
	lat, lon, heading := RandomCaliforniaLocation(scenario)

	v := &SimulatedVehicle{
		Vin:          vin,
		Model:        model,
		Scenario:     scenario,
		BatterySoC:   80.0,
		CurrentSpeed: 0.0,
		OdometerKm:   12450.0 + float32(rand.Intn(40000)),
		Latitude:     lat,
		Longitude:    lon,
		HeadingDeg:   heading,
		TireFLBar:    2.4, // Standard 35 PSI ~ 2.4 bar
		TireFRBar:    2.4,
		TireRLBar:    2.4,
		TireRRBar:    2.4,
		Aux12VVolts:  13.8,
		IsLocked:     true,
	}

	// Tailor starting conditions to the active scenario
	switch scenario {
	case ScenarioHeavyTowing:
		v.CurrentSpeed = 95.0
		v.BatterySoC = 80.0
	case ScenarioOffRoadAdventure:
		v.CurrentSpeed = 25.0
		v.BatterySoC = 70.0
	case ScenarioHighwayCruising:
		v.CurrentSpeed = 110.0
		v.BatterySoC = 75.0
	case ScenarioUrbanCommute:
		v.CurrentSpeed = 45.0
		v.BatterySoC = 60.0
	case ScenarioSupercharging:
		v.BatterySoC = 18.0
	case ScenarioLowBatteryTurtle:
		v.BatterySoC = 3.0
		v.CurrentSpeed = 25.0
	case ScenarioTirePuncture:
		v.CurrentSpeed = 65.0
		v.TireFLBar = 1.9 // Pressure actively leaking
	case ScenarioDeliveryStopCycle:
		v.FrunkOpen = false
		v.TailgateOpen = true // Amazon EDV unloading
		v.IsLocked = false
	case ScenarioAuxBatterySag:
		v.Aux12VVolts = 10.8 // Dying low-voltage battery
	}

	return v
}

// Tick advances the vehicle state by 1 second.
func (v *SimulatedVehicle) Tick() {
	// 1. Scenario-specific physics & battery behavior
	switch v.Scenario {
	case ScenarioHighwayCruising:
		v.CurrentSpeed = 110.0
		v.BatterySoC -= 0.006 // ~20 kWh/100km consumption
	case ScenarioHeavyTowing:
		v.CurrentSpeed = 95.0
		v.BatterySoC -= 0.014 // Heavy aerodynamic drag & load
	case ScenarioOffRoadAdventure:
		// Dynamic off-road crawl / trail riding speeds (15 - 35 km/h)
		if v.CurrentSpeed < 32.0 {
			v.CurrentSpeed += 2.0
		} else {
			v.CurrentSpeed = 18.0
		}
		v.BatterySoC -= 0.009 // High torque quad-motor demand on rough terrain
	case ScenarioUrbanCommute:
		// Slight variation around city speeds
		if v.CurrentSpeed < 45.0 {
			v.CurrentSpeed += 5.0
		} else {
			v.CurrentSpeed = 30.0
		}
		v.BatterySoC -= 0.004
	case ScenarioSupercharging:
		v.CurrentSpeed = 0.0
		if v.BatterySoC < 80.0 {
			v.BatterySoC += 0.08 // Rapid DC fast charge (~200 kW)
		} else if v.BatterySoC < 100.0 {
			v.BatterySoC += 0.02 // Taper curve
		}
	case ScenarioACCharging:
		v.CurrentSpeed = 0.0
		if v.BatterySoC < 100.0 {
			v.BatterySoC += 0.003 // ~11 kW Level 2 AC charge
		}
	case ScenarioLowBatteryTurtle:
		v.CurrentSpeed = 25.0 // Power-limited turtle mode
		if v.BatterySoC > 0.5 {
			v.BatterySoC -= 0.001
		}
	case ScenarioTirePuncture:
		v.CurrentSpeed = 50.0
		v.BatterySoC -= 0.007
		if v.TireFLBar > 0.8 {
			v.TireFLBar -= 0.02 // Gradual tire deflation
		}
	case ScenarioAuxBatterySag:
		v.CurrentSpeed = 0.0
		if v.Aux12VVolts > 9.0 {
			v.Aux12VVolts -= 0.01 // Parasitic drain / dead alternator/DCDC
		}
	default:
		// Parked / idle scenarios
		v.CurrentSpeed = 0.0
		v.BatterySoC -= 0.0002 // Phantom standby drain
	}

	// Clamp Battery SoC between 0% and 100%
	if v.BatterySoC < 0 {
		v.BatterySoC = 0
		v.CurrentSpeed = 0
	} else if v.BatterySoC > 100 {
		v.BatterySoC = 100
	}

	// Accumulate odometer and advance GPS if moving
	if v.CurrentSpeed > 0 {
		distKm := float64(v.CurrentSpeed) / 3600.0
		v.OdometerKm += float32(distKm)

		// Move along heading vector:
		// 1 degree latitude ≈ 111.0 km
		// 1 degree longitude ≈ 111.0 km * cos(latitude)
		headingRad := v.HeadingDeg * math.Pi / 180.0
		latDelta := (distKm / 111.0) * math.Cos(headingRad)
		lonDelta := (distKm / (111.0 * math.Cos(v.Latitude*math.Pi/180.0))) * math.Sin(headingRad)

		v.Latitude += latDelta
		v.Longitude += lonDelta
	}
}

// Creates the vehicle and immedietly returns on the address
func (v *SimulatedVehicle) ToTelemetry() *pb.VehicleTelemetry { 
	nowMs := time.Now().UnixMilli()

	// Determine Gear & Driving Dynamics
	gear := pb.Gear_GEAR_PARK
	if v.CurrentSpeed > 0 {
		gear = pb.Gear_GEAR_DRIVE
	}

	// Charging Subsystem
	chargingState := &pb.ChargingState{
		State: pb.ChargingState_CHARGE_STATE_DISCONNECTED,
	}
	if v.Scenario == ScenarioSupercharging {
		chargingState.State = pb.ChargingState_CHARGE_STATE_CHARGING
		chargingState.ChargerType = pb.ChargingState_CHARGER_TYPE_DC_FAST
		chargingState.ChargingPowerKw = 210.0
		chargingState.ChargePortDoorOpen = true
		chargingState.ChargePortLatchEngaged = true
	} else if v.Scenario == ScenarioACCharging {
		chargingState.State = pb.ChargingState_CHARGE_STATE_CHARGING
		chargingState.ChargerType = pb.ChargingState_CHARGER_TYPE_AC_LEVEL_2
		chargingState.ChargingPowerKw = 11.5
		chargingState.ChargePortDoorOpen = true
		chargingState.ChargePortLatchEngaged = true
	}

	// Battery Subsystem (Pack Voltage ~400V nominal)
	packVoltage := float32(392.0)
	packCurrent := float32(0.0)
	if v.CurrentSpeed > 0 {
		packCurrent = v.CurrentSpeed * 2.8 // Draw current proportional to speed
	} else if chargingState.State == pb.ChargingState_CHARGE_STATE_CHARGING {
		packCurrent = -(chargingState.ChargingPowerKw * 1000.0 / packVoltage)
	}

	battery := &pb.BatteryState{
		StateOfCharge:           v.BatterySoC,
		PackVoltage:             packVoltage,
		PackCurrent:             packCurrent,
		PackPower:               (packVoltage * packCurrent) / 1000.0,
		PackHealth:              98.5,
		PackStatus:              pb.BatteryState_BATTERY_STATUS_OK,
		LowVoltageBatteryVolts: v.Aux12VVolts,
		MinCellVoltage:          3.82,
		MaxCellVoltage:          3.85,
	}

	// Cabin & Closures
	cabin := &pb.CabinState{
		InsideTempCelsius:  21.5,
		OutsideTempCelsius: 18.0,
		ClimateOn:          true,
		IsLocked:           v.IsLocked,
		FrunkOpen:          v.FrunkOpen,
		DriverDoorOpen:     false,
	}

	//Build Base Telemetry Frame
	telemetry := &pb.VehicleTelemetry{
		Vin:              v.Vin,
		Model:            v.Model,
		TimestampMs:      nowMs,
		Gear:             gear,
		SpeedKmh:         v.CurrentSpeed,
		OdometerKm:       v.OdometerKm,
		RemainingRangeKm: v.BatterySoC * 4.6, // ~460 km range at 100%
		VehicleMode:      string(v.Scenario),
		Location: &pb.GPSLocation{
			Latitude:  v.Latitude,
			Longitude: v.Longitude,
			Speed:     float64(v.CurrentSpeed),
			Heading:   v.HeadingDeg,
		},
		BatteryState:  battery,
		ChargingState: chargingState,
		TirePressure: &pb.TirePressure{
			FrontLeftBar:  v.TireFLBar,
			FrontRightBar: v.TireFRBar,
			RearLeftBar:   v.TireRLBar,
			RearRightBar:  v.TireRRBar,
		},
		CabinState: cabin,
	}

	// 6. Truck-Specific Telemetry (Populated only for electric pickups)
	if v.Model == pb.VehicleModel_VEHICLE_MODEL_RIVIAN_R1T || v.Model == pb.VehicleModel_VEHICLE_MODEL_TESLA_CYBERTRUCK {
		telemetry.TruckState = &pb.TruckState{
			TailgateOpen:             v.TailgateOpen,
			TowModeActive:            v.Scenario == ScenarioHeavyTowing,
			TrailerConnected:         v.Scenario == ScenarioHeavyTowing,
			EstimatedTrailerWeightKg: 2800.0,
			BedOutletsActive:         v.Scenario == ScenarioV2LWorksite,
			BedOutletsPowerKw:        2.4,
		}
	}

	//Active Diagnostic Trouble Codes (DTCs)
	if v.TireFLBar < 1.7 {
		telemetry.ActiveAlertCodes = append(telemetry.ActiveAlertCodes, "BMS_w035_TirePressureLow")
	}
	if v.Aux12VVolts < 11.0 {
		telemetry.ActiveAlertCodes = append(telemetry.ActiveAlertCodes, "VCFRONT_a182_AuxBatterySag")
	}
	if v.BatterySoC < 5.0 {
		telemetry.ActiveAlertCodes = append(telemetry.ActiveAlertCodes, "DI_w032_LowSocTurtle")
	}

	return telemetry
}
