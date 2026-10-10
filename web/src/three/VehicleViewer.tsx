import { Suspense, useMemo, useRef, type MutableRefObject } from 'react'
import { Canvas, useFrame, useThree } from '@react-three/fiber'
import { ContactShadows, Grid, OrbitControls, useGLTF } from '@react-three/drei'
import * as THREE from 'three'
import type { CommandType, Vehicle } from '../api/types'
import type { Theme } from '../hooks/useTheme'
import { hotspotsFor, type Hotspot } from '../lib/hotspots'
import { MODELS, modelSpec, type ModelSpec } from '../lib/models'
import { closures, fleetStatus } from '../lib/vehicle'

interface ViewerProps {
  vehicle: Vehicle
  alertVins: Set<string>
  pending: Set<CommandType>
  theme: Theme
  onCommand: (cmd: CommandType) => void
}

type SpotId = Hotspot['id']
type SpotEls = MutableRefObject<Partial<Record<SpotId, HTMLButtonElement | null>>>

/**
 * Hotspots are ordinary DOM buttons in a layer over the canvas (accessible, styleable, part of the
 * normal React tree). Every frame the 3D scene projects each part's anchor point to screen pixels
 * and moves its button there, so the labels stay glued to the car while you orbit.
 */
export default function VehicleViewer({ vehicle, alertVins, pending, theme, onCommand }: ViewerProps) {
  const spotEls: SpotEls = useRef({})
  const spots = hotspotsFor(vehicle)
  const disabled = !vehicle.online

  return (
    <div className="viewer-stage">
      <Canvas
        shadows
        dpr={[1, 2]}
        camera={{ position: [4.6, 2.6, 5.2], fov: 36 }}
        gl={{ antialias: true, alpha: true }}
        aria-label={`3D model of ${vehicle.vin}. Drag to orbit.`}
      >
        {/* Soft gallery light: overcast key + cool Slate Blue fill, per the Rivian style */}
        <hemisphereLight args={['#ffffff', theme === 'day' ? '#cfd0d0' : '#212121', theme === 'day' ? 1.4 : 0.9]} />
        <directionalLight position={[5, 8, 4]} intensity={theme === 'day' ? 1.8 : 2.2} castShadow shadow-mapSize={[1024, 1024]} />
        <directionalLight position={[-6, 3, -4]} intensity={0.6} color="#9fa1b8" />

        <Suspense fallback={null}>
          <Scene vehicle={vehicle} alertVins={alertVins} spotEls={spotEls} theme={theme} />
        </Suspense>

        <OrbitControls
          target={[0, 0.6, 0]}
          enablePan={false}
          minDistance={4}
          maxDistance={10}
          minPolarAngle={0.35}
          maxPolarAngle={Math.PI / 2.08} // Never look from under the floor
        />
      </Canvas>

      <div className="hotspot-layer">
        {spots.map((s) => {
          const busy = pending.has(s.command)
          return (
            <button
              key={s.id}
              ref={(el) => { spotEls.current[s.id] = el }}
              style={{ visibility: 'hidden' }} // Shown once the first frame has positioned it
              className={`hotspot ${s.active ? 'is-active' : ''} ${busy ? 'is-busy' : ''}`}
              disabled={disabled || busy}
              onClick={() => onCommand(s.command)}
              aria-label={`${s.label}: ${s.state}. Activate to ${s.command.toLowerCase().replace(/_/g, ' ')}`}
            >
              <span className="hotspot-dot" aria-hidden />
              <span className="hotspot-text">
                <strong>{s.label}</strong>
                <span>{busy ? 'Sending…' : s.state}</span>
              </span>
            </button>
          )
        })}
      </div>
    </div>
  )
}

function Scene({ vehicle, alertVins, spotEls, theme }: { vehicle: Vehicle; alertVins: Set<string>; spotEls: SpotEls; theme: Theme }) {
  const spec = modelSpec(vehicle.telemetry.model)
  const c = closures(vehicle)
  const speed = vehicle.online ? vehicle.telemetry.speedKmh : 0

  return (
    <>
      <CarModel key={spec.url} spec={spec} speedKmh={speed} rearOpen={c.rearOpen} spotEls={spotEls} />
      <StatusRing status={fleetStatus(vehicle, alertVins)} locked={c.locked} />
      <MovingFloor speedKmh={speed} theme={theme} />
      <ContactShadows position={[0, 0.005, 0]} opacity={theme === 'day' ? 0.35 : 0.65} scale={10} blur={2.4} far={3} color={theme === 'day' ? '#616161' : '#000000'} />
    </>
  )
}

interface CarModelProps {
  spec: ModelSpec
  speedKmh: number
  rearOpen: boolean
  spotEls: SpotEls
}

const TARGET_LENGTH = 3.6 // Every model is normalised to this length, whatever units its author used
const WHEEL_RADIUS = 0.3 // Kenney wheel radius in model units
const MAX_VISUAL_SPIN = 22 // rad/s; beyond this wheels strobe on a 60 Hz screen

function CarModel({ spec, speedKmh, rearOpen, spotEls }: CarModelProps) {
  const { scene } = useGLTF(spec.url)
  const group = useRef<THREE.Group>(null)
  const { camera, size } = useThree()

  // useGLTF caches one scene per URL; clone it, center it on the origin, sit it on the floor,
  // and find the parts we animate plus the anchor point for each hotspot
  const { root, fitScale, anchors, wheels, rearDoor, rearDoorClosedX } = useMemo(() => {
    const root = scene.clone(true)
    root.traverse((o) => {
      if ((o as THREE.Mesh).isMesh) o.castShadow = true
    })

    const box = new THREE.Box3().setFromObject(root)
    const center = box.getCenter(new THREE.Vector3())
    root.position.set(-center.x, -box.min.y, -center.z)
    box.translate(new THREE.Vector3(-center.x, -box.min.y, -center.z))
    const { min, max } = box
    const height = max.y - min.y

    const anchors: Record<SpotId, THREE.Vector3> = {
      frunk: new THREE.Vector3(0, height * 0.6, max.z * 0.7),
      rear: new THREE.Vector3(0, height * 0.75, min.z * 0.9),
      doors: new THREE.Vector3(max.x * 1.05, height * 0.5, 0.05),
    }

    const wheels: THREE.Object3D[] = []
    let rearDoor: THREE.Object3D | undefined
    root.traverse((o) => {
      if (spec.parts.wheelPrefix && o.name.startsWith(spec.parts.wheelPrefix)) wheels.push(o)
      if (spec.parts.rearDoor && o.name === spec.parts.rearDoor) rearDoor = o
    })

    return {
      root,
      fitScale: TARGET_LENGTH / (max.z - min.z),
      anchors,
      wheels,
      rearDoor,
      rearDoorClosedX: rearDoor?.rotation.x ?? 0,
    }
  }, [scene, spec])

  const projected = useMemo(() => new THREE.Vector3(), [])

  useFrame((_, dt) => {
    // Wheels roll forward (+Z) → positive rotation about X. Damped spin, capped for readability.
    const omega = Math.min((speedKmh / 3.6 / WHEEL_RADIUS) * 0.3, MAX_VISUAL_SPIN)
    for (const w of wheels) w.rotation.x += omega * dt

    // Hinged at the top: swing the bottom edge out backwards when open
    if (rearDoor) {
      const target = rearDoorClosedX + (rearOpen ? 1.25 : 0)
      rearDoor.rotation.x = THREE.MathUtils.damp(rearDoor.rotation.x, target, 6, dt)
    }

    // Glue each hotspot button to its 3D anchor
    if (!group.current) return
    for (const id of Object.keys(anchors) as SpotId[]) {
      const el = spotEls.current[id]
      if (!el) continue
      projected.copy(anchors[id]).applyMatrix4(group.current.matrixWorld).project(camera)
      const x = ((projected.x + 1) / 2) * size.width
      const y = ((1 - projected.y) / 2) * size.height
      el.style.transform = `translate(-50%, -50%) translate3d(${x.toFixed(1)}px, ${y.toFixed(1)}px, 0)`
      el.style.visibility = projected.z < 1 ? 'visible' : 'hidden' // z ≥ 1 → behind the camera
    }
  })

  return (
    <group ref={group} scale={fitScale * spec.scale} rotation-y={spec.rotationY ?? 0}>
      <primitive object={root} />
    </group>
  )
}

// Rivian palette: Canyon Orange, Forest Green, drive blue, Steel, Fog
const RING_COLORS = {
  alert: '#e84826',
  charging: '#629b5c',
  driving: '#3876c9',
  parked: '#9fa1b8',
  offline: '#b8b8b8',
} as const

/** Glowing floor ring: pulses for alerts/charging so the car's state reads at a glance */
function StatusRing({ status, locked }: { status: keyof typeof RING_COLORS; locked: boolean }) {
  const mat = useRef<THREE.MeshBasicMaterial>(null)
  const pulses = status === 'alert' || status === 'charging'

  useFrame(({ clock }) => {
    if (!mat.current) return
    const base = locked ? 0.35 : 0.6
    mat.current.opacity = pulses ? base + Math.sin(clock.elapsedTime * 4) * 0.25 : base
  })

  return (
    <mesh rotation-x={-Math.PI / 2} position={[0, 0.01, 0]}>
      <ringGeometry args={[2.35, 2.5, 96]} />
      <meshBasicMaterial ref={mat} color={RING_COLORS[status]} transparent opacity={0.4} toneMapped={false} />
    </mesh>
  )
}

const CELL = 0.5

/** Infinite grid that slides under the car with its speed: a cheap but convincing sense of motion */
const FLOOR = {
  day: { cell: '#e5e7eb', section: '#cfd0d0' }, // Concrete / Ash hairlines on Chalk
  night: { cell: '#262626', section: '#333333' },
}

function MovingFloor({ speedKmh, theme }: { speedKmh: number; theme: Theme }) {
  const grid = useRef<THREE.Mesh>(null)
  useFrame((_, dt) => {
    if (!grid.current) return
    const z = grid.current.position.z - (speedKmh / 3.6) * 0.12 * dt
    grid.current.position.z = ((z % (CELL * 2)) + CELL * 2) % (CELL * 2) // Wrap seamlessly
  })
  return (
    <Grid
      ref={grid}
      infiniteGrid
      cellSize={CELL}
      sectionSize={CELL * 2}
      cellColor={FLOOR[theme].cell}
      sectionColor={FLOOR[theme].section}
      fadeDistance={16}
      fadeStrength={2.5}
    />
  )
}

// Warm the cache for every model so switching vehicles doesn't flash an empty stage
for (const url of new Set(Object.values(MODELS).map((m) => m.url))) useGLTF.preload(url)
