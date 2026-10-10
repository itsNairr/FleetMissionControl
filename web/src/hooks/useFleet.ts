import { useEffect, useRef, useState } from 'react'
import { fetchVehicles } from '../api/client'
import type { Vehicle } from '../api/types'
import { useSocket, type SocketStatus } from './useSocket'

export interface Fleet {
  vehicles: Map<string, Vehicle>
  status: SocketStatus
}

const FLUSH_MS = 250 // Batch pushes: 20 cars × 1 Hz would otherwise mean 20 React renders per second

/** Live map of every vehicle: one REST snapshot to start, then the fleet WebSocket keeps it current */
export function useFleet(): Fleet {
  const [vehicles, setVehicles] = useState<Map<string, Vehicle>>(new Map())
  const pending = useRef<Map<string, Vehicle>>(new Map())

  useEffect(() => {
    fetchVehicles()
      .then((list) => setVehicles((prev) => {
        const next = new Map(prev)
        for (const v of list) if (!next.has(v.vin)) next.set(v.vin, v)
        return next
      }))
      .catch(() => {}) // The socket's status pill already shows when the gateway is down
  }, [])

  const status = useSocket<Vehicle>('/fleet/stream', (v) => {
    pending.current.set(v.vin, v)
  })

  useEffect(() => {
    const timer = window.setInterval(() => {
      if (pending.current.size === 0) return
      const batch = pending.current
      pending.current = new Map()
      setVehicles((prev) => {
        const next = new Map(prev)
        for (const [vin, v] of batch) next.set(vin, v)
        return next
      })
    }, FLUSH_MS)
    return () => window.clearInterval(timer)
  }, [])

  return { vehicles, status }
}
