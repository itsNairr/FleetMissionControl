import { useCallback, useState } from 'react'
import { ApiError, sendCommand } from '../api/client'
import type { CommandType } from '../api/types'

export interface CommandToast {
  id: number
  vin: string
  command: CommandType
  ok: boolean
  message: string
}

let nextId = 1

/** Sends commands, tracks which are in flight (per vin), and keeps a short feed of vehicle verdicts */
export function useCommands() {
  const [pending, setPending] = useState<Set<string>>(new Set())
  const [toasts, setToasts] = useState<CommandToast[]>([])

  const dismiss = useCallback((id: number) => setToasts((t) => t.filter((x) => x.id !== id)), [])

  const push = useCallback((toast: Omit<CommandToast, 'id'>) => {
    const id = nextId++
    setToasts((t) => [...t.slice(-3), { ...toast, id }])
    window.setTimeout(() => dismiss(id), 6000)
  }, [dismiss])

  const run = useCallback(async (vin: string, command: CommandType, speedLimitMph?: number) => {
    const key = `${vin}|${command}`
    setPending((p) => new Set(p).add(key))
    try {
      const result = await sendCommand(vin, command, speedLimitMph)
      push({
        vin,
        command,
        ok: result.success,
        message: result.success ? 'Accepted by vehicle' : (result.error ?? 'Rejected by vehicle'),
      })
    } catch (err) {
      const message = err instanceof ApiError
        ? ({ 503: 'Vehicle offline', 504: 'No response from vehicle' } as Record<number, string>)[err.status] ?? err.message
        : 'Gateway unreachable'
      push({ vin, command, ok: false, message })
    } finally {
      setPending((p) => {
        const next = new Set(p)
        next.delete(key)
        return next
      })
    }
  }, [push])

  /** Commands currently in flight for one vehicle */
  const pendingFor = useCallback((vin: string): Set<CommandType> => {
    const out = new Set<CommandType>()
    for (const key of pending) {
      const [v, cmd] = key.split('|')
      if (v === vin) out.add(cmd as CommandType)
    }
    return out
  }, [pending])

  return { run, pendingFor, toasts, dismiss }
}
