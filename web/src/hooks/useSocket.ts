import { useEffect, useRef, useState } from 'react'
import { wsUrl } from '../api/client'

export type SocketStatus = 'connecting' | 'open' | 'closed'

/**
 * Opens a WebSocket to an API stream and keeps it open: reconnects with exponential backoff
 * (1s → 10s, same policy as the simulator's TCP reconnect). Each message is JSON-parsed
 * and handed to onMessage. Pass path=null to stay disconnected.
 */
export function useSocket<T>(path: string | null, onMessage: (msg: T) => void): SocketStatus {
  const [status, setStatus] = useState<SocketStatus>('connecting')
  const handler = useRef(onMessage)
  handler.current = onMessage // Always call the latest callback without reconnecting

  useEffect(() => {
    if (!path) {
      setStatus('closed')
      return
    }

    let socket: WebSocket | null = null
    let retryTimer: number | undefined
    let backoff = 1000
    let disposed = false

    const connect = () => {
      setStatus('connecting')
      socket = new WebSocket(wsUrl(path))
      socket.onopen = () => {
        backoff = 1000
        setStatus('open')
      }
      socket.onmessage = (event) => {
        try {
          handler.current(JSON.parse(event.data) as T)
        } catch {
          // Ignore malformed frames rather than killing the stream
        }
      }
      socket.onclose = () => {
        if (disposed) return
        setStatus('closed')
        retryTimer = window.setTimeout(connect, backoff)
        backoff = Math.min(backoff * 2, 10_000)
      }
    }

    connect()
    return () => {
      disposed = true
      window.clearTimeout(retryTimer)
      socket?.close()
    }
  }, [path])

  return status
}
