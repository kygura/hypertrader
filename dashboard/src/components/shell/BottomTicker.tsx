import { useMemo } from 'react'
import { useAllMids } from '../../hooks/useHLStream'
import { useMeta } from '../../hooks/useMeta'

const PRIMARY = [
  'BTC',
  'ETH',
  'SOL',
  'HYPE',
]

export function BottomTicker() {
  const { mids } = useAllMids()
  const { ctxs } = useMeta()

  const items = useMemo(() => {
    if (!mids) return []
    const ctxMap = new Map(ctxs?.map((c) => [c.name, c]) ?? [])
    const out: Array<{ name: string; price: number; change: number }> = []
    for (const name of PRIMARY) {
      const px = Number(mids[name])
      if (!px || Number.isNaN(px)) continue
      const ctx = ctxMap.get(name)
      const change = ctx ? ctx.dayChange : 0
      out.push({ name, price: px, change })
    }
    return out
  }, [mids, ctxs])

  if (!items.length) {
    return (
      <div className="h-7 flex-shrink-0 border-t border-border bg-panel-alt flex items-center px-3 text-[10px] uppercase tracking-wider text-text-secondary">
        connecting to hyperliquid…
      </div>
    )
  }

  const doubled = [...items, ...items]

  return (
    <div className="h-7 flex-shrink-0 border-t border-border bg-panel-alt overflow-hidden relative">
      <div
        className="flex items-center gap-6 absolute inset-0 px-3 whitespace-nowrap"
        style={{
          animation: 'ticker-scroll 90s linear infinite',
          willChange: 'transform',
        }}
      >
        {doubled.map((it, i) => (
          <TickerItem key={`${it.name}-${i}`} {...it} />
        ))}
      </div>
      <style>{`
        @keyframes ticker-scroll {
          0% { transform: translateX(0); }
          100% { transform: translateX(-50%); }
        }
      `}</style>
    </div>
  )
}

function TickerItem({
  name,
  price,
  change,
}: {
  name: string
  price: number
  change: number
}) {
  const up = change >= 0
  return (
    <div className="flex items-center gap-2 text-[11px] font-mono tabular-nums leading-none">
      <span className="text-text-secondary tracking-wide">{name}</span>
      <span className="text-text-primary">{formatPrice(price)}</span>
      <span className={up ? 'text-green' : 'text-red'}>
        {up ? '+' : ''}
        {(change * 100).toFixed(2)}%
      </span>
    </div>
  )
}

function formatPrice(p: number): string {
  if (p >= 1000) return p.toLocaleString(undefined, { maximumFractionDigits: 2 })
  if (p >= 1) return p.toFixed(3)
  if (p >= 0.01) return p.toFixed(4)
  return p.toFixed(6)
}
