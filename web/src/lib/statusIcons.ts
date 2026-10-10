import type { FleetStatus } from './vehicle'

// Inline SVG bodies (Lucide icon geometry, ISC license) so the same icon can be used both in React
// and inside Leaflet's raw-HTML markers. 24×24 viewBox, stroked with currentColor.
export const STATUS_SVG: Record<FleetStatus, string> = {
  alert: '<path d="m21.73 18-8-14a2 2 0 0 0-3.48 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3"/><path d="M12 9v4"/><path d="M12 17h.01"/>',
  charging: '<path d="M4 14a1 1 0 0 1-.78-1.63l9.9-10.2a.5.5 0 0 1 .86.46l-1.92 6.02A1 1 0 0 0 13 10h7a1 1 0 0 1 .78 1.63l-9.9 10.2a.5.5 0 0 1-.86-.46l1.92-6.02A1 1 0 0 0 11 14z"/>',
  driving: '<polygon points="12 2 19 21 12 17 5 21 12 2"/>', // Points north; rotated by heading on the map
  parked: '<path d="M9 17V7h4a3 3 0 0 1 0 6H9"/>',
  offline: '<path d="M18 6 6 18"/><path d="m6 6 12 12"/>',
}

export function statusSvg(status: FleetStatus, size = 14, rotateDeg = 0): string {
  const filled = status === 'charging' || status === 'driving'
  return `<svg xmlns="http://www.w3.org/2000/svg" width="${size}" height="${size}" viewBox="0 0 24 24" fill="${filled ? 'currentColor' : 'none'}" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round" style="transform: rotate(${rotateDeg}deg)" aria-hidden="true">${STATUS_SVG[status]}</svg>`
}
