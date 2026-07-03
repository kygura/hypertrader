// Thin client for the hyperagent daemon's unified backend core
// (tui/internal/api). The dashboard keeps its direct Hyperliquid link for
// public market data (see hl-client.ts) — this client only reaches for what
// the daemon uniquely owns: connection/health state and its push stream.

export const CORE_URL =
  (import.meta.env.VITE_CORE_URL as string | undefined) ??
  'http://127.0.0.1:8787'

const CORE_WS_URL = `${CORE_URL.replace(/^http/, 'ws')}/api/ws`

export interface CoreHealth {
  connected: boolean
  mode: string
  providers: { batch: string; chat: string }
  version: string
}

export interface CoreFrame {
  topic: string
  data: unknown
}

// fetchHealth probes the daemon. Any failure (unreachable, timeout, bad
// response) resolves to null rather than throwing — callers treat null as
// "offline," not an error to handle.
export async function fetchHealth(): Promise<CoreHealth | null> {
  try {
    const res = await fetch(`${CORE_URL}/api/health`, {
      signal: AbortSignal.timeout(1500),
    })
    if (!res.ok) return null
    return (await res.json()) as CoreHealth
  } catch {
    return null
  }
}

// coreWS opens the daemon's push stream and calls onFrame for each decoded
// frame, reconnecting with exponential backoff (capped 30s) if the daemon is
// down or restarts. Returns a cleanup function that stops reconnecting and
// closes the socket.
export function coreWS(onFrame: (frame: CoreFrame) => void): () => void {
  let alive = true
  let ws: WebSocket | undefined
  let reconnectMs = 1000
  const maxReconnectMs = 30_000
  let timer: ReturnType<typeof setTimeout> | undefined

  const connect = () => {
    if (!alive) return
    ws = new WebSocket(CORE_WS_URL)
    ws.onopen = () => {
      reconnectMs = 1000
    }
    ws.onclose = () => {
      if (!alive) return
      timer = setTimeout(connect, reconnectMs)
      reconnectMs = Math.min(reconnectMs * 2, maxReconnectMs)
    }
    ws.onerror = () => ws?.close()
    ws.onmessage = (e) => {
      try {
        onFrame(JSON.parse(e.data as string) as CoreFrame)
      } catch {
        // ignore malformed frame
      }
    }
  }
  connect()

  return () => {
    alive = false
    if (timer) clearTimeout(timer)
    ws?.close()
  }
}
