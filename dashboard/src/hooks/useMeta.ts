import { useEffect, useState } from 'react'
import {
  fetchPerpMetaAndCtxs,
  fetchSpotMetaAndCtxs,
} from '../lib/hl-client'
import type { AssetCtx } from '../lib/types'

let cache: { ts: number; ctxs: AssetCtx[] } | null = null
let inflight: Promise<AssetCtx[]> | null = null
const TTL = 15_000

async function load(): Promise<AssetCtx[]> {
  if (cache && Date.now() - cache.ts < TTL) return cache.ctxs
  if (inflight) return inflight
  inflight = (async () => {
    const [perp, spot] = await Promise.all([
      fetchPerpMetaAndCtxs().catch(() => ({ ctxs: [] as AssetCtx[] })),
      fetchSpotMetaAndCtxs().catch(() => ({ ctxs: [] as AssetCtx[] })),
    ])
    const ctxs = [...perp.ctxs, ...spot.ctxs]
    cache = { ts: Date.now(), ctxs }
    return ctxs
  })()
  try {
    return await inflight
  } finally {
    inflight = null
  }
}

export function useMeta(poll = 15_000) {
  const [ctxs, setCtxs] = useState<AssetCtx[] | null>(cache?.ctxs ?? null)
  const [loading, setLoading] = useState(!cache)

  useEffect(() => {
    let cancelled = false
    let timer: ReturnType<typeof setTimeout>

    const tick = () => {
      load()
        .then((c) => {
          if (!cancelled) {
            setCtxs(c)
            setLoading(false)
          }
        })
        .catch(() => !cancelled && setLoading(false))
        .finally(() => {
          if (!cancelled) timer = setTimeout(tick, poll)
        })
    }
    tick()
    return () => {
      cancelled = true
      clearTimeout(timer)
    }
  }, [poll])

  return { ctxs, loading }
}
