import type { CommandResult, CommandType, Vehicle } from './types'

// Relative by default: Vite's dev proxy forwards /api to the Go gateway.
// Set VITE_API_URL (e.g. http://fleet.example.com) to talk to a gateway on another host.
const API_BASE = (import.meta.env.VITE_API_URL as string | undefined)?.replace(/\/$/, '') ?? ''

export function apiUrl(path: string): string {
  return `${API_BASE}/api/v1${path}`
}

export function wsUrl(path: string): string {
  const base = API_BASE || window.location.origin
  return base.replace(/^http/, 'ws') + `/api/v1${path}`
}

export class ApiError extends Error {
  constructor(public status: number, message: string) {
    super(message)
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(apiUrl(path), init)
  const body = await res.json().catch(() => ({}))
  if (!res.ok) {
    throw new ApiError(res.status, (body as { error?: string }).error ?? `HTTP ${res.status}`)
  }
  return body as T
}

export function fetchVehicles(): Promise<Vehicle[]> {
  return request<Vehicle[]>('/vehicles')
}

/**
 * Resolves with the vehicle's verdict (success true/false).
 * Rejects with ApiError for 400 (bad input), 503 (offline), 504 (no ack within 3s).
 */
export function sendCommand(vin: string, type: CommandType, speedLimitMph?: number): Promise<CommandResult> {
  return request<CommandResult>(`/vehicles/${encodeURIComponent(vin)}/command`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ type, speed_limit_mph: speedLimitMph ?? 0 }),
  })
}
