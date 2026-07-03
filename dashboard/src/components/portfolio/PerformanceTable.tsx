import type { Branch } from '../../lib/types'
import { perfByAsset } from '../../lib/portfolio-derive'
import { Td, Th } from './PositionsTable'
import { classForPnl, fmtPct, fmtUsd } from '../../lib/metric-fmt'

interface Props {
  branch: Branch
}

export function PerformanceTable({ branch }: Props) {
  const rows = perfByAsset(branch)
  if (rows.length === 0) {
    return (
      <div className="h-full flex items-center justify-center text-text-secondary text-[11px] uppercase tracking-wider">
        No closed trades yet
      </div>
    )
  }
  return (
    <table className="w-full text-[11px] font-mono tabular-nums">
      <thead className="sticky top-0 bg-panel-alt border-b border-border z-10">
        <tr className="text-[10px] uppercase tracking-wider text-text-secondary">
          <Th>Asset</Th>
          <Th align="right">Trades</Th>
          <Th align="right">Wins</Th>
          <Th align="right">Win Rate</Th>
          <Th align="right">Volume</Th>
          <Th align="right">Realized PnL</Th>
        </tr>
      </thead>
      <tbody>
        {rows.map((r) => (
          <tr key={r.asset} className="border-b border-border-subtle hover:bg-hover">
            <Td>{r.asset}</Td>
            <Td align="right">{r.trades}</Td>
            <Td align="right">{r.wins}</Td>
            <Td align="right">{fmtPct(r.winRate, { decimals: 1 })}</Td>
            <Td align="right" className="text-text-secondary">
              {fmtUsd(r.volume, { compact: true, decimals: 2 })}
            </Td>
            <Td align="right" className={classForPnl(r.pnl)}>
              {fmtUsd(r.pnl, { sign: true })}
            </Td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}
