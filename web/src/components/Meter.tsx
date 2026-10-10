/**
 * Rivian-style battery pill: green fill with the percentage inside, and a white knob marking
 * the charge limit. Fill turns Solar Yellow below 20% and Canyon Orange below 10%.
 */
export function BatteryPill({ value, limit, size = 'lg' }: { value: number; limit?: number; size?: 'lg' | 'sm' }) {
  const pct = Math.max(0, Math.min(100, value))
  const level = pct < 10 ? 'critical' : pct < 20 ? 'low' : 'ok'
  return (
    <div
      className={`battery battery-${size} battery-${level}`}
      role="meter"
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={Math.round(pct)}
      aria-label="State of charge"
    >
      <div className="battery-fill" style={{ width: `${pct}%` }}>
        {size === 'lg' && pct >= 18 && <span className="battery-label num">{Math.round(pct)}<small>%</small></span>}
      </div>
      {limit !== undefined && limit > 0 && <span className="battery-knob" style={{ left: `${limit}%` }} title={`Charge limit ${limit}%`} />}
    </div>
  )
}
