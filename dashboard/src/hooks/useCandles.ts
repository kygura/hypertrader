import { useCallback, useState } from 'react'
import { fetchCandles, INTERVALS } from '../lib/hl-client'
import { getCandlesFor, hasRealData } from '../lib/price-data'
import type { Candle } from '../lib/types'
import { useEffect } from 'react'

export function useCandles(
  coin: string | null,
  interval: '1h' | '4h' | '1d' | '1w' = '1d',
  lookbackBars = 180,
) {
  const [candles, setCandles] = useState<Candle[] | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(
    (c: string) => {
      const iv = INTERVALS.find((i) => i.value === interval)!
      const end = Date.now()
      const start = end - iv.ms * lookbackBars
      fetchCandles(c, interval, start, end)
        .then((data) => {
          setCandles(data)
          setLoading(false)
          setError(null)
        })
        .catch(() => {
          if (interval === '1d' && hasRealData(c)) {
            setCandles(getCandlesFor(c).slice(-lookbackBars))
          } else {
            setError('Live candles unavailable')
          }
          setLoading(false)
        })
    },
    [interval, lookbackBars],
  )

  useEffect(() => {
    if (!coin) return
    // Start the fetch — state updates happen in the async callbacks above,
    // which is the correct pattern (not synchronous in the effect body).
    const timer = setTimeout(() => {
      setLoading(true)
      load(coin)
    }, 0)
    return () => clearTimeout(timer)
  }, [coin, load])

  return { candles, loading, error }
}
