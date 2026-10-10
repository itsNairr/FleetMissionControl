"""
Generate internal/simulator/routes.json: real California road geometry for the fleet simulator.

Routes come from the public OSRM demo server (https://project-osrm.org), which routes over
OpenStreetMap data (© OpenStreetMap contributors, ODbL). We only need ~11 requests, run once,
so this stays well inside the demo server's usage policy (max 1 request/second, no bulk use).

Usage (stdlib only):  python tools/routegen/fetch_routes.py
"""

import json
import math
import pathlib
import time
import urllib.request

OSRM = "https://router.project-osrm.org/route/v1/driving/"
OUT = pathlib.Path(__file__).resolve().parents[2] / "internal" / "simulator" / "routes.json"
SIMPLIFY_METERS = 15  # Douglas-Peucker tolerance: keeps curves, drops redundant points

# (lon, lat) waypoints. kind: highway = freeway corridors, urban = metro loops, mountain = Sierra roads.
# loop=True routes return to their start so vehicles can circulate forever.
ROUTES = [
    ("I-5 Central Valley", "highway", False, [(-121.4944, 38.5816), (-121.2908, 37.9577), (-119.0187, 35.3733)]),
    ("US-101 Central Coast", "highway", False, [(-121.8863, 37.3382), (-121.6555, 36.6777), (-120.6596, 35.2828), (-119.6982, 34.4208)]),
    ("CA-99 San Joaquin", "highway", False, [(-120.9969, 37.6391), (-119.7871, 36.7378), (-119.0187, 35.3733)]),
    ("I-80 Bay to Sierra", "highway", False, [(-122.2711, 37.8044), (-121.4944, 38.5816), (-120.1833, 39.3280)]),
    ("I-5 LA to San Diego", "highway", False, [(-118.2437, 34.0522), (-117.8265, 33.6846), (-117.1611, 32.7157)]),
    ("Bay Area loop", "urban", True, [(-122.4194, 37.7749), (-122.1430, 37.4419), (-121.9886, 37.5485), (-122.2711, 37.8044), (-122.4194, 37.7749)]),
    ("Silicon Valley loop", "urban", True, [(-121.8863, 37.3382), (-122.0839, 37.3861), (-122.0322, 37.3230), (-121.8863, 37.3382)]),
    ("Los Angeles loop", "urban", True, [(-118.2437, 34.0522), (-118.4912, 34.0195), (-118.3090, 34.1808), (-118.1445, 34.1478), (-118.2437, 34.0522)]),
    ("Orange County loop", "urban", True, [(-117.8265, 33.6846), (-118.1937, 33.7701), (-117.9145, 33.8366), (-117.8265, 33.6846)]),
    ("San Diego loop", "urban", True, [(-117.1611, 32.7157), (-117.2713, 32.8328), (-116.9625, 32.7948), (-117.1611, 32.7157)]),
    ("Lake Tahoe loop", "mountain", True, [(-120.1446, 39.1677), (-119.9843, 38.9399), (-119.9518, 39.2513), (-120.1446, 39.1677)]),
]


def fetch(waypoints):
    coords = ";".join(f"{lon},{lat}" for lon, lat in waypoints)
    url = f"{OSRM}{coords}?overview=full&geometries=geojson"
    req = urllib.request.Request(url, headers={"User-Agent": "FleetMissionControl-routegen/1.0 (one-off dataset build)"})
    with urllib.request.urlopen(req, timeout=60) as resp:
        data = json.load(resp)
    if data.get("code") != "Ok":
        raise RuntimeError(f"OSRM error: {data.get('code')} {data.get('message')}")
    route = data["routes"][0]
    return route["geometry"]["coordinates"], route["distance"]  # [[lon, lat], ...], meters


def simplify(points, tolerance_m):
    """Iterative Douglas-Peucker on an equirectangular projection (fine at city scale)."""
    if len(points) < 3:
        return points
    lat0 = math.radians(sum(p[1] for p in points) / len(points))
    xy = [(p[0] * 111_320 * math.cos(lat0), p[1] * 110_540) for p in points]

    def dist(i, a, b):
        (x, y), (x1, y1), (x2, y2) = xy[i], xy[a], xy[b]
        dx, dy = x2 - x1, y2 - y1
        if dx == dy == 0:
            return math.hypot(x - x1, y - y1)
        t = max(0, min(1, ((x - x1) * dx + (y - y1) * dy) / (dx * dx + dy * dy)))
        return math.hypot(x - (x1 + t * dx), y - (y1 + t * dy))

    keep = [False] * len(points)
    keep[0] = keep[-1] = True
    stack = [(0, len(points) - 1)]
    while stack:
        a, b = stack.pop()
        best, idx = 0.0, -1
        for i in range(a + 1, b):
            d = dist(i, a, b)
            if d > best:
                best, idx = d, i
        if best > tolerance_m:
            keep[idx] = True
            stack += [(a, idx), (idx, b)]
    return [p for p, k in zip(points, keep) if k]


def main():
    out = []
    for name, kind, loop, waypoints in ROUTES:
        coords, meters = fetch(waypoints)
        slim = simplify(coords, SIMPLIFY_METERS)
        out.append({
            "name": name,
            "kind": kind,
            "loop": loop,
            "points": [[round(lat, 5), round(lon, 5)] for lon, lat in slim],  # [lat, lon]
        })
        print(f"{name:24s} {meters / 1000:7.1f} km  {len(coords):6d} -> {len(slim):5d} points")
        time.sleep(1.2)  # OSRM demo server policy: at most 1 request per second

    OUT.write_text(json.dumps({
        "source": "OSRM (project-osrm.org) over OpenStreetMap data",
        "license": "Road geometry © OpenStreetMap contributors, ODbL 1.0 (https://www.openstreetmap.org/copyright)",
        "routes": out,
    }, separators=(",", ":")))
    print(f"wrote {OUT} ({OUT.stat().st_size / 1024:.0f} KB)")


if __name__ == "__main__":
    main()
