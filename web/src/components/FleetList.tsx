import { useState } from 'react'
import { Search } from 'lucide-react'
import type { Vehicle } from '../api/types'
import { modelSpec } from '../lib/models'
import { fleetStatus, humanize, soc, STATUS_META, STATUS_ORDER, type FleetStatus } from '../lib/vehicle'
import { useUnits } from '../lib/units'
import { BatteryPill } from './Meter'
import { StatusBadge } from './StatusBadge'

interface Props {
  vehicles: Vehicle[]
  alertVins: Set<string>
  selected: string | null
  onSelect: (vin: string) => void
}

type Filter = 'all' | FleetStatus

export function FleetList({ vehicles, alertVins, selected, onSelect }: Props) {
  const u = useUnits()
  const [query, setQuery] = useState('')
  const [filter, setFilter] = useState<Filter>('all')

  const all = vehicles.map((v) => ({ v, status: fleetStatus(v, alertVins) }))
  const rows = all
    .filter(({ v, status }) => {
      if (filter !== 'all' && status !== filter) return false
      const q = query.trim().toLowerCase()
      return !q || v.vin.toLowerCase().includes(q) || modelSpec(v.telemetry.model).name.toLowerCase().includes(q)
    })
    // Problems first, then by VIN
    .sort((a, b) => STATUS_ORDER.indexOf(a.status) - STATUS_ORDER.indexOf(b.status) || a.v.vin.localeCompare(b.v.vin))

  const filters: Filter[] = ['all', ...STATUS_ORDER.filter((s) => all.some((r) => r.status === s))]

  return (
    <section className="card fleet-list" aria-label="Fleet">
      <div className="card-head">
        <h2>Fleet</h2>
        <span className="count num">{rows.length}</span>
      </div>

      <label className="search">
        <Search size={16} aria-hidden />
        <input
          type="search"
          placeholder="Search VIN or model"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          aria-label="Search vehicles"
        />
      </label>

      <div className="filter-pills" role="radiogroup" aria-label="Filter by status">
        {filters.map((f) => (
          <button
            key={f}
            role="radio"
            aria-checked={filter === f}
            className={`pill ${filter === f ? 'is-on' : ''}`}
            onClick={() => setFilter(f)}
          >
            {f === 'all' ? 'All' : STATUS_META[f].label}
          </button>
        ))}
      </div>

      <ul className="vehicle-rows">
        {rows.map(({ v, status }) => {
          const spec = modelSpec(v.telemetry.model)
          return (
            <li key={v.vin}>
              <button
                className={`vehicle-row ${selected === v.vin ? 'is-selected' : ''} ${v.online ? '' : 'is-offline'}`}
                onClick={() => onSelect(v.vin)}
                aria-pressed={selected === v.vin}
              >
                <div className="row-top">
                  <span className="row-model">{spec.name}</span>
                  <StatusBadge status={status} />
                </div>
                <div className="row-meta">
                  <span className="vin">{v.vin}</span>
                  <span>{humanize(v.telemetry.vehicleMode)}</span>
                </div>
                <div className="row-bottom">
                  <BatteryPill value={soc(v)} size="sm" />
                  <span className="num row-soc">{Math.round(soc(v))}%</span>
                  <span className="num row-speed">{Math.round(u.speed(v.telemetry.speedKmh))} <small>{u.speedUnit}</small></span>
                </div>
              </button>
            </li>
          )
        })}
        {rows.length === 0 && (
          <li className="empty">
            {vehicles.length === 0 ? <>No vehicles yet. Start the simulator:<br /><code>go run ./cmd/simulator -n 20</code></> : 'No matches'}
          </li>
        )}
      </ul>
    </section>
  )
}
