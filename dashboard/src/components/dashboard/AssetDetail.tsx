import { useMemo, useState } from 'react'
import {
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
  CartesianGrid,
} from 'recharts'
import { useMeta } from '../../hooks/useMeta'
import { useAllMids } from '../../hooks/useHLStream'
import { useCandles } from '../../hooks/useCandles'

type DetailInterval = '1h' | '1d'

interface Props {
  asset: string
  onClose: () => void
}

function fmtCompact(n: number): string {
  if (!isFinite(n) || n === 0) return '$0'
  const abs = Math.abs(n)
  if (abs >= 1e9) return '$' + (n / 1e9).toFixed(2) + 'B'
  if (abs >= 1e6) return '$' + (n / 1e6).toFixed(2) + 'M'
  if (abs >= 1e3) return '$' + (n / 1e3).toFixed(2) + 'K'
  return '$' + n.toFixed(2)
}

function fmtPx(n: number): string {
  if (!isFinite(n)) return '--'
  if (n >= 1000) return n.toFixed(2)
  if (n >= 1) return n.toFixed(4)
  return n.toFixed(6)
}

function fmtPct(n: number, digits = 2): string {
  const sign = n >= 0 ? '+' : ''
  return `${sign}${(n * 100).toFixed(digits)}%`
}

export function AssetDetail({ asset, onClose }: Props) {
  const { ctxs } = useMeta()
  const { mids } = useAllMids()
  const [interval, setInterval] = useState<DetailInterval>('1d')
  const { candles } = useCandles(asset, interval, 90)

  const ctx = useMemo(() => ctxs?.find((c) => c.name === asset), [ctxs, asset])
  const live = mids?.[asset] ? Number(mids[asset]) : null
  const mark = live ?? ctx?.markPx ?? 0

  const stats = useMemo(() => {
    if (!candles || candles.length === 0) {
      return { high: ctx?.dayHigh ?? null, low: ctx?.dayLow ?? null }
    }
    let high = -Infinity
    let low = Infinity
    const cutoff = Date.now() - 24 * 60 * 60 * 1000
    for (const c of candles) {
      if (c.T < cutoff) continue
      if (c.h > high) high = c.h
      if (c.l < low) low = c.l
    }
    if (!isFinite(high) || !isFinite(low)) {
      const last24 = candles.slice(-24)
      high = Math.max(...last24.map((c) => c.h))
      low = Math.min(...last24.map((c) => c.l))
    }
    return { high, low }
  }, [candles, ctx])

  const chartData = useMemo(
    () =>
      (candles ?? []).map((c) => ({
        t: c.T,
        c: c.c,
      })),
    [candles],
  )

  const Stat = ({
    label,
    value,
    valueClass,
  }: {
    label: string
    value: React.ReactNode
    valueClass?: string
  }) => (
    <div className="flex flex-col gap-0.5 px-2 py-1.5 border border-border bg-panel-alt">
      <span className="text-[10px] uppercase tracking-wider text-text-secondary font-medium">
        {label}
      </span>
      <span className={`font-mono tabular-nums text-[12px] ${valueClass ?? 'text-text-primary'}`}>
        {value}
      </span>
    </div>
  )

  return (
    <div
      className="fixed top-16 right-4 z-50 w-[420px] max-w-[calc(100vw-2rem)] bg-panel border border-border shadow-2xl flex flex-col"
      style={{ maxHeight: 'calc(100vh - 5rem)' }}
    >
      <div className="flex items-center justify-between px-3 py-2 border-b border-border bg-panel-alt">
        <div className="flex items-center gap-2">
          <span className="font-mono text-[14px] text-text-primary tabular-nums">
            {asset}
          </span>
          {ctx && (
            <span className="text-[9px] uppercase tracking-wider px-1.5 py-0.5 border border-border text-text-secondary">
              {ctx.kind}
            </span>
          )}
        </div>
        <button
          onClick={onClose}
          className="text-text-secondary hover:text-text-primary px-2 py-1 text-[14px] leading-none"
          aria-label="Close"
        >
          ×
        </button>
      </div>

      <div className="flex items-baseline justify-between px-3 py-2 border-b border-border">
        <span className="font-mono tabular-nums text-[22px] text-text-primary">
          ${fmtPx(mark)}
        </span>
        {ctx && (
          <span
            className={`font-mono tabular-nums text-[12px] ${
              ctx.dayChange >= 0 ? 'text-green' : 'text-red'
            }`}
          >
            {fmtPct(ctx.dayChange)}
          </span>
        )}
      </div>

      <div className="grid grid-cols-2 gap-1 p-2 border-b border-border">
        <Stat label="MARK" value={`$${fmtPx(mark)}`} />
        <Stat
          label={ctx?.kind === 'PERP' ? 'ORACLE' : 'INDEX'}
          value={ctx ? `$${fmtPx(ctx.oraclePx)}` : '--'}
        />
        <Stat
          label="24H HIGH"
          value={stats.high != null && isFinite(stats.high) ? `$${fmtPx(stats.high)}` : '--'}
        />
        <Stat
          label="24H LOW"
          value={stats.low != null && isFinite(stats.low) ? `$${fmtPx(stats.low)}` : '--'}
        />
        <Stat
          label="OI"
          value={ctx && ctx.kind === 'PERP' ? fmtCompact(ctx.openInterest * mark) : '--'}
        />
        <Stat label="VOLUME 24H" value={ctx ? fmtCompact(ctx.dayNtlVlm) : '--'} />
        <Stat
          label="FUNDING"
          value={
            ctx && ctx.kind === 'PERP' ? `${(ctx.funding * 100).toFixed(4)}%` : '--'
          }
          valueClass={
            ctx && ctx.kind === 'PERP'
              ? ctx.funding >= 0
                ? 'text-green'
                : 'text-red'
              : undefined
          }
        />
        <Stat label="NEXT FUNDING" value={ctx?.kind === 'PERP' ? '~1H' : '--'} />
        <Stat
          label="PREMIUM"
          value={ctx && ctx.kind === 'PERP' ? `${(ctx.premium * 100).toFixed(4)}%` : '--'}
        />
        <Stat label="MAX LEVERAGE" value={ctx?.kind === 'PERP' ? '—' : '--'} />
      </div>

      <div className="flex items-center justify-between px-2 py-1 border-b border-border bg-panel-alt">
        <span className="text-[10px] uppercase tracking-wider text-text-secondary font-medium">
          PRICE
        </span>
        <div className="flex border border-border">
          {(['1h', '1d'] as DetailInterval[]).map((iv) => (
            <button
              key={iv}
              onClick={() => setInterval(iv)}
              className={`px-2 py-0.5 text-[10px] uppercase tracking-wider ${
                interval === iv
                  ? 'bg-elevated text-text-primary'
                  : 'text-text-secondary hover:text-text-primary'
              }`}
            >
              {iv === '1h' ? '1H' : '1D'}
            </button>
          ))}
        </div>
      </div>

      <div className="h-[180px] p-2">
        {chartData.length === 0 ? (
          <div className="h-full flex items-center justify-center text-text-secondary text-[11px] uppercase tracking-wider">
            LOADING…
          </div>
        ) : (
          <ResponsiveContainer width="100%" height="100%">
            <LineChart data={chartData} margin={{ top: 4, right: 6, left: 0, bottom: 0 }}>
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
                  fontSize: 9,
                  fontFamily: 'GeistMono, ui-monospace, monospace',
                  fill: 'var(--text-secondary)',
                }}
              />
              <YAxis
                stroke="var(--text-secondary)"
                domain={['dataMin', 'dataMax']}
                width={48}
                tickFormatter={(v) => fmtPx(v as number)}
                tick={{
                  fontSize: 9,
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
                formatter={(v) => [
                  `$${fmtPx(typeof v === 'number' ? v : Number(v))}`,
                  asset,
                ]}
              />
              <Line
                type="monotone"
                dataKey="c"
                stroke="var(--red-accent)"
                strokeWidth={1.4}
                dot={false}
                isAnimationActive={false}
              />
            </LineChart>
          </ResponsiveContainer>
        )}
      </div>
    </div>
  )
}
