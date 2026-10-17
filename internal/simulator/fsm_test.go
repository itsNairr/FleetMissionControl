package simulator

import (
	"testing"

	pb "github.com/itsnairr/fleet-telemetry-engine/internal/protocol"
)

func TestFSMGenericLifecycle(t *testing.T) {
	v := NewSimulatedVehicle("SIM-TEST", pb.VehicleModel_VEHICLE_MODEL_RIVIAN_R1T, ScenarioHighwayCruising)
	v.Quiet = true

	if v.LifecycleFSM.Current() != ScenarioHighwayCruising {
		t.Fatalf("expected initial state %s, got %s", ScenarioHighwayCruising, v.LifecycleFSM.Current())
	}

	// 1. Cannot transition to resume cruising if we are not supercharging
	err := v.LifecycleFSM.Trigger(EventFastChargeTaperReached, v)
	if err == nil {
		t.Fatalf("expected illegal transition error, got nil")
	}

	// 2. Battery drops to 14% -> Low battery reserve event
	v.BatterySoC = 14.0
	err = v.LifecycleFSM.Trigger(EventLowBatteryReserve, v)
	if err != nil {
		t.Fatalf("expected transition to Supercharging, got error: %v", err)
	}
	if v.LifecycleFSM.Current() != ScenarioSupercharging {
		t.Fatalf("expected state %s, got %s", ScenarioSupercharging, v.LifecycleFSM.Current())
	}
	if v.Scenario != ScenarioSupercharging {
		t.Fatalf("expected vehicle scenario %s, got %s", ScenarioSupercharging, v.Scenario)
	}
	if v.CurrentSpeed != 0.0 {
		t.Fatalf("expected speed 0.0 at supercharger stall, got %f", v.CurrentSpeed)
	}

	// 3. Charger brings battery to 82% -> Fast charge taper reached
	v.BatterySoC = 82.0
	err = v.LifecycleFSM.Trigger(EventFastChargeTaperReached, v)
	if err != nil {
		t.Fatalf("expected transition to HighwayCruising, got error: %v", err)
	}
	if v.LifecycleFSM.Current() != ScenarioHighwayCruising {
		t.Fatalf("expected state %s, got %s", ScenarioHighwayCruising, v.LifecycleFSM.Current())
	}
	if v.CurrentSpeed != 110.0 {
		t.Fatalf("expected highway speed 110.0, got %f", v.CurrentSpeed)
	}

	// 4. Verify transition history audit log
	history := v.LifecycleFSM.History()
	if len(history) < 2 {
		t.Fatalf("expected at least 2 history records, got %d", len(history))
	}
	last := history[len(history)-1]
	if last.To != string(ScenarioHighwayCruising) {
		t.Fatalf("expected last state %s, got %s", ScenarioHighwayCruising, last.To)
	}
}

func TestClosureSafetyFSM_ISO26262(t *testing.T) {
	v := NewSimulatedVehicle("SIM-TEST", pb.VehicleModel_VEHICLE_MODEL_TESLA_MODEL_S, ScenarioHighwayCruising)
	v.Quiet = true
	v.CurrentSpeed = 105.0

	// 1. Attempt to open frunk while moving at 105 km/h must fail
	err := v.ExecuteCommand(CommandOpenFrunk)
	if err != ErrFrunkHazardInMotion {
		t.Fatalf("expected ErrFrunkHazardInMotion, got %v", err)
	}
	if v.FrunkOpen {
		t.Fatalf("frunk must remain closed when moving")
	}

	// 2. Stop vehicle -> unlatching frunk must succeed
	v.CurrentSpeed = 0.0
	err = v.ExecuteCommand(CommandOpenFrunk)
	if err != nil {
		t.Fatalf("unexpected error opening frunk when stopped: %v", err)
	}
	if !v.FrunkOpen {
		t.Fatalf("frunk should be open")
	}

	// 3. Tailgate interlock check while moving
	v.CurrentSpeed = 40.0
	err = v.ExecuteCommand(CommandOpenTailgate)
	if err != ErrTailgateHazardInMotion {
		t.Fatalf("expected ErrTailgateHazardInMotion, got %v", err)
	}

	// 4. Latch frunk again
	err = v.ExecuteCommand(CommandCloseFrunk)
	if err != nil {
		t.Fatalf("unexpected error latching frunk: %v", err)
	}
	if v.FrunkOpen {
		t.Fatalf("frunk should be latched")
	}
}

func TestAutonomousTickLifecycleWithFSM(t *testing.T) {
	v := NewSimulatedVehicle("SIM-TICK", pb.VehicleModel_VEHICLE_MODEL_RIVIAN_R1T, ScenarioHighwayCruising)
	v.Quiet = true

	// Simulate battery dropping into low reserve
	v.BatterySoC = 14.5
	v.Tick()

	if v.LifecycleFSM.Current() != ScenarioSupercharging {
		t.Fatalf("expected autonomous switch to Supercharging, got %s", v.LifecycleFSM.Current())
	}
	if v.Scenario != ScenarioSupercharging {
		t.Fatalf("expected vehicle scenario to be Supercharging, got %s", v.Scenario)
	}

	// Fast forward charging to 80%
	v.BatterySoC = 80.5
	v.Tick()

	if v.LifecycleFSM.Current() != ScenarioHighwayCruising {
		t.Fatalf("expected autonomous resume to HighwayCruising, got %s", v.LifecycleFSM.Current())
	}
	if v.Scenario != ScenarioHighwayCruising {
		t.Fatalf("expected vehicle scenario to be HighwayCruising, got %s", v.Scenario)
	}
}
