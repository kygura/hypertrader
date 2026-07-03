import { useMemo, useState } from 'react'
import type { DayPnl } from '../../lib/portfolio-derive'
import { fmtPct, fmtUsd } from '../../lib/metric-fmt'

interface Props {
  series: DayPnl[]
}

const DAY_MS = 24 * 60 * 60 * 1000

export function CalendarHeatmap({ series }: Props) {
  const [hover, setHover] = useState<{ x: number; y: number; d: DayPnl } | null>(null)

  const { grid, monthLabels, maxAbs } = useMemo(() => {
    if (!series.length)
      return { grid: [] as Array<Array<DayPnl | null>>, monthLabels: [] as Array<{ x: number; label: string }>, maxAbs: 1 }

    const first = new Date(series[0].date)
    // Align start to the previous Sunday (day 0)
    const startMs = series[0].date - first.getUTCDay() * DAY_MS
    const lastDate = series[series.length - 1].date
    const totalDays = Math.ceil((lastDate - startMs) / DAY_MS) + 1
    const totalWeeks = Math.ceil(totalDays / 7)

    const byDate = new Map<number, DayPnl>()
    for (const d of series) {
      const k = Math.floor(d.date / DAY_MS) * DAY_MS
      byDate.set(k, d)
    }

    const grid: Array<Array<DayPnl | null>> = []
    for (let r = 0; r < 7; r++) grid.push(new Array(totalWeeks).fill(null))

    let maxAbs = 0
    for (let w = 0; w < totalWeeks; w++) {
      for (let d = 0; d < 7; d++) {
        const dayMs = startMs + (w * 7 + d) * DAY_MS
        const aligned = Math.floor(dayMs / DAY_MS) * DAY_MS
        const v = byDate.get(aligned)
        if (v) {
          grid[d][w] = v
          if (Math.abs(v.pnl) > maxAbs) maxAbs = Math.abs(v.pnl)
        }
      }
    }

    const monthLabels: Array<{ x: number; label: string }> = []
    let curMonth = -1
    for (let w = 0; w < totalWeeks; w++) {
      const cellMs = startMs + w * 7 * DAY_MS
      const m = new Date(cellMs).getUTCMonth()
      if (m !== curMonth) {
        curMonth = m
        monthLabels.push({
          x: w,
          label: new Date(cellMs)
            .toLocaleDateString(undefined, { month: 'short' })
            .toUpperCase(),
        })
      }
    }

    return { grid, monthLabels, maxAbs: maxAbs || 1 }
  }, [series])

  const colorFor = (d: DayPnl | null): string => {
    if (!d) return 'rgba(255,255,255,0.03)'
    const v = d.pnl
    if (v === 0) return 'rgba(255,255,255,0.06)'
    const intensity = Math.min(Math.abs(v) / maxAbs, 1)
    const bucket =
      intensity > 0.75 ? 0.9 : intensity > 0.5 ? 0.65 : intensity > 0.25 ? 0.4 : 0.2
    const base = v > 0 ? '56, 166, 124' : '188, 38, 62'
    return `rgba(${base}, ${bucket.toFixed(2)})`
  }

  const cellSize = 12
  const gap = 2
  const dayLabels = ['', 'Mon', '', 'Wed', '', 'Fri', '']

  return (
    <div className="relative h-full overflow-auto p-2">
      <div className="inline-flex flex-col">
        <div
          className="grid"
          style={{
            gridTemplateColumns: `28px repeat(${grid[0]?.length ?? 0}, ${cellSize}px)`,
            columnGap: `${gap}px`,
          }}
        >
          <div />
          {monthLabels.map((m, i) => {
            const next = monthLabels[i + 1]?.x ?? grid[0]?.length ?? 0
            const span = next - m.x
            return (
              <div
                key={`${m.label}-${i}`}
                style={{ gridColumn: `${m.x + 2} / span ${span}` }}
                className="text-[9px] uppercase tracking-wider text-text-secondary"
              >
                {m.label}
              </div>
            )
          })}
        </div>

        <div
          className="grid"
          style={{
            gridTemplateColumns: `28px repeat(${grid[0]?.length ?? 0}, ${cellSize}px)`,
            gridTemplateRows: `repeat(7, ${cellSize}px)`,
            columnGap: `${gap}px`,
            rowGap: `${gap}px`,
          }}
        >
          {grid.flatMap((row, r) => [
            <div
              key={`label-${r}`}
              style={{ gridColumn: 1, gridRow: r + 1 }}
              className="text-[9px] text-text-secondary leading-[12px]"
            >
              {dayLabels[r]}
            </div>,
            ...row.map((d, w) => (
              <div
                key={`${r}-${w}`}
                style={{
                  gridColumn: w + 2,
                  gridRow: r + 1,
                  width: cellSize,
                  height: cellSize,
                  background: colorFor(d),
                }}
                onMouseEnter={(e) => {
                  if (!d) return
                  const rect = (
                    e.currentTarget as HTMLDivElement
                  ).getBoundingClientRect()
                  const parent = (
                    e.currentTarget as HTMLDivElement
                  ).offsetParent as HTMLElement | null
                  const pRect = parent?.getBoundingClientRect() ?? { left: 0, top: 0 }
                  setHover({ x: rect.left - pRect.left, y: rect.top - pRect.top, d })
                }}
                onMouseLeave={() => setHover(null)}
                className="cursor-default"
              />
            )),
          ])}
        </div>

        <div className="flex items-center gap-1 mt-2 text-[9px] uppercase tracking-wider text-text-secondary">
          <span>Less</span>
          {[0.2, 0.4, 0.65, 0.9].map((b) => (
            <span
              key={`r-${b}`}
              style={{
                width: cellSize,
                height: cellSize,
                background: `rgba(188, 38, 62, ${b})`,
              }}
            />
          ))}
          <span
            style={{
              width: cellSize,
              height: cellSize,
              background: 'rgba(255,255,255,0.06)',
            }}
          />
          {[0.2, 0.4, 0.65, 0.9].map((b) => (
            <span
              key={`g-${b}`}
              style={{
                width: cellSize,
                height: cellSize,
                background: `rgba(56, 166, 124, ${b})`,
              }}
            />
          ))}
          <span>More</span>
        </div>
      </div>

      {hover && (
        <div
          className="absolute pointer-events-none z-10 bg-panel-alt border border-border px-2 py-1 text-[10px] font-mono tabular-nums text-text-primary"
          style={{
            left: hover.x + 16,
            top: hover.y + 16,
            whiteSpace: 'nowrap',
          }}
        >
          <div className="text-text-secondary uppercase tracking-wider text-[9px]">
            {new Date(hover.d.date).toLocaleDateString(undefined, {
              weekday: 'short',
              month: 'short',
              day: 'numeric',
            })}
          </div>
          <div>
            {fmtUsd(hover.d.pnl, { sign: true })} ({fmtPct(hover.d.pnlPct, { sign: true })})
          </div>
        </div>
      )}
    </div>
  )
}
