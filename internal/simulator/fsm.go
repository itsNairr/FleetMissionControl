package simulator

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// TransitionRecord logs historical state transitions for vehicle telemetry and diagnostics.
type TransitionRecord struct {
	Timestamp time.Time
	From      string
	Event     string
	To        string
	Reason    string
}

// GuardFunc evaluates whether a transition is permitted given context ctx.
// Returns (true, nil) if allowed, or (false, error) with the safety reason.
type GuardFunc[C any] func(ctx C) (bool, error)

// ActionFunc executes side-effects when a transition succeeds.
type ActionFunc[C any] func(ctx C)

// Transition defines a directed edge in the finite state machine.
type Transition[S comparable, E comparable, C any] struct {
	From   S
	Event  E
	To     S
	Guard  GuardFunc[C]
	Action ActionFunc[C]
	Reason string
}

// FSM is a thread-safe, generic Finite State Machine.
type FSM[S comparable, E comparable, C any] struct {
	mu          sync.RWMutex
	current     S
	transitions map[S]map[E]Transition[S, E, C]
	history     []TransitionRecord
	maxHistory  int
}

// NewFSM initializes an FSM in the given initial state.
func NewFSM[S comparable, E comparable, C any](initial S) *FSM[S, E, C] {
	return &FSM[S, E, C]{
		current:     initial,
		transitions: make(map[S]map[E]Transition[S, E, C]),
		history:     make([]TransitionRecord, 0, 16),
		maxHistory:  50,
	}
}

// AddTransition registers a transition from one state to another for a given event.
func (f *FSM[S, E, C]) AddTransition(t Transition[S, E, C]) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if _, ok := f.transitions[t.From]; !ok {
		f.transitions[t.From] = make(map[E]Transition[S, E, C])
	}
	f.transitions[t.From][t.Event] = t
}

// Current returns the current state.
func (f *FSM[S, E, C]) Current() S {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.current
}

// Can checks if an event can be triggered from the current state without firing it.
func (f *FSM[S, E, C]) Can(event E, ctx C) (bool, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	stateTransitions, ok := f.transitions[f.current]
	if !ok {
		return false, fmt.Errorf("no transitions defined from state %v", f.current)
	}

	t, ok := stateTransitions[event]
	if !ok {
		return false, fmt.Errorf("event %v is invalid in state %v", event, f.current)
	}

	if t.Guard != nil {
		allowed, err := t.Guard(ctx)
		if !allowed {
			if err != nil {
				return false, err
			}
			return false, errors.New("transition blocked by safety guard")
		}
	}

	return true, nil
}

// Trigger fires an event, checks guards, executes actions, and transitions state.
func (f *FSM[S, E, C]) Trigger(event E, ctx C) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	stateTransitions, ok := f.transitions[f.current]
	if !ok {
		return fmt.Errorf("no transitions registered from state %v", f.current)
	}

	t, ok := stateTransitions[event]
	if !ok {
		return fmt.Errorf("illegal transition: cannot process event %v in state %v", event, f.current)
	}

	if t.Guard != nil {
		allowed, err := t.Guard(ctx)
		if !allowed {
			if err != nil {
				return err
			}
			return errors.New("transition rejected by safety guard")
		}
	}

	oldState := f.current
	f.current = t.To

	// Record in transition history
	record := TransitionRecord{
		Timestamp: time.Now(),
		From:      fmt.Sprintf("%v", oldState),
		Event:     fmt.Sprintf("%v", event),
		To:        fmt.Sprintf("%v", t.To),
		Reason:    t.Reason,
	}
	f.history = append(f.history, record)
	if len(f.history) > f.maxHistory {
		f.history = f.history[1:]
	}

	// Execute transition action side effects
	if t.Action != nil {
		t.Action(ctx)
	}

	return nil
}

// ForceSet directly resets the state (e.g. for scenario resets or tests) and logs the event.
func (f *FSM[S, E, C]) ForceSet(state S, reason string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	oldState := f.current
	f.current = state

	record := TransitionRecord{
		Timestamp: time.Now(),
		From:      fmt.Sprintf("%v", oldState),
		Event:     "FORCE_SET",
		To:        fmt.Sprintf("%v", state),
		Reason:    reason,
	}
	f.history = append(f.history, record)
	if len(f.history) > f.maxHistory {
		f.history = f.history[1:]
	}
}

// History returns a copy of recent transition records.
func (f *FSM[S, E, C]) History() []TransitionRecord {
	f.mu.RLock()
	defer f.mu.RUnlock()

	res := make([]TransitionRecord, len(f.history))
	copy(res, f.history)
	return res
}

// --- Automotive Lifecycle FSM Definitions ---

// LifecycleEvent represents triggers that cause vehicle lifecycle state changes.
type LifecycleEvent string

const (
	EventLowBatteryReserve       LifecycleEvent = "LOW_BATTERY_RESERVE"        // SoC <= 15%
	EventCriticalBatteryDepletion LifecycleEvent = "CRITICAL_BATTERY_DEPLETION" // SoC <= 5%
	EventBatteryExhausted        LifecycleEvent = "BATTERY_EXHAUSTED"         // SoC <= 0%
	EventFastChargeTaperReached  LifecycleEvent = "FAST_CHARGE_TAPER_REACHED" // SoC >= 80%
	EventFullChargeReached       LifecycleEvent = "FULL_CHARGE_REACHED"        // SoC >= 100%
	EventSuperchargePluggedIn    LifecycleEvent = "SUPERCHARGE_PLUGGED_IN"
	EventACChargePluggedIn       LifecycleEvent = "AC_CHARGE_PLUGGED_IN"
	EventResumeCruising          LifecycleEvent = "RESUME_CRUISING"
	EventPark                    LifecycleEvent = "PARK"
)

// NewVehicleLifecycleFSM constructs the state machine for autonomous vehicle operational lifecycles.
func NewVehicleLifecycleFSM(initial Scenario) *FSM[Scenario, LifecycleEvent, *SimulatedVehicle] {
	fsm := NewFSM[Scenario, LifecycleEvent, *SimulatedVehicle](initial)

	// Driving scenarios that can encounter low battery:
	drivingScenarios := []Scenario{
		ScenarioHighwayCruising,
		ScenarioHeavyTowing,
		ScenarioUrbanCommute,
		ScenarioOffRoadAdventure,
		ScenarioDeliveryStopCycle,
	}

	for _, s := range drivingScenarios {
		// 1. Critical Battery Depletion -> Turtle Mode (SoC <= 5%)
		fsm.AddTransition(Transition[Scenario, LifecycleEvent, *SimulatedVehicle]{
			From:   s,
			Event:  EventCriticalBatteryDepletion,
			To:     ScenarioLowBatteryTurtle,
			Reason: "SoC <= 5%: Power throttled to preserve cells",
			Guard: func(v *SimulatedVehicle) (bool, error) {
				return v.BatterySoC <= 5.0, nil
			},
			Action: func(v *SimulatedVehicle) {
				v.Scenario = ScenarioLowBatteryTurtle
				v.CurrentSpeed = 25.0
				if !v.Quiet {
					fmt.Printf("[%s] WARNING: Battery state of charge critical (%.1f%%). Throttling power output to low battery mode.\n", v.Vin, v.BatterySoC)
				}
			},
		})

		// 2. Low Battery Reserve -> Route to Supercharger (SoC <= 15%)
		fsm.AddTransition(Transition[Scenario, LifecycleEvent, *SimulatedVehicle]{
			From:   s,
			Event:  EventLowBatteryReserve,
			To:     ScenarioSupercharging,
			Reason: "SoC <= 15%: Rerouting to Supercharger",
			Guard: func(v *SimulatedVehicle) (bool, error) {
				return v.BatterySoC <= 15.0, nil
			},
			Action: func(v *SimulatedVehicle) {
				v.Scenario = ScenarioSupercharging
				v.CurrentSpeed = 0.0
				if !v.Quiet {
					fmt.Printf("[%s] NOTICE: Battery state of charge low (%.1f%%). Routing to charging station.\n", v.Vin, v.BatterySoC)
				}
			},
		})
	}

	// 3. Supercharging -> Fast charge taper knee reached (80%) -> Resume highway journey
	fsm.AddTransition(Transition[Scenario, LifecycleEvent, *SimulatedVehicle]{
		From:   ScenarioSupercharging,
		Event:  EventFastChargeTaperReached,
		To:     ScenarioHighwayCruising,
		Reason: "SoC >= 80%: DC fast charge session complete, departing stall",
		Guard: func(v *SimulatedVehicle) (bool, error) {
			return v.BatterySoC >= 80.0, nil
		},
		Action: func(v *SimulatedVehicle) {
			v.Scenario = ScenarioHighwayCruising
			v.CurrentSpeed = 110.0
			if !v.Quiet {
				fmt.Printf("[%s] NOTICE: DC fast charge reached 80.0%% (%.1f%%). Disconnecting charger and resuming highway cruising.\n", v.Vin, v.BatterySoC)
			}
		},
	})

	// 4. AC Charging -> Full charge reached (100%) -> Parked Sentry
	fsm.AddTransition(Transition[Scenario, LifecycleEvent, *SimulatedVehicle]{
		From:   ScenarioACCharging,
		Event:  EventFullChargeReached,
		To:     ScenarioParkedSentry,
		Reason: "SoC >= 100%: Level 2 AC charge cycle finished",
		Guard: func(v *SimulatedVehicle) (bool, error) {
			return v.BatterySoC >= 100.0, nil
		},
		Action: func(v *SimulatedVehicle) {
			v.Scenario = ScenarioParkedSentry
			v.CurrentSpeed = 0.0
			if !v.Quiet {
				fmt.Printf("[%s] NOTICE: AC charging complete (100%%). Switching to Parked Sentry mode.\n", v.Vin)
			}
		},
	})

	// 5. Turtle Mode -> Battery Exhausted (SoC <= 0%) -> Stop
	fsm.AddTransition(Transition[Scenario, LifecycleEvent, *SimulatedVehicle]{
		From:   ScenarioLowBatteryTurtle,
		Event:  EventBatteryExhausted,
		To:     ScenarioLowBatteryTurtle,
		Reason: "SoC <= 0%: Full pack depletion",
		Guard: func(v *SimulatedVehicle) (bool, error) {
			return v.BatterySoC <= 0.0, nil
		},
		Action: func(v *SimulatedVehicle) {
			v.CurrentSpeed = 0.0
			if !v.Quiet {
				fmt.Printf("[%s] EMERGENCY: Battery completely drained. Propulsion disabled.\n", v.Vin)
			}
		},
	})

	// 6. Direct plugging into Supercharger from Parked
	fsm.AddTransition(Transition[Scenario, LifecycleEvent, *SimulatedVehicle]{
		From:   ScenarioParkedSentry,
		Event:  EventSuperchargePluggedIn,
		To:     ScenarioSupercharging,
		Reason: "Station connected to DC Fast charge port",
		Guard: func(v *SimulatedVehicle) (bool, error) {
			return v.CurrentSpeed == 0.0, nil
		},
		Action: func(v *SimulatedVehicle) {
			v.Scenario = ScenarioSupercharging
			v.CurrentSpeed = 0.0
		},
	})

	// 7. Direct plugging into AC Charger from Parked
	fsm.AddTransition(Transition[Scenario, LifecycleEvent, *SimulatedVehicle]{
		From:   ScenarioParkedSentry,
		Event:  EventACChargePluggedIn,
		To:     ScenarioACCharging,
		Reason: "Station connected to AC L2 charge port",
		Guard: func(v *SimulatedVehicle) (bool, error) {
			return v.CurrentSpeed == 0.0, nil
		},
		Action: func(v *SimulatedVehicle) {
			v.Scenario = ScenarioACCharging
			v.CurrentSpeed = 0.0
		},
	})

	return fsm
}

// --- ISO-26262 Closure Safety FSM ---

// ClosureState models latching states of doors, frunks, and tailgates.
type ClosureState string

const (
	ClosureStateLatched ClosureState = "LATCHED"
	ClosureStateOpen    ClosureState = "OPEN"
)

// ClosureEvent models actuator triggers for vehicle closures.
type ClosureEvent string

const (
	ClosureEventUnlatch ClosureEvent = "UNLATCH"
	ClosureEventLatch   ClosureEvent = "LATCH"
)

// NewClosureSafetyFSM returns a safety-guarded FSM for an actuator (frunk or tailgate).
// In accordance with ISO-26262 ASIL-B hazard prevention, unlatching is blocked when speed > 0.
func NewClosureSafetyFSM(initial ClosureState, componentName string) *FSM[ClosureState, ClosureEvent, *SimulatedVehicle] {
	fsm := NewFSM[ClosureState, ClosureEvent, *SimulatedVehicle](initial)

	// Transition: LATCHED -> OPEN
	fsm.AddTransition(Transition[ClosureState, ClosureEvent, *SimulatedVehicle]{
		From:   ClosureStateLatched,
		Event:  ClosureEventUnlatch,
		To:     ClosureStateOpen,
		Reason: fmt.Sprintf("Unlatch %s", componentName),
		Guard: func(v *SimulatedVehicle) (bool, error) {
			if v.CurrentSpeed > 0 {
				return false, fmt.Errorf("ISO-26262: cannot unlatch %s while vehicle is in motion (current speed: %.1f km/h)", componentName, v.CurrentSpeed)
			}
			return true, nil
		},
		Action: func(v *SimulatedVehicle) {
			if componentName == "frunk" {
				v.FrunkOpen = true
			} else if componentName == "tailgate" || componentName == "trunk" {
				v.TailgateOpen = true
				v.TrunkOpen = true
			}
		},
	})

	// Transition: OPEN -> LATCHED
	fsm.AddTransition(Transition[ClosureState, ClosureEvent, *SimulatedVehicle]{
		From:   ClosureStateOpen,
		Event:  ClosureEventLatch,
		To:     ClosureStateLatched,
		Reason: fmt.Sprintf("Latch %s", componentName),
		Action: func(v *SimulatedVehicle) {
			if componentName == "frunk" {
				v.FrunkOpen = false
			} else if componentName == "tailgate" || componentName == "trunk" {
				v.TailgateOpen = false
				v.TrunkOpen = false
			}
		},
	})

	return fsm
}
