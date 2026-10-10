import { statusSvg } from '../lib/statusIcons'
import { STATUS_META, type FleetStatus } from '../lib/vehicle'

export function StatusIcon({ status, size = 12 }: { status: FleetStatus; size?: number }) {
  return <span className={`status-icon status-${status}`} dangerouslySetInnerHTML={{ __html: statusSvg(status, size) }} />
}

/** Pill chip: icon + label + color, so status never depends on color alone */
export function StatusBadge({ status }: { status: FleetStatus }) {
  return (
    <span className={`chip chip-${status}`}>
      <StatusIcon status={status} size={11} />
      {STATUS_META[status].label}
    </span>
  )
}
