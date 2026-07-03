import { useEffect, useState } from 'react'
import { fetchUserState } from '../lib/hl-client'
import type { HLUserState } from '../lib/types'

export function useUserState(address: string | null, poll = 8000) {
  const [state, setState] = useState<HLUserState | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!address || !/^0x[0-9a-fA-F]{40}$/.test(address)) {
      setState(null)
      setError(null)
      return
    }
    let cancelled = false
    let timer: ReturnType<typeof setTimeout>
    setLoading(true)
    const tick = () => {
      fetchUserState(address)
        .then((s) => {
          if (!cancelled) {
            setState(s)
            setError(null)
            setLoading(false)
          }
        })
        .catch((e) => {
          if (!cancelled) {
            setError(String(e))
            setLoading(false)
          }
        })
        .finally(() => {
          if (!cancelled) timer = setTimeout(tick, poll)
        })
    }
    tick()
    return () => {
      cancelled = true
      clearTimeout(timer)
    }
  }, [address, poll])

  return { state, loading, error }
}
