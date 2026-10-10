import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { MapContainer, Marker, TileLayer, Tooltip, useMap } from 'react-leaflet'
import L from 'leaflet'
import 'leaflet/dist/leaflet.css'
import { LocateFixed, Minus, Plus } from 'lucide-react'
import type { Vehicle } from '../api/types'
import type { Theme } from '../hooks/useTheme'
import { modelSpec } from '../lib/models'
import { statusSvg } from '../lib/statusIcons'
import { fleetStatus, STATUS_META, STATUS_ORDER, type FleetStatus } from '../lib/vehicle'
import { useUnits } from '../lib/units'
import { StatusIcon } from './StatusBadge'

interface Props {
  vehicles: Vehicle[]
  alertVins: Set<string>
  selected: string | null
  onSelect: (vin: string) => void
  theme: Theme
}

// Basemap: keyless OpenStreetMap tiles. Day mode shows them as-is (like the in-car map);
// night mode darkens them with a CSS filter. For a native style set VITE_MAP_TILES (+ VITE_MAP_ATTRIBUTION).
// OSM's tile policy allows light, attributed use like this; production traffic needs its own provider.
const CUSTOM_TILES = import.meta.env.VITE_MAP_TILES as string | undefined
const TILE_URL = CUSTOM_TILES ?? 'https://tile.openstreetmap.org/{z}/{x}/{y}.png'
const TILE_ATTRIBUTION = (import.meta.env.VITE_MAP_ATTRIBUTION as string | undefined)
  ?? '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors'

// One cached Leaflet icon per (status, heading bucket, selected) so markers only re-render on real changes
const iconCache = new Map<string, L.DivIcon>()
function pinIcon(status: FleetStatus, heading: number, selected: boolean): L.DivIcon {
  const bucket = Math.round(heading / 15) * 15
  const key = `${status}|${bucket}|${selected}`
  let icon = iconCache.get(key)
  if (!icon) {
    const glyph = statusSvg(status, 14, status === 'driving' ? bucket : 0)
    icon = L.divIcon({
      className: '',
      html: `<div class="map-pin pin-${status}${selected ? ' is-selected' : ''}">${glyph}</div>`,
      iconSize: [30, 30],
      iconAnchor: [15, 15],
    })
    iconCache.set(key, icon)
  }
  return icon
}

function located(vehicles: Vehicle[]) {
  return vehicles.filter((v) => v.telemetry.location && v.telemetry.location.latitude !== 0)
}

export function FleetMap({ vehicles, alertVins, selected, onSelect, theme }: Props) {
  const u = useUnits()
  const onMap = located(vehicles)
  const selectedVehicle = onMap.find((v) => v.vin === selected)
  // Follow mode: the map tracks the selected car like in-car navigation until the operator drags it
  const [following, setFollowing] = useState(true)
  useEffect(() => setFollowing(true), [selected])
  const stopFollowing = useCallback(() => setFollowing(false), [])

  return (
    <section className="card map-card" aria-label="Fleet map">
      {/* The day/night tile filter is pure CSS keyed on <html data-theme>: react-leaflet never
          re-applies a TileLayer's className after creation, so a prop-based class would go stale */}
      <MapContainer
        center={[36.7, -119.4]}
        zoom={6}
        className={`map ${CUSTOM_TILES ? 'custom-tiles' : 'osm-tiles'}`}
        zoomControl={false}
        attributionControl
        aria-label={`Fleet map, ${theme} mode`}
      >
        <TileLayer url={TILE_URL} attribution={TILE_ATTRIBUTION} maxZoom={19} />
        <FitOnFirstData vehicles={onMap} />
        <FollowSelected vehicle={selectedVehicle} following={following} onUserPan={stopFollowing} />
        <MapButtons vehicles={onMap} selected={selectedVehicle} following={following} onRecenter={() => setFollowing(true)} />
        {onMap.map((v) => {
          const status = fleetStatus(v, alertVins)
          const loc = v.telemetry.location!
          return (
            <Marker
              key={v.vin}
              position={[loc.latitude, loc.longitude]}
              icon={pinIcon(status, loc.heading, v.vin === selected)}
              zIndexOffset={v.vin === selected ? 1000 : STATUS_ORDER.length - STATUS_ORDER.indexOf(status)}
              eventHandlers={{ click: () => onSelect(v.vin) }}
              keyboard
              title={`${v.vin} ${STATUS_META[status].label}`}
            >
              <Tooltip direction="top" offset={[0, -14]} className="map-tip">
                <strong>{modelSpec(v.telemetry.model).name}</strong> <span className="vin">{v.vin}</span>
                <br />
                {STATUS_META[status].label} · {Math.round(u.speed(v.telemetry.speedKmh))} {u.speedUnit} · {Math.round(v.telemetry.batteryState?.stateOfCharge ?? 0)}%
              </Tooltip>
            </Marker>
          )
        })}
      </MapContainer>

      <ul className="map-legend" aria-label="Map legend">
        {STATUS_ORDER.map((s) => (
          <li key={s}>
            <span className={`map-pin pin-${s} legend-pin`} aria-hidden><StatusIcon status={s} size={9} /></span>
            {STATUS_META[s].label}
          </li>
        ))}
      </ul>
    </section>
  )
}

interface MapButtonsProps {
  vehicles: Vehicle[]
  selected?: Vehicle
  following: boolean
  onRecenter: () => void
}

/** Round floating controls, like the buttons on the in-car map */
function MapButtons({ vehicles, selected, following, onRecenter }: MapButtonsProps) {
  const map = useMap()
  const latest = useRef(vehicles)
  latest.current = vehicles
  // With a car selected: recenter on it and resume following. Otherwise: show the whole fleet.
  const locate = () => {
    if (selected) {
      onRecenter()
      return
    }
    const pts = latest.current.map((v) => [v.telemetry.location!.latitude, v.telemetry.location!.longitude] as [number, number])
    if (pts.length) map.flyToBounds(L.latLngBounds(pts), { padding: [60, 60], maxZoom: 11, duration: 0.8 })
  }
  // Leaflet would otherwise treat clicks on our buttons as map clicks/drags
  const stop = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (stop.current) L.DomEvent.disableClickPropagation(stop.current)
  }, [])

  return (
    <div className="map-buttons" ref={stop}>
      <button className="round-btn" onClick={() => map.zoomIn()} aria-label="Zoom in"><Plus size={18} /></button>
      <button className="round-btn" onClick={() => map.zoomOut()} aria-label="Zoom out"><Minus size={18} /></button>
      <button
        className={`round-btn ${selected && !following ? 'is-attention' : ''}`}
        onClick={locate}
        aria-label={selected ? `Recenter on ${selected.vin}` : 'Show whole fleet'}
        title={selected ? (following ? 'Following vehicle' : 'Recenter on vehicle') : 'Show whole fleet'}
      >
        <LocateFixed size={18} />
      </button>
    </div>
  )
}

/** Zoom to the fleet once, when vehicles first appear (later the operator controls the view) */
function FitOnFirstData({ vehicles }: { vehicles: Vehicle[] }) {
  const map = useMap()
  const done = useRef(false)
  const points = useMemo(
    () => vehicles.map((v) => [v.telemetry.location!.latitude, v.telemetry.location!.longitude] as [number, number]),
    [vehicles],
  )
  useEffect(() => {
    if (done.current || points.length === 0) return
    done.current = true
    map.fitBounds(L.latLngBounds(points), { padding: [60, 60], maxZoom: 11 })
  }, [map, points])
  return null
}

interface FollowProps {
  vehicle?: Vehicle
  following: boolean
  onUserPan: () => void
}

/**
 * Fly to a vehicle when it's selected, then keep it centered as it drives (follow mode).
 * A user drag ends following; the locate button resumes it.
 */
function FollowSelected({ vehicle, following, onUserPan }: FollowProps) {
  const map = useMap()
  const vin = vehicle?.vin
  const lat = vehicle?.telemetry.location?.latitude
  const lon = vehicle?.telemetry.location?.longitude
  const flying = useRef(false)

  // Only real user drags stop following (programmatic pans don't fire dragstart)
  useEffect(() => {
    map.on('dragstart', onUserPan)
    return () => { map.off('dragstart', onUserPan) }
  }, [map, onUserPan])

  // New selection, or recenter pressed: fly in
  useEffect(() => {
    if (!vin || lat === undefined || lon === undefined || !following) return
    flying.current = true
    map.flyTo([lat, lon], Math.max(map.getZoom(), 13), { duration: 0.8 })
    map.once('moveend', () => { flying.current = false })
    // eslint-disable-next-line react-hooks/exhaustive-deps -- position updates are handled below
  }, [map, vin, following])

  // Each telemetry update: glide along with the car
  useEffect(() => {
    if (!vin || !following || flying.current || lat === undefined || lon === undefined) return
    map.panTo([lat, lon], { animate: true, duration: 0.25, easeLinearity: 1 })
  }, [map, vin, following, lat, lon])

  return null
}
