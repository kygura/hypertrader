import { useEffect, useRef, useState } from 'react'
import {
  fetchAllMids,
  getSharedSocket,
  onConnectionChange,
} from '../lib/hl-client'
import type { AllMids } from '../lib/types'

export function useAllMids() {
  const [mids, setMids] = useState<AllMids | null>(null)
  const [connected, setConnected] = useState(false)
  const seededRef = useRef(false)

  useEffect(() => {
    let cancelled = false
    fetchAllMids()
      .then((m) => {
        if (!cancelled) {
          setMids(m)
          seededRef.current = true
        }
      })
      .catch(() => {})

    const sock = getSharedSocket()
    const unsub = sock.subscribe(
      { type: 'allMids' },
      'allMids',
      (data) => {
        const payload = data as { mids?: AllMids } | AllMids | undefined
        if (!payload) return
        const next =
          (payload as { mids?: AllMids }).mids ?? (payload as AllMids)
        setMids((prev) => ({ ...(prev ?? {}), ...next }))
      },
    )

    const unsubConn = onConnectionChange(setConnected)
    return () => {
      cancelled = true
      unsub()
      unsubConn()
    }
  }, [])

  return { mids, connected }
}
