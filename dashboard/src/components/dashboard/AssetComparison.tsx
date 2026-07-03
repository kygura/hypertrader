import { useEffect, useMemo, useRef, useState } from 'react'
import {
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
  CartesianGrid,
} from 'recharts'
import { fetchCandles, INTERVALS, type CandleInterval } from '../../lib/hl-client'
import { useMeta } from '../../hooks/useMeta'

const PALETTE = ['#ed3602', '#38a67c', '#ffb800', '#3aa6ff', '#b07cff', '#ff6fad']
const DEFAULTS = ['BTC', 'ETH', 'SOL', 'HYPE']
const MAX = 6

type Interval = '1h' | '4h' | '1d' | '1w'

interface SeriesPoint {
  t: number
  raw: number
  norm: number
}

export function AssetComparison() {
  const { ctxs } = useMeta()
  const [assets, setAssets] = useState<string[]>(DEFAULTS)
  const [interval, setInterval] = useState<Interval>('1d')
  const [series, setSeries] = useState<Record<string, SeriesPoint[]>>({})
  const [loading, setLoading] = useState(false)
  const [showDrop, setShowDrop] = useState(false)
  const [query, setQuery] = useState('')
  const dropRef = useRef<HTMLDivElement | null>(null)

  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (!dropRef.current) return
      if (!dropRef.current.contains(e.target as Node)) setShowDrop(false)
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [])

  useEffect(() => {
    let cancelled = false
    if (assets.length === 0) {
      setSeries({})
      return
    }
    setLoading(true)
    const iv = INTERVALS.find((i) => i.value === interval) as CandleInterval
    const end = Date.now()
    const start = end - iv.ms * 180
    Promise.all(
      assets.map((a) =>
        fetchCandles(a, interval, start, end)
          .then((cs) => ({ a, cs }))
          .catch(() => ({ a, cs: [] })),
      ),
    ).then((results) => {
      if (cancelled) return
      const next: Record<string, SeriesPoint[]> = {}
      for (const { a, cs } of results) {
        if (!cs.length) {
          next[a] = []
          continue
        }
        const base = cs[0].c
        next[a] = cs.map((c) => ({
          t: c.T,
          raw: c.c,
          norm: base > 0 ? (c.c / base) * 100 : 100,
        }))
      }
      setSeries(next)
      setLoading(false)
    })
    return () => {
      cancelled = true
    }
  }, [assets, interval])

  const merged = useMemo(() => {
    const tsSet = new Set<number>()
    for (const a of assets) for (const p of series[a] ?? []) tsSet.add(p.t)
    const ts = [...tsSet].sort((a, b) => a - b)
    return ts.map((t) => {
      const row: Record<string, number> = { t }
      for (const a of assets) {
        const p = (series[a] ?? []).find((x) => x.t === t)
        if (p) {
          row[a] = p.norm
          row[`${a}__raw`] = p.raw
        }
      }
      return row
    })
  }, [series, assets])

  const available = useMemo(() => {
    if (!ctxs) return []
    const q = query.trim().toUpperCase()
    return ctxs
      .filter((c) => !assets.includes(c.name))
      .sort((a, b) => b.dayNtlVlm - a.dayNtlVlm)
      .filter((c) => (q ? c.name.toUpperCase().includes(q) : true))
      .slice(0, 30)
  }, [ctxs, assets, query])

  const addAsset = (name: string) => {
    if (assets.includes(name)) return
    if (assets.length >= MAX) return
    setAssets([...assets, name])
    setQuery('')
    setShowDrop(false)
  }

  const removeAsset = (name: string) => {
    setAssets(assets.filter((a) => a !== name))
  }

  const colorOf = (i: number) => PALETTE[i % PALETTE.length]

  return (
    <div className="flex flex-col h-full w-full">
      <div className="flex items-center gap-2 px-2 py-2 border-b border-border bg-panel-alt flex-shrink-0">
        <div className="flex border border-border">
          {(['1h', '4h', '1d', '1w'] as Interval[]).map((iv) => (
            <button
              key={iv}
              onClick={() => setInterval(iv)}
              className={`px-2 py-1 text-[10px] uppercase tracking-wider ${
                interval === iv
                  ? 'bg-elevated text-text-primary'
                  : 'text-text-secondary hover:text-text-primary'
              }`}
            >
              {iv === '1h' ? '1H' : iv === '4h' ? '4H' : iv === '1d' ? '1D' : '1W'}
            </button>
          ))}
        </div>
        <div ref={dropRef} className="relative ml-auto">
          <input
            placeholder={assets.length >= MAX ? 'MAX 6' : '+ ADD ASSET'}
            value={query}
            onFocus={() => setShowDrop(true)}
            onChange={(e) => {
              setQuery(e.target.value)
              setShowDrop(true)
            }}
            disabled={assets.length >= MAX}
            className="text-[11px] w-[140px]"
          />
          {showDrop && assets.length < MAX && available.length > 0 && (
            <div className="absolute right-0 top-full mt-1 max-h-[240px] overflow-auto z-10 bg-panel-alt border border-border min-w-[140px]">
              {available.map((c) => (
                <button
                  key={c.name + c.kind}
                  onClick={() => addAsset(c.name)}
                  className="w-full text-left px-2 py-1 text-[11px] font-mono tabular-nums text-text-primary hover:bg-hover flex items-center justify-between gap-2"
                >
                  <span>{c.name}</span>
                  <span className="text-text-secondary">{c.kind}</span>
                </button>
              ))}
            </div>
          )}
        </div>
      </div>

      <div className="flex-1 min-h-0 min-w-0 p-2">
        {loading && merged.length === 0 ? (
          <div className="h-full flex items-center justify-center text-text-secondary text-[11px] uppercase tracking-wider">
            LOADING…
          </div>
        ) : assets.length === 0 ? (
          <div className="h-full flex items-center justify-center text-text-secondary text-[11px] uppercase tracking-wider">
            ADD AN ASSET TO COMPARE
          </div>
        ) : (
          <ResponsiveContainer width="100%" height="100%">
            <LineChart data={merged} margin={{ top: 4, right: 6, left: 0, bottom: 0 }}>
              <CartesianGrid stroke="rgba(255,255,255,0.04)" />
              <XAxis
                dataKey="t"
                type="number"
                domain={['dataMin', 'dataMax']}
                tickFormatter={(t) =>
                  new Date(t).toLocaleDateString(undefined, {
                    month: 'short',
                    day: 'numeric',
                  })
                }
                stroke="var(--text-secondary)"
                tick={{
                  fontSize: 10,
                  fontFamily: 'GeistMono, ui-monospace, monospace',
                  fill: 'var(--text-secondary)',
                }}
              />
              <YAxis
                stroke="var(--text-secondary)"
                width={42}
                tickFormatter={(v) => (v as number).toFixed(0)}
                tick={{
                  fontSize: 10,
                  fontFamily: 'GeistMono, ui-monospace, monospace',
                  fill: 'var(--text-secondary)',
                }}
              />
              <Tooltip
                contentStyle={{
                  background: 'var(--bg-panel-alt)',
                  border: '1px solid var(--border)',
                  fontFamily: 'GeistMono, ui-monospace, monospace',
                  fontSize: 11,
                  color: 'var(--text-primary)',
                }}
                labelFormatter={(t) => new Date(t as number).toLocaleString()}
                formatter={(value, name, ctx) => {
                  const key = name as string
                  const raw = (ctx?.payload as Record<string, number> | undefined)?.[
                    `${key}__raw`
                  ]
                  const v = typeof value === 'number' ? value : Number(value)
                  return [
                    `${v.toFixed(2)}  $${raw != null ? raw.toFixed(4) : '--'}`,
                    key,
                  ]
                }}
              />
              {assets.map((a, i) => (
                <Line
                  key={a}
                  type="monotone"
                  dataKey={a}
                  stroke={colorOf(i)}
                  strokeWidth={1.4}
                  dot={false}
                  isAnimationActive={false}
                  connectNulls
                />
              ))}
            </LineChart>
          </ResponsiveContainer>
        )}
      </div>

      <div className="flex flex-wrap gap-1 px-2 py-2 border-t border-border bg-panel-alt flex-shrink-0">
        {assets.map((a, i) => (
          <span
            key={a}
            className="inline-flex items-center gap-1 px-2 py-1 border border-border text-[10px] font-mono"
          >
            <span
              className="inline-block w-2 h-2"
              style={{ background: colorOf(i) }}
            />
            <span className="text-text-primary tabular-nums">{a}</span>
            <button
              onClick={() => removeAsset(a)}
              className="text-text-secondary hover:text-red-accent ml-1"
              aria-label={`Remove ${a}`}
            >
              ×
            </button>
          </span>
        ))}
      </div>
    </div>
  )
}
