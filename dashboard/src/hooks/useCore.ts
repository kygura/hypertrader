import { useEffect, useState } from 'react'
import { fetchHealth, type CoreHealth } from '../lib/core-client'

const POLL_MS = 10_000

// useCoreHealth polls the daemon's /api/health every 10s. `online` reflects
// whether the core is reachable at all (fetch succeeded), independent of the
// daemon's own upstream Hyperliquid connection state carried in `health`.
export function useCoreHealth(): { health: CoreHealth | null; online: boolean } {
  const [health, setHealth] = useState<CoreHealth | null>(null)

  useEffect(() => {
    let cancelled = false
    const poll = () => {
      fetchHealth().then((h) => {
        if (!cancelled) setHealth(h)
      })
    }
    poll()
    const id = setInterval(poll, POLL_MS)
    return () => {
      cancelled = true
      clearInterval(id)
    }
  }, [])

  return { health, online: health !== null }
}
