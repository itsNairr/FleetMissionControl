import { ChevronRight } from 'lucide-react'
import type { ActiveAlert } from '../hooks/useAlerts'
import { timeAgo } from '../lib/vehicle'
import { StatusIcon } from './StatusBadge'

const LEVEL: Record<string, { label: string; cls: string }> = {
  ALERT_LEVEL_CRITICAL: { label: 'Critical', cls: 'critical' },
  ALERT_LEVEL_ERROR: { label: 'Serious', cls: 'serious' },
  ALERT_LEVEL_WARNING: { label: 'Warning', cls: 'warning' },
  ALERT_LEVEL_INFO: { label: 'Info', cls: 'info' },
  ALERT_LEVEL_UNSPECIFIED: { label: 'Info', cls: 'info' },
}

interface Props {
  alerts: ActiveAlert[]
  onSelect: (vin: string) => void
}

export function AlertsPanel({ alerts, onSelect }: Props) {
  return (
    <section className="card alerts-panel" aria-label="Active alerts">
      <div className="card-head">
        <h2>Active alerts</h2>
        <span className="count num">{alerts.length}</span>
      </div>
      <ul className="alert-rows" aria-live="polite">
        {alerts.map((a) => {
          const lvl = LEVEL[a.level] ?? LEVEL.ALERT_LEVEL_INFO
          return (
            <li key={a.key}>
              <button className={`alert-row alert-${lvl.cls}`} onClick={() => onSelect(a.vin)}>
                <span className="alert-badge" aria-hidden><StatusIcon status="alert" size={16} /></span>
                <span className="alert-body">
                  <span className="alert-title">{a.description}</span>
                  <span className="alert-meta">
                    <strong>{lvl.label}</strong> · <span className="vin">{a.vin}</span> · since {timeAgo(a.firstSeen)}
                  </span>
                </span>
                <ChevronRight size={18} className="alert-chevron" aria-hidden />
              </button>
            </li>
          )
        })}
        {alerts.length === 0 && <li className="empty">All clear. No active alerts.</li>}
      </ul>
    </section>
  )
}
