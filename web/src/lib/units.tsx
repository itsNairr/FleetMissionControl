import { createContext, useContext, useEffect, useState, type ReactNode } from 'react'

/**
 * One display-unit system for the whole UI. Telemetry always arrives metric (km/h, km, °C, bar);
 * we convert only at the edge, for display, and convert back for commands (the protocol's
 * SET_SPEED_LIMIT field is in mph).
 */
export type UnitSystem = 'imperial' | 'metric'

const KM_PER_MI = 1.609344
const PSI_PER_BAR = 14.5038

export interface Units {
  system: UnitSystem
  speedUnit: string
  distanceUnit: string
  tempUnit: string
  pressureUnit: string
  weightUnit: string
  weight: (kg: number) => number
  speed: (kmh: number) => number
  distance: (km: number) => number
  temp: (c: number) => number
  pressure: (bar: number) => number
  pressureDigits: number
  /** Speed-limit slider range in display units */
  limitRange: { min: number; max: number; step: number; initial: number }
  /** Display-unit speed limit → mph for the SET_SPEED_LIMIT command */
  limitToMph: (value: number) => number
  gaugeMax: number // Speedometer full scale in display units
}

const IMPERIAL: Units = {
  system: 'imperial',
  speedUnit: 'mph',
  distanceUnit: 'mi',
  tempUnit: '°F',
  pressureUnit: 'psi',
  weightUnit: 'lb',
  weight: (kg) => kg * 2.20462,
  speed: (kmh) => kmh / KM_PER_MI,
  distance: (km) => km / KM_PER_MI,
  temp: (c) => (c * 9) / 5 + 32,
  pressure: (bar) => bar * PSI_PER_BAR,
  pressureDigits: 0,
  limitRange: { min: 25, max: 90, step: 5, initial: 65 },
  limitToMph: (mph) => mph,
  gaugeMax: 100,
}

const METRIC: Units = {
  system: 'metric',
  speedUnit: 'km/h',
  distanceUnit: 'km',
  tempUnit: '°C',
  pressureUnit: 'bar',
  weightUnit: 'kg',
  weight: (kg) => kg,
  speed: (kmh) => kmh,
  distance: (km) => km,
  temp: (c) => c,
  pressure: (bar) => bar,
  pressureDigits: 2,
  limitRange: { min: 40, max: 150, step: 10, initial: 100 },
  limitToMph: (kmh) => kmh / KM_PER_MI,
  gaugeMax: 160,
}

const KEY = 'fmc-units'
const UnitsContext = createContext<[Units, () => void]>([IMPERIAL, () => {}])

function initialSystem(): UnitSystem {
  try {
    const saved = window.localStorage.getItem(KEY)
    if (saved === 'imperial' || saved === 'metric') return saved
  } catch {
    // Storage blocked: use the default
  }
  return 'imperial' // US fleet, and Rivian's US cluster reads in mph
}

export function UnitsProvider({ children }: { children: ReactNode }) {
  const [system, setSystem] = useState<UnitSystem>(initialSystem)
  useEffect(() => {
    try {
      window.localStorage.setItem(KEY, system)
    } catch {
      // Non-essential
    }
  }, [system])
  const units = system === 'imperial' ? IMPERIAL : METRIC
  const toggle = () => setSystem((s) => (s === 'imperial' ? 'metric' : 'imperial'))
  return <UnitsContext.Provider value={[units, toggle]}>{children}</UnitsContext.Provider>
}

export function useUnits(): Units {
  return useContext(UnitsContext)[0]
}

export function useUnitsToggle(): [UnitSystem, () => void] {
  const [units, toggle] = useContext(UnitsContext)
  return [units.system, toggle]
}
