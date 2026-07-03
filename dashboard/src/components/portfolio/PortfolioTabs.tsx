import { useMemo, useState } from 'react'
import {
  Area,
  AreaChart,
  CartesianGrid,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import type { Branch } from '../../lib/types'
import { computeBranchEquity } from '../../lib/margin-engine'
import { getAllSyntheticDates } from '../../lib/price-data'
import { dailyPnlSeries } from '../../lib/portfolio-derive'
import { CalendarHeatmap } from './CalendarHeatmap'
import { fmtDateShort, fmtUsd } from '../../lib/metric-fmt'

type Tab = 'PERPS' | 'COMBINED' | 'CALENDAR'
type Range = '24H' | '7D' | '30D' | '90D' | 'ALL'

interface Props {
  branch: Branch
  branches: Branch[]
  onSelect: (id: string) => void
}

export function PortfolioTabs({ branch, branches }: Props) {
  const [tab, setTab] = useState<Tab>('PERPS')
  const [range, setRange] = useState<Range>('ALL')

  const dates = getAllSyntheticDates()

  const points = useMemo(() => {
    const eq = computeBranchEquity(branch)
    const firstTradeDate = branch.positions.length
      ? Math.min(...branch.positions.map((p) => p.entryDate))
      : dates[0]
    const startIdx = dates.findIndex((d) => d >= firstTradeDate)
    const from = startIdx >= 0 ? startIdx : 0
    return dates.slice(from).map((d, i) => ({ t: d, v: eq[from + i] }))
  }, [branch, dates])

  const allBranchSeries = useMemo(() => {
    const firstDate = branches.reduce((min, b) => {
      if (!b.positions.length) return min
      return Math.min(min, ...b.positions.map((p) => p.entryDate))
    }, Infinity)
    const startIdx = isFinite(firstDate) ? Math.max(0, dates.findIndex((d) => d >= firstDate)) : 0
    const merged = new Map<number, Record<string, number>>()
    for (const b of branches) {
      const eq = computeBranchEquity(b)
      for (let i = startIdx; i < dates.length; i++) {
        const row = merged.get(dates[i]) ?? { t: dates[i] }
        row[b.id] = eq[i]
        merged.set(dates[i], row)
      }
    }
    return [...merged.values()].sort((a, b) => (a.t as number) - (b.t as number))
  }, [branches, dates])

  const rangedPoints = useMemo(() => {
    if (range === 'ALL') return points
    const days = range === '24H' ? 1 : range === '7D' ? 7 : range === '30D' ? 30 : 90
    return points.slice(-days)
  }, [points, range])

  const perfColor = useMemo(() => {
    const first = rangedPoints[0]?.v ?? branch.startingBalance
    const last = rangedPoints[rangedPoints.length - 1]?.v ?? first
    return last >= first ? 'var(--green)' : 'var(--red)'
  }, [rangedPoints, branch.startingBalance])

  return (
    <div className="bg-panel border border-border rounded-md overflow-hidden">
      <div className="flex items-end justify-between border-b border-border bg-panel-alt">
        <div className="flex">
          {(['PERPS', 'COMBINED', 'CALENDAR'] as Tab[]).map((t) => (
            <button
              key={t}
              onClick={() => setTab(t)}
              className={`px-3 py-2 text-[11px] uppercase tracking-wider ${
                tab === t
                  ? 'text-text-primary tab-active'
                  : 'text-text-secondary hover:text-text-primary'
              }`}
            >
              {t}
            </button>
          ))}
        </div>
        {tab !== 'CALENDAR' && (
          <div className="flex pr-2 py-1 gap-1">
            {(['24H', '7D', '30D', '90D', 'ALL'] as Range[]).map((r) => (
              <button
                key={r}
                onClick={() => setRange(r)}
                className={`text-[10px] uppercase tracking-wider px-2 py-0.5 border ${
                  range === r
                    ? 'border-border bg-elevated text-text-primary'
                    : 'border-transparent text-text-secondary hover:text-text-primary'
                }`}
              >
                {r}
              </button>
            ))}
          </div>
        )}
      </div>

      <div className="h-[260px] p-2">
        {tab === 'PERPS' && (
          <EquityArea data={rangedPoints} color={perfColor} />
        )}
        {tab === 'COMBINED' && (
          <CombinedChart data={allBranchSeries} branches={branches} selectedId={branch.id} />
        )}
        {tab === 'CALENDAR' && <CalendarHeatmap series={dailyPnlSeries(branch)} />}
      </div>
    </div>
  )
}

function EquityArea({
  data,
  color,
}: {
  data: Array<{ t: number; v: number }>
  color: string
}) {
  const gradId = `eqgrad-${color.replace(/[^a-z0-9]/gi, '')}`
  return (
    <ResponsiveContainer width="100%" height="100%">
      <AreaChart data={data} margin={{ top: 4, right: 8, left: 0, bottom: 0 }}>
        <defs>
          <linearGradient id={gradId} x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor={color} stopOpacity={0.35} />
            <stop offset="100%" stopColor={color} stopOpacity={0} />
          </linearGradient>
        </defs>
        <CartesianGrid stroke="rgba(255,255,255,0.04)" />
        <XAxis
          dataKey="t"
          type="number"
          domain={['dataMin', 'dataMax']}
          tickFormatter={(t) => fmtDateShort(t as number)}
          stroke="var(--text-secondary)"
          tick={{
            fontSize: 10,
            fontFamily: 'GeistMono, ui-monospace, monospace',
            fill: 'var(--text-secondary)',
          }}
        />
        <YAxis
          stroke="var(--text-secondary)"
          width={64}
          tickFormatter={(v) => fmtUsd(v as number, { compact: true, decimals: 0 })}
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
          formatter={(v) => fmtUsd(v as number)}
        />
        <Area
          type="monotone"
          dataKey="v"
          stroke={color}
          strokeWidth={1.6}
          fill={`url(#${gradId})`}
          isAnimationActive={false}
        />
      </AreaChart>
    </ResponsiveContainer>
  )
}

function CombinedChart({
  data,
  branches,
  selectedId,
}: {
  data: Array<Record<string, number>>
  branches: Branch[]
  selectedId: string
}) {
  return (
    <ResponsiveContainer width="100%" height="100%">
      <LineChart data={data} margin={{ top: 4, right: 8, left: 0, bottom: 0 }}>
        <CartesianGrid stroke="rgba(255,255,255,0.04)" />
        <XAxis
          dataKey="t"
          type="number"
          domain={['dataMin', 'dataMax']}
          tickFormatter={(t) => fmtDateShort(t as number)}
          stroke="var(--text-secondary)"
          tick={{
            fontSize: 10,
            fontFamily: 'GeistMono, ui-monospace, monospace',
            fill: 'var(--text-secondary)',
          }}
        />
        <YAxis
          stroke="var(--text-secondary)"
          width={64}
          tickFormatter={(v) => fmtUsd(v as number, { compact: true, decimals: 0 })}
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
          formatter={(v, name) => {
            const id = name as string
            const b = branches.find((x) => x.id === id)
            return [fmtUsd(v as number), b?.name ?? id]
          }}
        />
        {branches.map((b) => (
          <Line
            key={b.id}
            type="monotone"
            dataKey={b.id}
            stroke={b.color}
            strokeWidth={b.id === selectedId ? 2 : 1}
            strokeOpacity={b.id === selectedId ? 1 : 0.35}
            dot={false}
            isAnimationActive={false}
          />
        ))}
      </LineChart>
    </ResponsiveContainer>
  )
}
