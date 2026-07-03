import type { Branch } from '../../lib/types'
import { lastIdx, openPositions } from '../../lib/portfolio-derive'
import { notionalSize, posStateAt } from '../../lib/margin-engine'
import {
  classForPnl,
  fmtLev,
  fmtPct,
  fmtPrice,
  fmtSize,
  fmtUsd,
} from '../../lib/metric-fmt'

interface Props {
  branch: Branch
  onClose: (id: string) => void
  onDelete: (id: string) => void
}

export function PositionsTable({ branch, onClose, onDelete }: Props) {
  const open = openPositions(branch)
  const idx = lastIdx()

  if (open.length === 0) {
    return (
      <div className="h-full flex items-center justify-center text-text-secondary text-[11px] uppercase tracking-wider">
        No open positions
      </div>
    )
  }

  return (
    <table className="w-full text-[11px] font-mono tabular-nums">
      <thead className="sticky top-0 bg-panel-alt border-b border-border z-10">
        <tr className="text-[10px] uppercase tracking-wider text-text-secondary">
          <Th>Asset</Th>
          <Th>Dir</Th>
          <Th>Lev</Th>
          <Th align="right">Size</Th>
          <Th align="right">Value</Th>
          <Th align="right">Entry</Th>
          <Th align="right">Mark</Th>
          <Th align="right">Liq</Th>
          <Th align="right">Margin</Th>
          <Th align="right">PnL</Th>
          <Th align="right">PnL %</Th>
          <Th />
        </tr>
      </thead>
      <tbody>
        {open.map((p) => {
          const st = posStateAt(p, idx)
          const size = notionalSize(p)
          const rowBg = p.side === 'long' ? 'bg-green/[0.04]' : 'bg-red/[0.04]'
          return (
            <tr
              key={p.id}
              className={`border-b border-border-subtle ${rowBg} hover:bg-hover`}
            >
              <Td>{p.asset}</Td>
              <Td>
                <DirChip side={p.side} />
              </Td>
              <Td>{fmtLev(p.leverage)}</Td>
              <Td align="right">{fmtSize(size, 4)}</Td>
              <Td align="right">{fmtUsd(st.notional)}</Td>
              <Td align="right">{fmtPrice(p.entryPrice)}</Td>
              <Td align="right">{fmtPrice(st.markPrice)}</Td>
              <Td align="right" className="text-red">
                {fmtPrice(st.liqPrice)}
              </Td>
              <Td align="right">{fmtUsd(p.marginUsd)}</Td>
              <Td align="right" className={classForPnl(st.upnl)}>
                {fmtUsd(st.upnl, { sign: true })}
              </Td>
              <Td align="right" className={classForPnl(st.upnl)}>
                {fmtPct(st.pnlPct, { sign: true })}
              </Td>
              <Td align="right">
                <button
                  onClick={() => onClose(p.id)}
                  className="text-[10px] uppercase tracking-wider px-1.5 py-0.5 border border-border text-text-secondary hover:text-text-primary"
                  title="Close at mark"
                >
                  Close
                </button>
                <button
                  onClick={() => onDelete(p.id)}
                  className="text-[10px] uppercase tracking-wider px-1.5 py-0.5 ml-1 border border-border text-text-secondary hover:text-red"
                  title="Delete from history"
                >
                  ×
                </button>
              </Td>
            </tr>
          )
        })}
      </tbody>
    </table>
  )
}

export function Th({
  children,
  align = 'left',
}: {
  children?: React.ReactNode
  align?: 'left' | 'right' | 'center'
}) {
  return (
    <th
      className={`px-2 py-1.5 font-medium ${
        align === 'right' ? 'text-right' : align === 'center' ? 'text-center' : 'text-left'
      }`}
    >
      {children}
    </th>
  )
}

export function Td({
  children,
  align = 'left',
  className = '',
}: {
  children?: React.ReactNode
  align?: 'left' | 'right' | 'center'
  className?: string
}) {
  return (
    <td
      className={`px-2 py-1.5 ${
        align === 'right' ? 'text-right' : align === 'center' ? 'text-center' : 'text-left'
      } ${className}`}
    >
      {children}
    </td>
  )
}

export function DirChip({ side }: { side: 'long' | 'short' }) {
  return (
    <span
      className={`inline-block px-1.5 py-0.5 text-[9px] uppercase tracking-wider ${
        side === 'long' ? 'bg-green/15 text-green' : 'bg-red/15 text-red'
      }`}
    >
      {side === 'long' ? 'Long' : 'Short'}
    </span>
  )
}
