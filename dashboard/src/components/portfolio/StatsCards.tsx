import React, { useMemo, useState } from 'react'
import type { Branch } from '../../lib/types'
import {
  directionBias,
  trades,
} from '../../lib/portfolio-derive'
import { computeBranchMetrics } from '../../lib/margin-engine'
import { getAllSyntheticDates } from '../../lib/price-data'
import {
  classForPnl,
  fmtLev,
  fmtPct,
  fmtUsd,
} from '../../lib/metric-fmt'

type PerfRange = '24H' | '7D' | '30D' | '90D' | 'ALL'

interface Props {
  branch: Branch
  branches: Branch[]
}

export function StatsCards({ branch }: Props) {
  const [perfRange, setPerfRange] = useState<PerfRange>('ALL')
  const m = useMemo(() => computeBranchMetrics(branch), [branch])
  const allTrades = useMemo(() => trades(branch).slice().reverse(), [branch]) // oldest→newest
  const tradeSeries = useMemo(() => {
    if (perfRange === 'ALL') return allTrades
    const dates = getAllSyntheticDates()
    const lastDate = dates[dates.length - 1] ?? Date.now()
    const days =
      perfRange === '24H' ? 1
      : perfRange === '7D' ? 7
      : perfRange === '30D' ? 30
      : 90
    const cutoff = lastDate - days * 86_400_000
    const filtered = allTrades.filter((t) => t.exitDate >= cutoff)
    // Fallback: if no trades in range, show last 4
    return filtered.length > 0 ? filtered : allTrades.slice(-4)
  }, [allTrades, perfRange])
  const bias = useMemo(() => directionBias(branch), [branch])

  const acctValue = m.finalValue
  const upnl = m.finalValue - branch.startingBalance
  const winRate = tradeSeries.length
    ? tradeSeries.filter((t) => t.pnl > 0).length / tradeSeries.length
    : m.winRate
  const totalTrades = tradeSeries.length

  const totalNotional = bias.longNtl + bias.shortNtl
  const leverage = acctValue > 0 ? totalNotional / acctValue : 0
  const marginUsage =
    acctValue > 0 ? Math.min((totalNotional / 5) / acctValue, 1) : 0
  // Use maintenance / equity as a margin-usage proxy
  const totalLong = bias.longNtl
  const totalShort = bias.shortNtl
  const longPct = totalNotional > 0 ? totalLong / totalNotional : 0.5
  const shortPct = totalNotional > 0 ? totalShort / totalNotional : 0.5

  const liqDistance = 100 // pct from liquidation, placeholder when no positions
  const free = Math.max(acctValue - totalNotional / Math.max(leverage, 1), 0)

  return (
    <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-4 gap-2">
      {/* Performance */}
      <Card title="Performance" right={<TimeframeChip value={perfRange} onChange={setPerfRange} />}>
        <div className="flex items-baseline gap-2">
          <span
            className={`text-[20px] font-mono tabular-nums ${classForPnl(upnl)}`}
          >
            {fmtUsd(upnl, { sign: true })}
          </span>
          <span className="text-[10px] uppercase tracking-wider text-text-secondary">
            PnL · {fmtPct(upnl / branch.startingBalance, { sign: true })}
          </span>
        </div>
        <BarSparkline data={tradeSeries.map((t) => t.pnl)} />
        <div className="flex items-center justify-between text-[10px] uppercase tracking-wider text-text-secondary mt-1">
          <span>
            Win Rate{' '}
            <span className="text-text-primary font-mono tabular-nums">
              {fmtPct(winRate, { decimals: 1 })}
            </span>
          </span>
          <span>
            Trades{' '}
            <span className="text-text-primary font-mono tabular-nums">
              {totalTrades}
            </span>
          </span>
        </div>
      </Card>

      {/* Leverage */}
      <Card title="Leverage">
        <div className="text-[20px] font-mono tabular-nums text-text-primary">
          {fmtLev(leverage)}
        </div>
        <GradientBar pct={Math.min(leverage / 20, 1)} />
        <div className="flex items-center justify-between text-[10px] uppercase tracking-wider text-text-secondary mt-1">
          <span>
            Notional{' '}
            <span className="text-text-primary font-mono tabular-nums">
              {fmtUsd(totalNotional, { compact: true, decimals: 2 })}
            </span>
          </span>
          <span>
            Equity{' '}
            <span className="text-text-primary font-mono tabular-nums">
              {fmtUsd(acctValue, { compact: true, decimals: 2 })}
            </span>
          </span>
        </div>
      </Card>

      {/* Margin Usage */}
      <Card title="Margin Usage">
        <div className="text-[20px] font-mono tabular-nums text-text-primary">
          {fmtPct(marginUsage, { decimals: 2 })}
        </div>
        <SimpleBar pct={marginUsage} />
        <div className="flex items-center justify-between text-[10px] uppercase tracking-wider text-text-secondary mt-1">
          <span>
            Free{' '}
            <span className="text-text-primary font-mono tabular-nums">
              {fmtUsd(free, { compact: true, decimals: 2 })}
            </span>
          </span>
          <span>
            From Liq{' '}
            <span className="text-text-primary font-mono tabular-nums">
              {liqDistance}%
            </span>
          </span>
        </div>
      </Card>

      {/* Direction Bias */}
      <Card title="Direction Bias">
        <div className="text-[20px] font-mono text-text-primary">
          {bias.label}
        </div>
        <SplitBar long={longPct} short={shortPct} />
        <div className="flex items-center justify-between text-[10px] uppercase tracking-wider text-text-secondary mt-1">
          <span>
            <span className="text-green font-mono tabular-nums">
              {fmtPct(longPct, { decimals: 1 })}
            </span>{' '}
            Long
          </span>
          <span>
            <span className="text-red font-mono tabular-nums">
              {fmtPct(shortPct, { decimals: 1 })}
            </span>{' '}
            Short
          </span>
        </div>
      </Card>
    </div>
  )
}

function Card({
  title,
  right,
  children,
}: {
  title: string
  right?: React.ReactNode
  children: React.ReactNode
}) {
  return (
    <div className="bg-panel border border-border rounded-md p-3 flex flex-col gap-1.5">
      <div className="flex items-center justify-between">
        <span className="label">{title}</span>
        {right}
      </div>
      {children}
    </div>
  )
}

const PERF_RANGES: PerfRange[] = ['24H', '7D', '30D', '90D', 'ALL']

function TimeframeChip({
  value,
  onChange,
}: {
  value: PerfRange
  onChange: (r: PerfRange) => void
}) {
  const [open, setOpen] = React.useState(false)
  return (
    <div className="relative">
      <button
        onClick={() => setOpen((o) => !o)}
        className="flex items-center gap-0.5 text-[10px] uppercase tracking-wider text-text-secondary hover:text-text-primary"
      >
        {value}
        <svg width="10" height="10" viewBox="0 0 10 10" fill="currentColor">
          <path d="M2 3.5L5 6.5L8 3.5" stroke="currentColor" strokeWidth="1.2" fill="none" strokeLinecap="round" strokeLinejoin="round"/>
        </svg>
      </button>
      {open && (
        <div className="absolute right-0 top-full mt-1 z-50 bg-panel border border-border rounded shadow-lg py-0.5 min-w-[56px]">
          {PERF_RANGES.map((r) => (
            <button
              key={r}
              onClick={() => { onChange(r); setOpen(false) }}
              className={`block w-full text-left px-2.5 py-1 text-[10px] uppercase tracking-wider ${
                r === value ? 'text-text-primary bg-elevated' : 'text-text-secondary hover:text-text-primary hover:bg-elevated'
              }`}
            >
              {r}
            </button>
          ))}
        </div>
      )}
    </div>
  )
}

const BAR_H = 8    // px
const BAR_GAP = 3  // px
const MIN_BAR_W = 8 // px — cap how many bars show

function BarSparkline({ data }: { data: number[] }) {
  const ref = React.useRef<HTMLDivElement>(null)
  const [containerW, setContainerW] = React.useState(300)

  React.useEffect(() => {
    const el = ref.current
    if (!el) return
    setContainerW(el.getBoundingClientRect().width)
    const obs = new ResizeObserver(([e]) => setContainerW(e.contentRect.width))
    obs.observe(el)
    return () => obs.disconnect()
  }, [])

  const maxBars = Math.max(1, Math.floor((containerW + BAR_GAP) / (MIN_BAR_W + BAR_GAP)))
  const visible = data.slice(-maxBars)

  return (
    <div
      ref={ref}
      style={{
        width: '100%',
        height: BAR_H,
        display: 'flex',
        alignItems: 'stretch',
        gap: BAR_GAP,
      }}
    >
      {visible.map((v, i) => (
        <div
          key={i}
          style={{
            flex: 1,
            height: BAR_H,
            background: v >= 0 ? 'var(--green)' : 'var(--red)',
            borderRadius: 999,
          }}
        />
      ))}
    </div>
  )
}

function GradientBar({ pct }: { pct: number }) {
  return (
    <div className="relative h-1.5 bg-elevated">
      <div
        className="absolute inset-y-0 left-0"
        style={{
          width: `${Math.max(0, Math.min(pct, 1)) * 100}%`,
          background:
            'linear-gradient(90deg, var(--green) 0%, var(--amber) 60%, var(--red) 100%)',
        }}
      />
    </div>
  )
}

function SimpleBar({ pct }: { pct: number }) {
  return (
    <div className="relative h-1.5 bg-elevated">
      <div
        className="absolute inset-y-0 left-0"
        style={{
          width: `${Math.max(0, Math.min(pct, 1)) * 100}%`,
          background: 'var(--amber)',
        }}
      />
    </div>
  )
}

function SplitBar({ long, short }: { long: number; short: number }) {
  return (
    <div className="relative h-1.5 flex bg-elevated">
      <div style={{ width: `${long * 100}%`, background: 'var(--green)' }} />
      <div style={{ width: `${short * 100}%`, background: 'var(--red)' }} />
    </div>
  )
}
