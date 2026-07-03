import { useMemo, useState } from 'react'
import type { Branch } from '../../lib/types'
import { equityAtDate } from '../../lib/margin-engine'
import { getAllSyntheticDates } from '../../lib/price-data'
import { fmtUsd } from '../../lib/metric-fmt'

interface Props {
  branch: Branch
  onCancel: () => void
  onSubmit: (name: string, forkDate: number) => void
}

export function ForkDialog({ branch, onCancel, onSubmit }: Props) {
  const dates = getAllSyntheticDates()
  const min = dates[0]
  const max = dates[dates.length - 1]
  const defaultDate = dates[Math.floor(dates.length / 2)]
  const [name, setName] = useState(`${branch.name}-fork`)
  const [dateMs, setDateMs] = useState<number>(defaultDate)

  const startingBalance = useMemo(
    () => equityAtDate(branch, dateMs),
    [branch, dateMs],
  )

  const dateStr = new Date(dateMs).toISOString().slice(0, 10)
  const minStr = new Date(min).toISOString().slice(0, 10)
  const maxStr = new Date(max).toISOString().slice(0, 10)

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center"
      style={{ background: 'rgba(0,0,0,0.5)' }}
      onClick={onCancel}
    >
      <div
        className="bg-panel border border-border w-[360px]"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between px-3 py-2 border-b border-border bg-panel-alt">
          <span className="label">Fork Portfolio</span>
          <button
            onClick={onCancel}
            className="text-text-secondary hover:text-text-primary text-[14px]"
          >
            ×
          </button>
        </div>
        <div className="p-3 space-y-3">
          <div>
            <div className="label mb-1">Name</div>
            <input value={name} onChange={(e) => setName(e.target.value)} className="w-full" />
          </div>
          <div>
            <div className="label mb-1">Fork Date</div>
            <input
              type="date"
              min={minStr}
              max={maxStr}
              value={dateStr}
              onChange={(e) => {
                const ts = new Date(e.target.value).getTime()
                if (!isNaN(ts)) setDateMs(ts)
              }}
              className="w-full"
            />
          </div>
          <div className="flex items-center justify-between border-t border-border pt-2">
            <span className="text-[10px] uppercase tracking-wider text-text-secondary">
              Starting Balance
            </span>
            <span className="text-[12px] font-mono tabular-nums text-text-primary">
              {fmtUsd(startingBalance)}
            </span>
          </div>
        </div>
        <div className="px-3 py-2 flex gap-2 justify-end border-t border-border bg-panel-alt">
          <button
            onClick={onCancel}
            className="text-[10px] uppercase tracking-wider px-3 py-1 border border-border text-text-secondary hover:text-text-primary"
          >
            Cancel
          </button>
          <button
            onClick={() => onSubmit(name.trim() || 'fork', dateMs)}
            disabled={!name.trim()}
            className="text-[10px] uppercase tracking-wider px-3 py-1 border border-border bg-elevated text-text-primary disabled:opacity-50"
          >
            Fork
          </button>
        </div>
      </div>
    </div>
  )
}
