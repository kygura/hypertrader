import type { Branch } from '../../lib/types'
import { fills } from '../../lib/portfolio-derive'
import { DirChip, Td, Th } from './PositionsTable'
import {
  fmtDateTime,
  fmtPrice,
  fmtSize,
  fmtUsd,
} from '../../lib/metric-fmt'

interface Props {
  branch: Branch
}

export function FillsTable({ branch }: Props) {
  const rows = fills(branch)
  if (rows.length === 0) {
    return (
      <div className="h-full flex items-center justify-center text-text-secondary text-[11px] uppercase tracking-wider">
        No fills
      </div>
    )
  }
  return (
    <table className="w-full text-[11px] font-mono tabular-nums">
      <thead className="sticky top-0 bg-panel-alt border-b border-border z-10">
        <tr className="text-[10px] uppercase tracking-wider text-text-secondary">
          <Th align="right">Time</Th>
          <Th>Asset</Th>
          <Th>Side</Th>
          <Th>Kind</Th>
          <Th align="right">Price</Th>
          <Th align="right">Size</Th>
          <Th align="right">Value</Th>
          <Th align="right">Fee</Th>
        </tr>
      </thead>
      <tbody>
        {rows.map((f) => (
          <tr key={f.id} className="border-b border-border-subtle hover:bg-hover">
            <Td align="right" className="text-text-secondary">
              {fmtDateTime(f.time)}
            </Td>
            <Td>{f.asset}</Td>
            <Td>
              <DirChip side={f.side} />
            </Td>
            <Td>
              <KindChip kind={f.kind} />
            </Td>
            <Td align="right">{fmtPrice(f.price)}</Td>
            <Td align="right">{fmtSize(f.size, 4)}</Td>
            <Td align="right">{fmtUsd(f.value)}</Td>
            <Td align="right" className="text-text-secondary">
              {fmtUsd(f.fee, { decimals: 4 })}
            </Td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}

function KindChip({ kind }: { kind: 'OPEN' | 'CLOSE' | 'LIQ' }) {
  const cls =
    kind === 'OPEN'
      ? 'bg-amber/15 text-amber'
      : kind === 'CLOSE'
        ? 'bg-text-secondary/15 text-text-secondary'
        : 'bg-red/20 text-red'
  return (
    <span className={`inline-block px-1.5 py-0.5 text-[9px] uppercase tracking-wider ${cls}`}>
      {kind}
    </span>
  )
}
