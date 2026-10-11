import type { CommandType, Vehicle } from '../api/types'

export type FleetStatus = 'alert' | 'charging' | 'driving' | 'parked' | 'offline'

/**
 * Status colors (Rivian palette: Canyon Orange, Forest Green, drive blue, Steel, Fog) are reserved
 * for state and always paired with an icon + label. Orange vs green sits in the colorblind "floor"
 * band (ΔE 6.4), which is only acceptable because of that secondary encoding.
 */
export const STATUS_META: Record<FleetStatus, { label: string; color: string }> = {
  alert: { label: 'Alert', color: 'var(--st-alert)' },
  charging: { label: 'Charging', color: 'var(--st-charge)' },
  driving: { label: 'Driving', color: 'var(--st-drive)' },
  parked: { label: 'Parked', color: 'var(--st-parked)' },
  offline: { label: 'Offline', color: 'var(--st-offline)' },
}

export const STATUS_ORDER: FleetStatus[] = ['alert', 'charging', 'driving', 'parked', 'offline']

export function fleetStatus(v: Vehicle, alertVins: Set<string>): FleetStatus {
  if (!v.online) return 'offline'
  if (alertVins.has(v.vin) || v.telemetry.activeAlertCodes.length > 0) return 'alert'
  if (v.telemetry.chargingState?.state === 'CHARGE_STATE_CHARGING') return 'charging'
  if (v.telemetry.speedKmh > 0.5) return 'driving'
  return 'parked'
}

export function soc(v: Vehicle): number {
  return v.telemetry.batteryState?.stateOfCharge ?? 0
}

/** Closures as the cockpit needs them, merging car vs pickup fields */
export function closures(v: Vehicle) {
  const cabin = v.telemetry.cabinState
  return {
    locked: cabin?.isLocked ?? true,
    frunkOpen: cabin?.frunkOpen ?? false,
    rearOpen: (cabin?.trunkOrLiftgateOpen ?? false) || (v.telemetry.truckState?.tailgateOpen ?? false),
  }
}

/** Returns a copy of the vehicle with closures optimistically updated for instant UI responsiveness */
export function applyOptimisticCommand(v: Vehicle, cmd: CommandType): Vehicle {
  const t = v.telemetry
  const cabin = t.cabinState
    ? { ...t.cabinState }
    : {
        insideTempCelsius: 20,
        outsideTempCelsius: 20,
        climateOn: false,
        isLocked: true,
        sentryOrGuardModeActive: false,
        driverDoorOpen: false,
        passengerDoorOpen: false,
        rearLeftDoorOpen: false,
        rearRightDoorOpen: false,
        frunkOpen: false,
        trunkOrLiftgateOpen: false,
      }
  const truck = t.truckState ? { ...t.truckState } : null

  switch (cmd) {
    case 'UNLOCK_DOORS':
      cabin.isLocked = false
      break
    case 'LOCK_DOORS':
      cabin.isLocked = true
      break
    case 'OPEN_FRUNK':
      cabin.frunkOpen = true
      break
    case 'CLOSE_FRUNK':
      cabin.frunkOpen = false
      break
    case 'OPEN_TRUNK':
      cabin.trunkOrLiftgateOpen = true
      if (truck) truck.tailgateOpen = true
      break
    case 'CLOSE_TRUNK':
      cabin.trunkOrLiftgateOpen = false
      if (truck) truck.tailgateOpen = false
      break
  }

  return {
    ...v,
    telemetry: {
      ...t,
      cabinState: cabin,
      truckState: truck,
    },
  }
}

/** HIGHWAY_CRUISING -> Highway cruising */
export function humanize(constant: string): string {
  const s = constant.replace(/^[A-Z]+_STATE_|^CHARGER_TYPE_/, '').toLowerCase().replace(/_/g, ' ')
  return (s.charAt(0).toUpperCase() + s.slice(1))
    .replace(/\b(v2l|ac|dc|edv)\b/gi, (m) => m.toUpperCase()) // Keep acronyms: V2L worksite, AC level 2
}

const compact = new Intl.NumberFormat('en-US', { maximumFractionDigits: 1 })
export const fmt = (n: number | undefined, digits = 0) =>
  n === undefined || Number.isNaN(n) ? '—' : n.toFixed(digits)
export const fmtCompact = (n: number) => compact.format(n)

export function timeAgo(ms: number): string {
  const s = Math.max(0, Math.round((Date.now() - ms) / 1000))
  if (s < 60) return `${s}s ago`
  const m = Math.round(s / 60)
  return m < 60 ? `${m}m ago` : `${Math.round(m / 60)}h ago`
}
