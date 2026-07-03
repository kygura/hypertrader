import { useMemo } from 'react'
import { useMeta } from '../../hooks/useMeta'
import type { TreemapSortBy } from './MarketTreemap'

interface Props {
  onMetricClick?: (sortBy: TreemapSortBy | null) => void
}

function fmtCompact(n: number): string {
  if (!isFinite(n) || n === 0) return '$0'
  const abs = Math.abs(n)
  if (abs >= 1e9) return '$' + (n / 1e9).toFixed(2) + 'B'
  if (abs >= 1e6) return '$' + (n / 1e6).toFixed(2) + 'M'
  if (abs >= 1e3) return '$' + (n / 1e3).toFixed(2) + 'K'
  return '$' + n.toFixed(2)
}

function fmtPct(n: number, digits = 2): string {
  const sign = n >= 0 ? '+' : ''
  return `${sign}${(n * 100).toFixed(digits)}%`
}

export function MarketStatsStrip({ onMetricClick }: Props) {
  const { ctxs } = useMeta()

  const stats = useMemo(() => {
    if (!ctxs || ctxs.length === 0) return null
    const perps = ctxs.filter((c) => c.kind === 'PERP')
    const spots = ctxs.filter((c) => c.kind === 'SPOT')
    const totalOI = perps.reduce((s, c) => s + c.openInterest * c.markPx, 0)
    const totalVol = ctxs.reduce((s, c) => s + c.dayNtlVlm, 0)

    let topGain: typeof ctxs[number] | null = null
    let topLoss: typeof ctxs[number] | null = null
    let topFund: typeof ctxs[number] | null = null
    for (const c of ctxs) {
      if (c.dayChange != null && (!topGain || c.dayChange > topGain.dayChange))
        topGain = c
      if (c.dayChange != null && (!topLoss || c.dayChange < topLoss.dayChange))
        topLoss = c
    }
    for (const c of perps) {
      if (c.funding != null) {
        if (!topFund || Math.abs(c.funding) > Math.abs(topFund.funding))
          topFund = c
      }
    }

    return {
      totalOI,
      totalVol,
      perpCount: perps.length,
      spotCount: spots.length,
      topGain,
      topLoss,
      topFund,
    }
  }, [ctxs])

  if (!stats) {
    return (
      <div className="h-full flex items-center justify-center text-text-secondary text-[11px] uppercase tracking-wider">
        LOADING…
      </div>
    )
  }

  const Chip = ({
    label,
    value,
    sub,
    valueClass,
    onClick,
  }: {
    label: string
    value: string
    sub?: string
    valueClass?: string
    onClick?: () => void
  }) => (
    <button
      onClick={onClick}
      className="flex flex-col items-start gap-0.5 px-3 py-2 border border-border bg-panel-alt hover:bg-elevated transition-colors min-w-[140px] flex-shrink-0 text-left"
    >
      <span className="text-[10px] uppercase tracking-wider text-text-secondary font-medium">
        {label}
      </span>
      <div className="flex items-baseline gap-2">
        <span className={`font-mono tabular-nums text-[13px] ${valueClass ?? 'text-text-primary'}`}>
          {value}
        </span>
        {sub && (
          <span className="font-mono tabular-nums text-[10px] text-text-secondary">
            {sub}
          </span>
        )}
      </div>
    </button>
  )

  return (
    <div className="h-full overflow-x-auto overflow-y-hidden">
      <div className="flex items-center gap-2 px-2 py-2 h-full min-w-min">
        <Chip
          label="TOTAL OI"
          value={fmtCompact(stats.totalOI)}
          onClick={() => onMetricClick?.('oi')}
        />
        <Chip
          label="VOLUME 24H"
          value={fmtCompact(stats.totalVol)}
          onClick={() => onMetricClick?.('volume')}
        />
        <Chip
          label="PERPS"
          value={String(stats.perpCount)}
          onClick={() => onMetricClick?.(null)}
        />
        <Chip
          label="SPOTS"
          value={String(stats.spotCount)}
          onClick={() => onMetricClick?.(null)}
        />
        {stats.topGain && (
          <Chip
            label="TOP GAINER"
            value={stats.topGain.name}
            sub={fmtPct(stats.topGain.dayChange)}
            valueClass="text-green"
            onClick={() => onMetricClick?.('change')}
          />
        )}
        {stats.topLoss && (
          <Chip
            label="TOP LOSER"
            value={stats.topLoss.name}
            sub={fmtPct(stats.topLoss.dayChange)}
            valueClass="text-red"
            onClick={() => onMetricClick?.('change')}
          />
        )}
        {stats.topFund && (
          <Chip
            label="HIGHEST FUNDING"
            value={stats.topFund.name}
            sub={`${(stats.topFund.funding * 100).toFixed(4)}%`}
            valueClass={stats.topFund.funding >= 0 ? 'text-green' : 'text-red'}
            onClick={() => onMetricClick?.('funding')}
          />
        )}
      </div>
    </div>
  )
}
