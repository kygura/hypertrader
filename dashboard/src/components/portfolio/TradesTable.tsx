import type { Branch } from '../../lib/types'
import { trades } from '../../lib/portfolio-derive'
import { DirChip, Td, Th } from './PositionsTable'
import {
  classForPnl,
  fmtDate,
  fmtDuration,
  fmtLev,
  fmtPct,
  fmtPrice,
  fmtSize,
  fmtUsd,
} from '../../lib/metric-fmt'

interface Props {
  branch: Branch
}

export function TradesTable({ branch }: Props) {
  const rows = trades(branch)
  if (rows.length === 0) {
    return (
      <div className="h-full flex items-center justify-center text-text-secondary text-[11px] uppercase tracking-wider">
        No closed trades
      </div>
    )
  }
  return (
    <table className="w-full text-[11px] font-mono tabular-nums">
      <thead className="sticky top-0 bg-panel-alt border-b border-border z-10">
        <tr className="text-[10px] uppercase tracking-wider text-text-secondary">
          <Th>Asset</Th>
          <Th>Side</Th>
          <Th>Lev</Th>
          <Th>Entry Date</Th>
          <Th>Exit Date</Th>
          <Th align="right">Entry</Th>
          <Th align="right">Exit</Th>
          <Th align="right">Size</Th>
          <Th align="right">Duration</Th>
          <Th align="right">PnL</Th>
          <Th align="right">PnL %</Th>
        </tr>
      </thead>
      <tbody>
        {rows.map((t) => (
          <tr
            key={t.id}
            className={`border-b border-border-subtle hover:bg-hover ${
              t.liquidated ? 'opacity-70' : ''
            }`}
          >
            <Td>
              {t.asset}
              {t.liquidated && (
                <span className="ml-1 inline-block px-1 py-0.5 text-[9px] uppercase tracking-wider bg-red/20 text-red">
                  LIQ
                </span>
              )}
            </Td>
            <Td>
              <DirChip side={t.side} />
            </Td>
            <Td>{fmtLev(t.leverage)}</Td>
            <Td className="text-text-secondary">{fmtDate(t.entryDate)}</Td>
            <Td className="text-text-secondary">{fmtDate(t.exitDate)}</Td>
            <Td align="right">{fmtPrice(t.entryPrice)}</Td>
            <Td align="right">{fmtPrice(t.exitPrice)}</Td>
            <Td align="right">{fmtSize(t.size, 4)}</Td>
            <Td align="right" className="text-text-secondary">
              {fmtDuration(t.durationMs)}
            </Td>
            <Td align="right" className={classForPnl(t.pnl)}>
              {fmtUsd(t.pnl, { sign: true })}
            </Td>
            <Td align="right" className={classForPnl(t.pnl)}>
              {fmtPct(t.pnlPct, { sign: true })}
            </Td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}
