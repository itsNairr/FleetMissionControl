import { Moon, Sun } from 'lucide-react'
import type { Vehicle } from '../api/types'
import type { SocketStatus } from '../hooks/useSocket'
import type { Theme } from '../hooks/useTheme'
import { fleetStatus, soc, STATUS_META, STATUS_ORDER, type FleetStatus } from '../lib/vehicle'
import { useUnitsToggle } from '../lib/units'
import { StatusIcon } from './StatusBadge'

interface Props {
  vehicles: Vehicle[]
  alertVins: Set<string>
  alertCount: number
  link: SocketStatus
  theme: Theme
  onToggleTheme: () => void
}

export function TopBar({ vehicles, alertVins, alertCount, link, theme, onToggleTheme }: Props) {
  const [unitSystem, toggleUnits] = useUnitsToggle()
  const online = vehicles.filter((v) => v.online)
  const counts = Object.fromEntries(STATUS_ORDER.map((s) => [s, 0])) as Record<FleetStatus, number>
  for (const v of vehicles) counts[fleetStatus(v, alertVins)]++
  const avgSoc = online.length ? online.reduce((sum, v) => sum + soc(v), 0) / online.length : 0

  return (
    <header className="topbar">
      <div className="brand">
        <svg viewBox="0 0 32 32" width="28" height="28" aria-hidden>
          <rect width="32" height="32" rx="9" fill="var(--color-solar-yellow)" />
          <path d="M16 7 L23 16 L16 25 L9 16 Z" fill="none" stroke="var(--color-graphite)" strokeWidth="2.6" strokeLinejoin="round" />
          <circle cx="16" cy="16" r="2.4" fill="var(--color-graphite)" />
        </svg>
        <div className="brand-text">
          <span className="brand-name">Mission Control</span>
          <span className="brand-sub">Fleet operations</span>
        </div>
      </div>

      <dl className="kpis">
        <div className="kpi">
          <dt>Online</dt>
          <dd className="num">{online.length}<span className="kpi-of">/{vehicles.length}</span></dd>
        </div>
        <div className="kpi">
          <dt>Avg charge</dt>
          <dd className="num">{online.length ? Math.round(avgSoc) : '—'}<span className="kpi-of">%</span></dd>
        </div>
        <div className="kpi">
          <dt>Active alerts</dt>
          <dd className={`num ${alertCount ? 'kpi-alert' : ''}`}>{alertCount}</dd>
        </div>
      </dl>

      <ul className="status-strip" aria-label="Fleet status">
        {STATUS_ORDER.filter((s) => s !== 'offline' || counts.offline > 0).map((s) => (
          <li key={s} className={`strip-chip strip-${s}`} title={STATUS_META[s].label}>
            <StatusIcon status={s} size={12} />
            <span className="num">{counts[s]}</span>
            <span className="strip-label">{STATUS_META[s].label}</span>
          </li>
        ))}
      </ul>

      <div className="topbar-end">
        <div className={`live live-${link}`} role="status">
          <span className="live-dot" aria-hidden />
          {link === 'open' ? 'Live' : link === 'connecting' ? 'Connecting' : 'Gateway offline'}
        </div>
        <div className="segmented" role="radiogroup" aria-label="Units">
          {(['imperial', 'metric'] as const).map((sys) => (
            <button
              key={sys}
              role="radio"
              aria-checked={unitSystem === sys}
              className={unitSystem === sys ? 'is-on' : ''}
              onClick={() => unitSystem !== sys && toggleUnits()}
              title={sys === 'imperial' ? 'Imperial: mph, mi, °F, psi' : 'Metric: km/h, km, °C, bar'}
            >
              {sys === 'imperial' ? 'mi' : 'km'}
            </button>
          ))}
        </div>
        <button
          className="round-btn"
          onClick={onToggleTheme}
          aria-label={theme === 'day' ? 'Switch to night mode' : 'Switch to day mode'}
          title={theme === 'day' ? 'Night mode' : 'Day mode'}
        >
          {theme === 'day' ? <Moon size={18} /> : <Sun size={18} />}
        </button>
      </div>
    </header>
  )
}
