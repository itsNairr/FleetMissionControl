package simulator

import (
	"math"
	"testing"

	pb "github.com/itsnairr/fleet-telemetry-engine/internal/protocol"
)

func TestEmbeddedRoutesLoad(t *testing.T) {
	if len(Routes()) == 0 {
		t.Fatal("no routes embedded")
	}
	kinds := map[RouteKind]bool{}
	for _, r := range Routes() {
		if r.LengthKm < 10 || len(r.Points) < 10 {
			t.Errorf("%s looks broken: %.1f km, %d points", r.Name, r.LengthKm, len(r.Points))
		}
		kinds[r.Kind] = true
	}
	for _, k := range []RouteKind{RouteHighway, RouteUrban, RouteMountain} {
		if !kinds[k] {
			t.Errorf("no %s routes", k)
		}
	}
}

func TestPositionAtEndpointsAndHeading(t *testing.T) {
	r := Routes()[0]
	lat, lon, heading := r.PositionAt(0)
	if lat != r.Points[0][0] || lon != r.Points[0][1] {
		t.Fatalf("start = %v,%v; want %v", lat, lon, r.Points[0])
	}
	if heading < 0 || heading >= 360 {
		t.Fatalf("heading out of range: %v", heading)
	}
	last := r.Points[len(r.Points)-1]
	if lat, lon, _ := r.PositionAt(r.LengthKm + 50); lat != last[0] || lon != last[1] {
		t.Fatalf("past-the-end should clamp to the last point")
	}
}

// distanceToRouteKm is the shortest distance from a point to the route polyline (equirectangular, fine at this scale)
func distanceToRouteKm(r *Route, lat, lon float64) float64 {
	best := math.Inf(1)
	kx := 111.32 * math.Cos(lat*math.Pi/180)
	const ky = 110.54
	px, py := lon*kx, lat*ky
	for i := 1; i < len(r.Points); i++ {
		ax, ay := r.Points[i-1][1]*kx, r.Points[i-1][0]*ky
		bx, by := r.Points[i][1]*kx, r.Points[i][0]*ky
		dx, dy := bx-ax, by-ay
		tt := 0.0
		if l := dx*dx + dy*dy; l > 0 {
			tt = math.Max(0, math.Min(1, ((px-ax)*dx+(py-ay)*dy)/l))
		}
		best = math.Min(best, math.Hypot(px-(ax+tt*dx), py-(ay+tt*dy)))
	}
	return best
}

// Every scenario, driven for 30 simulated minutes, must stay on its road the whole time
func TestVehiclesStayOnTheirRoad(t *testing.T) {
	for _, w := range DefaultScenarioWeights {
		v := NewSimulatedVehicle("SIM-T", pb.VehicleModel_VEHICLE_MODEL_RIVIAN_R1T, w.Scenario)
		for tick := 0; tick < 1800; tick++ {
			v.Tick()
			if d := distanceToRouteKm(v.Route, v.Latitude, v.Longitude); d > 0.01 {
				t.Fatalf("%s left %s by %.0f m at tick %d", w.Scenario, v.Route.Name, d*1000, tick)
			}
		}
	}
}

func TestHighwayVehicleActuallyMoves(t *testing.T) {
	v := NewSimulatedVehicle("SIM-T", pb.VehicleModel_VEHICLE_MODEL_TESLA_MODEL_3, ScenarioHighwayCruising)
	if v.Route.Kind != RouteHighway {
		t.Fatalf("highway scenario placed on %s route %q", v.Route.Kind, v.Route.Name)
	}
	startLat, startLon := v.Latitude, v.Longitude
	for i := 0; i < 60; i++ {
		v.Tick()
	}
	moved := haversineKm([2]float64{startLat, startLon}, [2]float64{v.Latitude, v.Longitude})
	if moved < 0.5 { // 110 km/h for 60 s ≈ 1.8 km (less if it turned around at a corridor end)
		t.Fatalf("highway vehicle only moved %.2f km in a minute", moved)
	}
}
