import { useEffect, useState } from 'react'
import type { AlertLevel, VehicleAlert } from '../api/types'
import { useSocket, type SocketStatus } from './useSocket'

/**
 * The rules engine re-fires an alert every telemetry tick (1 Hz) while its condition holds.
 * We collapse those repeats into one "active" alert per vin+code, and clear it once it
 * hasn't been seen for STALE_MS, giving the UI raised / cleared semantics.
 */
const STALE_MS = 5_000

export interface ActiveAlert {
  key: string
  vin: string
  code: string
  level: AlertLevel
  description: string
  firstSeen: number
  lastSeen: number
}

const severity: Record<AlertLevel, number> = {
  ALERT_LEVEL_CRITICAL: 4,
  ALERT_LEVEL_ERROR: 3,
  ALERT_LEVEL_WARNING: 2,
  ALERT_LEVEL_INFO: 1,
  ALERT_LEVEL_UNSPECIFIED: 0,
}

/** Plain-English names for the diagnostic trouble codes vehicles self-report in telemetry */
const DTC_DESCRIPTIONS: Record<string, string> = {
  VCFRONT_a182_AuxBatterySag: '12V auxiliary battery voltage sagging',
  BMS_w035_TirePressureLow: 'Tire pressure low (vehicle TPMS)',
  DI_w032_LowSocTurtle: 'Critically low charge: drive power limited',
}

/**
 * Alerts come from two places: the gateway's rules engine (streamed) and the vehicle's own
 * diagnostic trouble codes (inside telemetry). Merge both so the map, KPIs and panel agree.
 */
const dtcFirstSeen = new Map<string, number>() // Telemetry has no "raised at", so remember first sighting

export function mergeVehicleDtcs(
  alerts: ActiveAlert[],
  vehicles: { vin: string; online: boolean; telemetry: { activeAlertCodes: string[]; timestampMs: string } }[],
): ActiveAlert[] {
  const seen = new Set(alerts.map((a) => a.key))
  const extra: ActiveAlert[] = []
  for (const v of vehicles) {
    if (!v.online) continue
    for (const code of v.telemetry.activeAlertCodes) {
      const key = `${v.vin}|${code}`
      if (seen.has(key)) continue
      const ts = Number(v.telemetry.timestampMs)
      if (!dtcFirstSeen.has(key)) dtcFirstSeen.set(key, ts)
      extra.push({
        key,
        vin: v.vin,
        code,
        level: 'ALERT_LEVEL_WARNING',
        description: DTC_DESCRIPTIONS[code] ?? code,
        firstSeen: dtcFirstSeen.get(key)!,
        lastSeen: ts,
      })
    }
  }
  // Forget codes that cleared, so a recurrence shows a fresh "since"
  const live = new Set(extra.map((a) => a.key))
  for (const key of dtcFirstSeen.keys()) if (!live.has(key)) dtcFirstSeen.delete(key)
  return [...alerts, ...extra].sort(
    (a, b) => severity[b.level] - severity[a.level] || a.vin.localeCompare(b.vin),
  )
}

export function useAlerts(): { alerts: ActiveAlert[]; status: SocketStatus } {
  const [active, setActive] = useState<Map<string, ActiveAlert>>(new Map())

  const status = useSocket<VehicleAlert>('/alerts/stream', (a) => {
    const now = Date.now()
    const key = `${a.vin}|${a.code}`
    setActive((prev) => {
      const next = new Map(prev)
      const existing = prev.get(key)
      next.set(key, {
        key,
        vin: a.vin,
        code: a.code,
        level: a.level,
        description: a.description, // Latest reading (e.g. current temperature)
        firstSeen: existing?.firstSeen ?? now,
        lastSeen: now,
      })
      return next
    })
  })

  // Expire alerts whose condition stopped re-firing
  useEffect(() => {
    const timer = window.setInterval(() => {
      const cutoff = Date.now() - STALE_MS
      setActive((prev) => {
        let changed = false
        const next = new Map(prev)
        for (const [key, a] of prev) {
          if (a.lastSeen < cutoff) {
            next.delete(key)
            changed = true
          }
        }
        return changed ? next : prev
      })
    }, 1_000)
    return () => window.clearInterval(timer)
  }, [])

  const alerts = [...active.values()].sort(
    (a, b) => severity[b.level] - severity[a.level] || a.firstSeen - b.firstSeen,
  )
  return { alerts, status }
}
