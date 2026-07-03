import { useEffect, useMemo, useRef, useState } from 'react'
import * as d3 from 'd3'
import { useMeta } from '../../hooks/useMeta'
import { useAllMids } from '../../hooks/useHLStream'
import type { AssetCtx } from '../../lib/types'

export type TreemapSortBy = 'volume' | 'oi' | 'funding' | 'change'
type Filter = 'ALL' | 'PERP' | 'SPOT'

const SORT_LABELS: Record<TreemapSortBy, string> = {
  volume: 'VOLUME',
  oi: 'OPEN INTEREST',
  funding: 'FUNDING',
  change: '24H CHANGE',
}

interface Props {
  sortBy: TreemapSortBy
  onSortByChange: (s: TreemapSortBy) => void
  onSelect: (name: string) => void
}

function fmtCompact(n: number): string {
  if (!isFinite(n) || n === 0) return '0'
  const abs = Math.abs(n)
  if (abs >= 1e9) return (n / 1e9).toFixed(2) + 'B'
  if (abs >= 1e6) return (n / 1e6).toFixed(2) + 'M'
  if (abs >= 1e3) return (n / 1e3).toFixed(2) + 'K'
  if (abs < 1) return n.toFixed(4)
  return n.toFixed(2)
}

function fmtPct(n: number, digits = 2): string {
  const v = n * 100
  const sign = v >= 0 ? '+' : ''
  return `${sign}${v.toFixed(digits)}%`
}

function metricValue(c: AssetCtx, sortBy: TreemapSortBy): number {
  switch (sortBy) {
    case 'volume':
      return c.dayNtlVlm
    case 'oi':
      return c.openInterest * c.markPx
    case 'funding':
      return Math.abs(c.funding)
    case 'change':
      return Math.abs(c.dayChange)
  }
}

function metricDisplay(c: AssetCtx, sortBy: TreemapSortBy): string {
  switch (sortBy) {
    case 'volume':
      return '$' + fmtCompact(c.dayNtlVlm)
    case 'oi':
      return '$' + fmtCompact(c.openInterest * c.markPx)
    case 'funding':
      return (c.funding * 100).toFixed(4) + '%'
    case 'change':
      return fmtPct(c.dayChange)
  }
}

export function MarketTreemap({ sortBy, onSortByChange, onSelect }: Props) {
  const { ctxs } = useMeta()
  const { mids } = useAllMids()
  const [filter, setFilter] = useState<Filter>('ALL')
  const [query, setQuery] = useState('')
  const containerRef = useRef<HTMLDivElement | null>(null)
  const [dims, setDims] = useState({ w: 0, h: 0 })

  useEffect(() => {
    const el = containerRef.current
    if (!el) return
    const ro = new ResizeObserver((entries) => {
      for (const e of entries) {
        const cr = e.contentRect
        setDims({ w: Math.max(0, cr.width), h: Math.max(0, cr.height) })
      }
    })
    ro.observe(el)
    return () => ro.disconnect()
  }, [])

  const data = useMemo(() => {
    if (!ctxs) return [] as AssetCtx[]
    const q = query.trim().toUpperCase()
    return ctxs
      .map((c) => {
        const live = mids?.[c.name]
        if (live) return { ...c, markPx: Number(live) }
        return c
      })
      .filter((c) => (filter === 'ALL' ? true : c.kind === filter))
      .filter((c) => (q ? c.name.toUpperCase().includes(q) : true))
      .filter((c) => metricValue(c, sortBy) > 0)
  }, [ctxs, mids, filter, query, sortBy])

  const leaves = useMemo(() => {
    if (!dims.w || !dims.h || data.length === 0) return []
    const root = d3
      .hierarchy<{ children: AssetCtx[] } | AssetCtx>({ children: data } as {
        children: AssetCtx[]
      })
      .sum((d) => {
        if ((d as { children?: unknown }).children) return 0
        return metricValue(d as AssetCtx, sortBy)
      })
      .sort((a, b) => (b.value ?? 0) - (a.value ?? 0))

    d3
      .treemap<{ children: AssetCtx[] } | AssetCtx>()
      .size([dims.w, dims.h])
      .paddingInner(2)
      .round(true)(root)

    return (root.leaves() as unknown as Array<
      d3.HierarchyRectangularNode<AssetCtx>
    >)
  }, [data, dims, sortBy])

  const colorOpacity = useMemo(
    () => d3.scaleLinear().domain([0, 0.1]).range([0.2, 0.9]).clamp(true),
    [],
  )

  return (
    <div className="flex flex-col h-full w-full">
      <div className="flex items-center gap-2 px-2 py-2 border-b border-border bg-panel-alt flex-shrink-0">
        <div className="flex border border-border">
          {(['ALL', 'PERP', 'SPOT'] as Filter[]).map((f) => (
            <button
              key={f}
              onClick={() => setFilter(f)}
              className={`px-2 py-1 text-[10px] uppercase tracking-wider ${
                filter === f
                  ? 'bg-elevated text-text-primary'
                  : 'text-text-secondary hover:text-text-primary'
              }`}
            >
              {f}
            </button>
          ))}
        </div>
        <input
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="SEARCH"
          className="text-[11px] flex-1 max-w-[180px]"
        />
        <div className="ml-auto flex items-center gap-1">
          <span className="label">SORT</span>
          <div className="flex border border-border">
            {(Object.keys(SORT_LABELS) as TreemapSortBy[]).map((s) => (
              <button
                key={s}
                onClick={() => onSortByChange(s)}
                className={`px-2 py-1 text-[10px] uppercase tracking-wider ${
                  sortBy === s
                    ? 'bg-elevated text-text-primary'
                    : 'text-text-secondary hover:text-text-primary'
                }`}
              >
                {SORT_LABELS[s]}
              </button>
            ))}
          </div>
        </div>
      </div>

      <div ref={containerRef} className="flex-1 min-h-0 min-w-0 relative overflow-hidden">
        {dims.w > 0 && dims.h > 0 && leaves.length > 0 && (
          <svg width={dims.w} height={dims.h} className="block">
            {leaves.map((leaf) => {
              const d = leaf.data
              const w = leaf.x1 - leaf.x0
              const h = leaf.y1 - leaf.y0
              const up = d.dayChange >= 0
              const opacity = colorOpacity(Math.abs(d.dayChange))
              const fill = up ? 'var(--green)' : 'var(--red)'
              const showLabels = w >= 56 && h >= 34
              const bigVal = metricDisplay(d, sortBy)
              const labelFont = Math.max(
                10,
                Math.min(20, Math.floor(Math.min(w / 7, h / 4))),
              )
              return (
                <g
                  key={d.name + d.kind}
                  transform={`translate(${leaf.x0},${leaf.y0})`}
                  className="cursor-pointer"
                  onClick={() => onSelect(d.name)}
                >
                  <rect
                    width={w}
                    height={h}
                    fill={fill}
                    fillOpacity={opacity}
                    stroke="var(--bg-panel)"
                    strokeWidth={1}
                  />
                  {showLabels && (
                    <>
                      <text
                        x={6}
                        y={12}
                        fontSize={10}
                        fontFamily="GeistMono, ui-monospace, monospace"
                        fill="var(--text-primary)"
                      >
                        {d.name}
                      </text>
                      <text
                        x={w / 2}
                        y={h / 2 + labelFont / 3}
                        textAnchor="middle"
                        fontSize={labelFont}
                        fontFamily="GeistMono, ui-monospace, monospace"
                        fill="var(--text-primary)"
                        opacity={0.95}
                      >
                        {bigVal}
                      </text>
                      <text
                        x={w - 6}
                        y={h - 6}
                        textAnchor="end"
                        fontSize={10}
                        fontFamily="GeistMono, ui-monospace, monospace"
                        fill={up ? 'var(--green)' : 'var(--red)'}
                        opacity={1}
                      >
                        {fmtPct(d.dayChange)}
                      </text>
                    </>
                  )}
                  <title>
                    {`${d.name} (${d.kind})\n${SORT_LABELS[sortBy]}: ${bigVal}\nChange: ${fmtPct(d.dayChange)}\nMark: ${d.markPx}`}
                  </title>
                </g>
              )
            })}
          </svg>
        )}
        {(!ctxs || data.length === 0) && (
          <div className="absolute inset-0 flex items-center justify-center text-text-secondary text-[11px] uppercase tracking-wider">
            {ctxs ? 'NO MATCHES' : 'LOADING…'}
          </div>
        )}
      </div>
    </div>
  )
}
