import type { VehicleModel } from '../api/types'

/**
 * Model registry: maps each fleet vehicle model to a 3D asset + how the cockpit should treat it.
 *
 * To use a different/realistic model (e.g. a CC-BY model from Sketchfab):
 *   1. Export/download it as .glb and drop it in web/public/models/<source>/
 *   2. Point `url` at it; set `scale` / `rotationY` so it faces +Z (front) and sits on the ground
 *   3. Name animated parts in `parts` (open the .glb in https://gltf.report to see node names)
 *   4. Add the author + license to CREDITS below (CC-BY requires attribution)
 */

export type RearKind = 'trunk' | 'liftgate' | 'tailgate' | 'cargo door'

export interface ModelSpec {
  name: string // Display name
  maker: 'Tesla' | 'Rivian'
  url: string // GLB path under /public
  scale: number // Kenney cars are ~2.5 units long; normalise to similar visual size
  rotationY?: number // Radians, if the asset's front isn't +Z
  hasFrunk: boolean
  rear: RearKind
  // Node names inside the GLB that we animate
  parts: {
    wheelPrefix?: string // Every node starting with this spins with road speed
    rearDoor?: string // Hinged rear door node, swung open when trunk/tailgate is open
  }
}

const KENNEY = '/models/kenney'
const wheels = { wheelPrefix: 'wheel' }

export const MODELS: Record<VehicleModel, ModelSpec> = {
  VEHICLE_MODEL_TESLA_MODEL_3: { name: 'Model 3', maker: 'Tesla', url: `${KENNEY}/sedan.glb`, scale: 1, hasFrunk: true, rear: 'trunk', parts: wheels },
  VEHICLE_MODEL_TESLA_MODEL_S: { name: 'Model S', maker: 'Tesla', url: `${KENNEY}/sedan-sports.glb`, scale: 1, hasFrunk: true, rear: 'liftgate', parts: wheels },
  VEHICLE_MODEL_TESLA_MODEL_Y: { name: 'Model Y', maker: 'Tesla', url: `${KENNEY}/suv.glb`, scale: 1, hasFrunk: true, rear: 'liftgate', parts: wheels },
  VEHICLE_MODEL_TESLA_MODEL_X: { name: 'Model X', maker: 'Tesla', url: `${KENNEY}/suv-luxury.glb`, scale: 1, hasFrunk: true, rear: 'liftgate', parts: wheels },
  VEHICLE_MODEL_TESLA_CYBERTRUCK: { name: 'Cybertruck', maker: 'Tesla', url: `${KENNEY}/truck.glb`, scale: 0.95, hasFrunk: true, rear: 'tailgate', parts: wheels },
  VEHICLE_MODEL_TESLA_SEMI: { name: 'Semi', maker: 'Tesla', url: `${KENNEY}/truck-flat.glb`, scale: 0.9, hasFrunk: false, rear: 'tailgate', parts: wheels },
  VEHICLE_MODEL_RIVIAN_R1T: { name: 'R1T', maker: 'Rivian', url: `${KENNEY}/truck.glb`, scale: 0.95, hasFrunk: true, rear: 'tailgate', parts: wheels },
  VEHICLE_MODEL_RIVIAN_R1S: { name: 'R1S', maker: 'Rivian', url: `${KENNEY}/suv.glb`, scale: 1, hasFrunk: true, rear: 'liftgate', parts: wheels },
  VEHICLE_MODEL_RIVIAN_EDV: { name: 'EDV', maker: 'Rivian', url: `${KENNEY}/delivery.glb`, scale: 0.85, hasFrunk: false, rear: 'cargo door', parts: { ...wheels, rearDoor: 'door' } },
  VEHICLE_MODEL_UNSPECIFIED: { name: 'Vehicle', maker: 'Tesla', url: `${KENNEY}/sedan.glb`, scale: 1, hasFrunk: false, rear: 'trunk', parts: wheels },
}

export function modelSpec(model: VehicleModel | undefined): ModelSpec {
  return MODELS[model ?? 'VEHICLE_MODEL_UNSPECIFIED'] ?? MODELS.VEHICLE_MODEL_UNSPECIFIED
}

export interface Credit {
  title: string
  author: string
  license: string
  licenseUrl: string
  sourceUrl: string
}

/** Every third-party asset shown in the UI. Rendered in the footer; keep in sync with CREDITS.md */
export const CREDITS: Credit[] = [
  {
    title: 'Car Kit 3.1',
    author: 'Kenney',
    license: 'CC0 1.0',
    licenseUrl: 'https://creativecommons.org/publicdomain/zero/1.0/',
    sourceUrl: 'https://kenney.nl/assets/car-kit',
  },
]
