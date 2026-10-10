import type { CommandType, Vehicle } from '../api/types'
import { modelSpec } from './models'
import { closures } from './vehicle'

export interface Hotspot {
  id: 'frunk' | 'rear' | 'doors'
  label: string
  state: string
  active: boolean // Open / unlocked → highlighted
  command: CommandType // What clicking it does next (toggle)
}

/** Which controllable parts this vehicle has. Shared by the 3D hotspots and the button panel. */
export function hotspotsFor(vehicle: Vehicle): Hotspot[] {
  const spec = modelSpec(vehicle.telemetry.model)
  const c = closures(vehicle)
  const rearLabel = spec.rear.charAt(0).toUpperCase() + spec.rear.slice(1)
  const spots: Hotspot[] = []
  if (spec.hasFrunk) {
    spots.push({ id: 'frunk', label: 'Frunk', state: c.frunkOpen ? 'Open' : 'Closed', active: c.frunkOpen, command: c.frunkOpen ? 'CLOSE_FRUNK' : 'OPEN_FRUNK' })
  }
  spots.push({ id: 'rear', label: rearLabel, state: c.rearOpen ? 'Open' : 'Closed', active: c.rearOpen, command: c.rearOpen ? 'CLOSE_TRUNK' : 'OPEN_TRUNK' })
  spots.push({ id: 'doors', label: 'Doors', state: c.locked ? 'Locked' : 'Unlocked', active: !c.locked, command: c.locked ? 'UNLOCK_DOORS' : 'LOCK_DOORS' })
  return spots
}
