interface Props {
  speed: number // In display units
  unit: string
  powerKw: number // Positive = drawing power (driving), negative = energy flowing in (charging / regen)
  gear: string // P, R, N, D
  maxSpeed: number
  maxPower?: number
}

// Angles are degrees clockwise from 12 o'clock. Each arc sweeps 120° up one side of the dial,
// leaving a gap at the top (gear box) and at the bottom, like the Rivian instrument cluster.
const SWEEP = 120
const SPEED_START = 215 // bottom-left, grows clockwise toward the top
const POWER_START = 145 // bottom-right, grows counter-clockwise toward the top

const SIZE = 220
const C = SIZE / 2
const R = 88

function point(deg: number): [number, number] {
  const rad = (deg * Math.PI) / 180
  return [C + R * Math.sin(rad), C - R * Math.cos(rad)]
}

/** Clockwise arc between two angles */
function arc(from: number, to: number): string {
  const [x1, y1] = point(from)
  const [x2, y2] = point(to)
  const large = to - from > 180 ? 1 : 0
  return `M ${x1.toFixed(2)} ${y1.toFixed(2)} A ${R} ${R} 0 ${large} 1 ${x2.toFixed(2)} ${y2.toFixed(2)}`
}

export function SpeedGauge({ speed, unit, powerKw, gear, maxSpeed, maxPower = 250 }: Props) {
  const speedFrac = Math.min(Math.max(speed / maxSpeed, 0), 1)
  const powerFrac = Math.min(Math.abs(powerKw) / maxPower, 1)
  const charging = powerKw < 0

  const speedEnd = SPEED_START + SWEEP * speedFrac
  const powerEnd = POWER_START - SWEEP * powerFrac
  const [kx, ky] = point(speedEnd)
  const [px, py] = point(powerEnd)

  return (
    <figure className="gauge" aria-label={`Speed ${Math.round(speed)} ${unit}, ${charging ? 'charging' : 'power'} ${Math.abs(powerKw).toFixed(0)} kW, gear ${gear}`}>
      <svg viewBox={`0 0 ${SIZE} ${SIZE}`} width="100%" role="img" aria-hidden="true">
        {/* Tracks */}
        <path d={arc(SPEED_START, SPEED_START + SWEEP)} className="gauge-track" />
        <path d={arc(POWER_START - SWEEP, POWER_START)} className="gauge-track" />

        {/* Speed: Rivian drive blue with a white knob */}
        {speedFrac > 0.005 && <path d={arc(SPEED_START, speedEnd)} className="gauge-speed" />}
        <circle cx={kx} cy={ky} r={7} className="gauge-knob" />

        {/* Power: Solar Yellow when drawing, Forest Green when energy flows in */}
        {powerFrac > 0.005 && <path d={arc(powerEnd, POWER_START)} className={charging ? 'gauge-charge' : 'gauge-power'} />}
        {powerFrac > 0.005 && <circle cx={px} cy={py} r={4} className="gauge-power-dot" />}
      </svg>

      <div className="gauge-gear" aria-hidden>{gear}</div>
      <div className="gauge-center" aria-hidden>
        <span className="gauge-value num">{Math.round(speed)}</span>
        <span className="gauge-unit">{unit}</span>
      </div>
      <figcaption className="gauge-caption">
        <span><i className="gauge-dot gauge-dot-drive" />Speed</span>
        <span className="num"><i className={`gauge-dot ${charging ? 'gauge-dot-charge' : 'gauge-dot-power'}`} />{Math.abs(powerKw).toFixed(0)} kW {charging ? 'in' : 'out'}</span>
      </figcaption>
    </figure>
  )
}
