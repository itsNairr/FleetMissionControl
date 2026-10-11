import { lazy, Suspense, useEffect, useRef, useState } from 'react'
import { Lock, LockOpen, Package, PackageOpen, Power, Thermometer, X, Zap } from 'lucide-react'
import type { CommandType, Vehicle } from '../api/types'
import type { Theme } from '../hooks/useTheme'
import { hotspotsFor, type Hotspot } from '../lib/hotspots'
import { modelSpec } from '../lib/models'
import { fleetStatus, fmt, humanize, soc, timeAgo } from '../lib/vehicle'
import { useUnits } from '../lib/units'
import { BatteryPill } from './Meter'
import { PanelBoundary } from './PanelBoundary'
import { SpeedGauge } from './SpeedGauge'
import { StatusBadge } from './StatusBadge'

// three.js is ~600 KB: load it only when a vehicle is actually opened
const VehicleViewer = lazy(() => import('../three/VehicleViewer'))

interface Props {
  vehicle: Vehicle
  alertVins: Set<string>
  pending: Set<CommandType>
  theme: Theme
  onCommand: (cmd: CommandType, speedLimitMph?: number) => void
  onClose: () => void
}

const LOW_TIRE_BAR = 1.7
const GEAR_LETTER: Record<string, string> = { GEAR_PARK: 'P', GEAR_REVERSE: 'R', GEAR_NEUTRAL: 'N', GEAR_DRIVE: 'D' }

export function VehicleDetail({ vehicle, alertVins, pending, theme, onCommand, onClose }: Props) {
  const t = vehicle.telemetry
  const spec = modelSpec(t.model)
  const status = fleetStatus(vehicle, alertVins)
  const charging = t.chargingState?.state === 'CHARGE_STATE_CHARGING'
  const u = useUnits()
  const [speedLimit, setSpeedLimit] = useState(u.limitRange.initial)
  // Switching mph ↔ km/h resets the slider to a sensible value in the new unit
  useEffect(() => setSpeedLimit(u.limitRange.initial), [u])
  const offline = !vehicle.online
  const packTemp = t.batteryState?.packTemperature ?? 0
  const aux = t.batteryState?.lowVoltageBatteryVolts ?? 0

  // A newly opened vehicle starts at the top, not at the previous vehicle's scroll position
  const panel = useRef<HTMLElement>(null)
  useEffect(() => {
    panel.current?.scrollTo({ top: 0 })
  }, [vehicle.vin])

  return (
    <section ref={panel} className="card detail" aria-label={`Vehicle ${vehicle.vin}`}>
      <div className="detail-head">
        <div>
          <p className="eyebrow">{spec.maker}</p>
          <h2 className="detail-title">{spec.name}</h2>
          <div className="detail-sub">
            <StatusBadge status={status} />
            <span className="vin">{vehicle.vin}</span>
            <span>· {humanize(t.vehicleMode)}</span>
          </div>
        </div>
        <button className="round-btn" onClick={onClose} aria-label="Close vehicle panel"><X size={18} /></button>
      </div>

      {/* Stage: the vehicle sits directly on the surface, with its monogram behind it */}
      <div className="stage">
        <span className="monogram" aria-hidden>{spec.name.replace('Model ', '')}</span>
        <PanelBoundary name="3D view">
          <Suspense fallback={<div className="viewer-loading">Loading 3D…</div>}>
            <VehicleViewer vehicle={vehicle} alertVins={alertVins} pending={pending} theme={theme} onCommand={(c) => onCommand(c)} />
          </Suspense>
        </PanelBoundary>
        <p className="stage-hint">Drag to orbit · tap a hotspot to send a signed command</p>
      </div>

      {/* Round quick controls, like the Rivian app */}
      <div className="quick-controls" aria-label="Remote commands">
        {hotspotsFor(vehicle).map((s) => (
          <QuickButton key={s.id} spot={s} busy={pending.has(s.command)} disabled={offline} onClick={() => onCommand(s.command)} />
        ))}
      </div>
      {offline && <p className="offline-note">Vehicle is offline. Commands resume when it reconnects.</p>}

      <div className="drive-row">
        <SpeedGauge speed={u.speed(t.speedKmh)} unit={u.speedUnit} maxSpeed={u.gaugeMax} powerKw={t.batteryState?.packPower ?? 0} gear={GEAR_LETTER[t.gear] ?? '–'} />
        <div className="range">
          <span className="range-value num">{fmt(u.distance(t.remainingRangeKm))}<small>{u.distanceUnit}</small></span>
          <span className="range-label">Estimated range</span>
          <BatteryPill value={soc(vehicle)} limit={t.chargingState?.chargeLimitSocPct || undefined} />
          <span className="updated">Updated {timeAgo(Number(t.timestampMs))}</span>
        </div>
      </div>

      <div className="tiles">
        <div className="tile tile-energy">
          <span className="tile-title"><Zap size={16} aria-hidden /> Energy</span>
          <span className="tile-value num">{fmt(soc(vehicle))}%</span>
          <span className="tile-sub">
            {charging
              ? `Charging ${fmt(t.chargingState?.chargingPowerKw, 1)} kW · ${humanize(t.chargingState?.chargerType ?? '')}`
              : `${fmt(Math.abs(t.batteryState?.packPower ?? 0), 1)} kW ${((t.batteryState?.packPower ?? 0) >= 0) ? 'draw' : 'in'}`}
          </span>
        </div>
        <div className="tile tile-climate">
          <span className="tile-title"><Thermometer size={16} aria-hidden /> Climate</span>
          <span className="tile-value num">{fmt(u.temp(t.cabinState?.insideTempCelsius ?? 0))}°</span>
          <span className="tile-sub">Cabin · {fmt(u.temp(t.cabinState?.outsideTempCelsius ?? 0))}{u.tempUnit} outside</span>
        </div>
      </div>

      <dl className="spec-list">
        <Spec label="Pack temperature" value={`${fmt(u.temp(packTemp), 1)} ${u.tempUnit}`} warn={packTemp > 55} />
        <Spec label="12V battery" value={`${fmt(aux, 1)} V`} warn={aux > 0 && aux < 11} />
        <Spec label="Odometer" value={`${Math.round(u.distance(t.odometerKm)).toLocaleString()} ${u.distanceUnit}`} />
        {t.truckState?.towModeActive && <Spec label="Towing" value={`${Math.round(u.weight(t.truckState.estimatedTrailerWeightKg)).toLocaleString()} ${u.weightUnit}`} />}
        {t.truckState?.bedOutletsActive && <Spec label="Bed outlets" value={`${fmt(t.truckState.bedOutletsPowerKw, 1)} kW`} />}
      </dl>

      {t.tirePressure && (
        <div className="tires" aria-label={`Tire pressures in ${u.pressureUnit}`}>
          <span className="section-label">Tire pressure · {u.pressureUnit}</span>
          <div className="tire-grid">
            {([
              ['Front left', t.tirePressure.frontLeftBar], ['Front right', t.tirePressure.frontRightBar],
              ['Rear left', t.tirePressure.rearLeftBar], ['Rear right', t.tirePressure.rearRightBar],
            ] as const).map(([pos, bar]) => (
              <div key={pos} className={`tire ${bar < LOW_TIRE_BAR ? 'is-low' : ''}`}>
                <span className="tire-pos">{pos}</span>
                <span className="tire-val num">{u.pressure(bar).toFixed(u.pressureDigits)}{bar < LOW_TIRE_BAR && <span className="warn-mark" aria-label="low pressure"> ▲</span>}</span>
              </div>
            ))}
          </div>
        </div>
      )}

      <div className="limit">
        <span className="section-label">Speed limit</span>
        <div className="limit-row">
          <input
            type="range"
            min={u.limitRange.min}
            max={u.limitRange.max}
            step={u.limitRange.step}
            value={speedLimit}
            onChange={(e) => setSpeedLimit(Number(e.target.value))}
            aria-label={`Speed limit in ${u.speedUnit}`}
          />
          <span className="limit-sign num" aria-hidden>{speedLimit}<small>{u.speedUnit}</small></span>
        </div>
        <div className="limit-actions">
          <button className="btn btn-graphite" disabled={offline || pending.has('SET_SPEED_LIMIT')} onClick={() => onCommand('SET_SPEED_LIMIT', u.limitToMph(speedLimit))}>
            {pending.has('SET_SPEED_LIMIT') ? 'Sending…' : `Set ${speedLimit} ${u.speedUnit} limit`}
          </button>
          <button className="btn btn-ghost" disabled={offline || pending.has('REBOOT_COMPUTER')} onClick={() => onCommand('REBOOT_COMPUTER')}>
            <Power size={15} aria-hidden /> Reboot onboard computer
          </button>
        </div>
      </div>
    </section>
  )
}

const QUICK_ICON = {
  doors: { on: LockOpen, off: Lock },
  frunk: { on: PackageOpen, off: Package },
  rear: { on: PackageOpen, off: Package },
} as const

function QuickButton({ spot, busy, disabled, onClick }: { spot: Hotspot; busy: boolean; disabled: boolean; onClick: () => void }) {
  const Icon = spot.active ? QUICK_ICON[spot.id].on : QUICK_ICON[spot.id].off
  const verb = spot.command.split('_')[0].toLowerCase() // open / close / lock / unlock
  return (
    <button
      className={`quick ${spot.active ? 'is-active' : ''} ${busy ? 'is-busy' : ''}`}
      disabled={disabled || busy}
      onClick={onClick}
      aria-label={`${spot.label} is ${spot.state.toLowerCase()}. ${verb} ${spot.label.toLowerCase()}`}
    >
      <span className="quick-circle"><Icon size={22} strokeWidth={1.8} aria-hidden /></span>
      <span className="quick-label">{spot.label}</span>
      <span className="quick-state">{spot.state}</span>
    </button>
  )
}

function Spec({ label, value, warn = false }: { label: string; value: string; warn?: boolean }) {
  return (
    <div className={`spec ${warn ? 'is-warn' : ''}`}>
      <dt>{label}</dt>
      <dd className="num">{value}{warn && <span className="warn-mark" aria-label="out of range"> ▲</span>}</dd>
    </div>
  )
}
