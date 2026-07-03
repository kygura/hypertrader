import { useState } from 'react'
import type { Branch } from '../../lib/types'
import { computeBranchMetrics } from '../../lib/margin-engine'
import {
  avgTradeDuration,
  directionBias,
  longestWinStreak,
  medianTradeDuration,
  perfByAsset,
  pnlCohort,
  sizeCohort,
  totalVolume,
  tradingStyle,
  venueEquity,
} from '../../lib/portfolio-derive'
import {
  classForPnl,
  fmtDuration,
  fmtLev,
  fmtPct,
  fmtUsd,
} from '../../lib/metric-fmt'

interface Props {
  branches: Branch[]
  selected: Branch
  onSelect: (id: string) => void
  onAdd: () => void
  onFork: () => void
  onImport: () => void
  onRename: (name: string) => void
  onDelete: (id: string) => void
}

export function PortfolioSidebar({
  branches,
  selected,
  onSelect,
  onAdd,
  onFork,
  onImport,
  onRename,
  onDelete,
}: Props) {
  const [editing, setEditing] = useState(false)
  const [nameDraft, setNameDraft] = useState(selected.name)
  const [perfWindow, setPerfWindow] = useState<'30D' | '90D' | 'ALL'>('30D')

  const m = computeBranchMetrics(selected)
  const venue = venueEquity(selected)
  const bias = directionBias(selected)
  const perf = perfByAsset(selected)
  void perf
  const winStreak = longestWinStreak(selected)
  const style = tradingStyle(selected)
  const avgDur = avgTradeDuration(selected)
  const medDur = medianTradeDuration(selected)
  const cohort = pnlCohort(selected)
  const sCohort = sizeCohort(selected)
  const vol = totalVolume(selected)

  const accountValue = m.finalValue
  const upnl = m.finalValue - selected.startingBalance

  const initials = selected.name
    .split(/\s+/)
    .map((s) => s[0]?.toUpperCase() ?? '')
    .join('')
    .slice(0, 2) || selected.name.slice(0, 2).toUpperCase()

  const submit = () => {
    const next = nameDraft.trim()
    if (next && next !== selected.name) onRename(next)
    setEditing(false)
  }

  return (
    <aside className="w-[220px] flex-shrink-0 border-r border-border bg-panel-alt flex flex-col overflow-auto">
      <div className="p-3 border-b border-border flex items-start gap-2">
        <div
          className="w-10 h-10 flex items-center justify-center text-[11px] font-mono font-semibold flex-shrink-0"
          style={{ background: selected.color, color: '#111' }}
        >
          {initials}
        </div>
        <div className="flex-1 min-w-0">
          {editing ? (
            <input
              autoFocus
              value={nameDraft}
              onChange={(e) => setNameDraft(e.target.value)}
              onBlur={submit}
              onKeyDown={(e) => {
                if (e.key === 'Enter') submit()
                if (e.key === 'Escape') {
                  setNameDraft(selected.name)
                  setEditing(false)
                }
              }}
              className="text-[12px] w-full px-1 py-0.5"
            />
          ) : (
            <button
              onClick={() => {
                setNameDraft(selected.name)
                setEditing(true)
              }}
              className="text-[12px] font-medium text-text-primary truncate w-full text-left hover:text-amber"
              title="Click to rename"
            >
              {selected.name}
            </button>
          )}
          <div className="text-[9px] uppercase tracking-wider text-text-secondary font-mono mt-0.5">
            {selected.id.slice(0, 8).toUpperCase()}
          </div>
        </div>
      </div>

      <div className="px-3 py-2 flex gap-1 border-b border-border">
        <button
          onClick={onAdd}
          className="flex-1 text-[10px] uppercase tracking-wider py-1.5 bg-elevated text-text-primary hover:bg-hover border border-border"
        >
          + Position
        </button>
        <button
          onClick={onFork}
          className="text-[10px] uppercase tracking-wider px-2 py-1.5 bg-panel text-text-secondary hover:text-text-primary border border-border"
          title="Fork portfolio"
        >
          Fork
        </button>
      </div>

      <div className="px-3 py-2 border-b border-border">
        <div className="label mb-1">Portfolios</div>
        <ul className="space-y-px">
          {branches.map((b) => {
            const mb = computeBranchMetrics(b)
            const ret = mb.totalReturn
            const isSel = b.id === selected.id
            return (
              <li key={b.id}>
                <button
                  onClick={() => onSelect(b.id)}
                  onDoubleClick={() => onDelete(b.id)}
                  className={`w-full flex items-center gap-2 px-2 py-1.5 text-left ${
                    isSel ? 'bg-elevated' : 'hover:bg-hover'
                  }`}
                  title="Double-click to delete"
                >
                  <span
                    className="w-[3px] h-4 flex-shrink-0"
                    style={{ background: b.color }}
                  />
                  <span
                    className={`flex-1 truncate text-[11px] ${
                      isSel ? 'text-text-primary' : 'text-text-muted'
                    }`}
                  >
                    {b.name}
                  </span>
                  <span
                    className={`text-[10px] font-mono tabular-nums ${classForPnl(ret)}`}
                  >
                    {fmtPct(ret, { sign: true, decimals: 1 })}
                  </span>
                </button>
              </li>
            )
          })}
        </ul>
        <button
          onClick={onImport}
          className="mt-2 w-full text-[10px] uppercase tracking-wider py-1 text-text-secondary hover:text-text-primary border border-border"
        >
          ↑ Import
        </button>
      </div>

      <div className="px-3 py-2 border-b border-border">
        <div className="label mb-1">Account Value</div>
        <div className="text-[18px] font-mono tabular-nums text-text-primary">
          {fmtUsd(accountValue)}
        </div>
        <div className={`text-[10px] font-mono tabular-nums ${classForPnl(upnl)}`}>
          {fmtUsd(upnl, { sign: true })} ({fmtPct(upnl / selected.startingBalance, { sign: true })})
        </div>
      </div>

      <div className="px-3 py-2 border-b border-border">
        <div className="label mb-1">Account Equity</div>
        <SidebarRow label="Perps" value={fmtUsd(venue.perps)} />
        <SidebarRow label="Spot" value={fmtUsd(venue.spot)} />
        <SidebarRow label="Staked" value={fmtUsd(venue.staked)} />
      </div>

      <div className="px-3 py-2 border-b border-border">
        <div className="label mb-1">Overview</div>
        <SidebarRow
          label="UPnL"
          value={fmtUsd(upnl, { sign: true })}
          colorClass={classForPnl(upnl)}
        />
        <SidebarRow label="Leverage" value={fmtLev(bias.longNtl + bias.shortNtl > 0 ? (bias.longNtl + bias.shortNtl) / accountValue : 0)} />
        <SidebarRow label="Margin Use" value={fmtPct(accountValue > 0 ? (bias.longNtl + bias.shortNtl) / accountValue : 0)} />
        <SidebarRow
          label="All-Time PNL"
          value={fmtUsd(m.finalValue - selected.startingBalance, { sign: true })}
          colorClass={classForPnl(m.finalValue - selected.startingBalance)}
        />
        <SidebarRow label="Volume" value={fmtUsd(vol, { compact: true, decimals: 1 })} />
      </div>

      <div className="px-3 py-2 border-b border-border">
        <div className="label mb-1">Analysis</div>
        <SidebarRow label="Win Streak" value={`${winStreak}`} />
        <SidebarRow label="Style" value={style} />
        <SidebarRow label="Avg Duration" value={fmtDuration(avgDur)} />
        <SidebarRow label="Median Duration" value={fmtDuration(medDur)} />
        <SidebarRow
          label="PNL Cohort"
          value={cohort}
          colorClass={
            cohort === 'Profitable'
              ? 'text-green'
              : cohort === 'Unprofitable'
                ? 'text-red'
                : 'text-text-secondary'
          }
        />
        <SidebarRow label="Size Cohort" value={sCohort} />
      </div>

      <div className="px-3 py-2">
        <div className="flex items-center justify-between mb-1">
          <div className="label">Performance</div>
          <select
            value={perfWindow}
            onChange={(e) => setPerfWindow(e.target.value as '30D' | '90D' | 'ALL')}
            className="text-[10px] py-0.5 px-1 bg-panel border border-border text-text-primary"
          >
            <option value="30D">30D</option>
            <option value="90D">90D</option>
            <option value="ALL">ALL</option>
          </select>
        </div>
        <SidebarRow
          label="Drawdown"
          value={fmtPct(m.maxDrawdown)}
          colorClass="text-red"
        />
        <SidebarRow label="Win Rate" value={fmtPct(m.winRate)} />
        <SidebarRow label="Sharpe" value={isFinite(m.sharpe) ? m.sharpe.toFixed(2) : '—'} />
      </div>
    </aside>
  )
}

function SidebarRow({
  label,
  value,
  colorClass,
}: {
  label: string
  value: string
  colorClass?: string
}) {
  return (
    <div className="flex items-center justify-between py-0.5">
      <span className="text-[10px] uppercase tracking-wider text-text-secondary">
        {label}
      </span>
      <span
        className={`text-[11px] font-mono tabular-nums ${colorClass ?? 'text-text-primary'}`}
      >
        {value}
      </span>
    </div>
  )
}
