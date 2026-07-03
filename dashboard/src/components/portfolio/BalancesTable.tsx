import type { Branch } from '../../lib/types'
import { venueEquity } from '../../lib/portfolio-derive'
import { accountStateAt } from '../../lib/margin-engine'
import { lastIdx } from '../../lib/portfolio-derive'
import { Td, Th } from './PositionsTable'
import { fmtPct, fmtUsd } from '../../lib/metric-fmt'

interface Props {
  branch: Branch
}

export function BalancesTable({ branch }: Props) {
  const ve = venueEquity(branch)
  const acct = accountStateAt(branch, lastIdx())
  const rows = [
    { name: 'Perps', value: ve.perps, available: acct.available, ratio: 1 },
    { name: 'Spot', value: ve.spot, available: 0, ratio: 0 },
    { name: 'Staked', value: ve.staked, available: 0, ratio: 0 },
  ]
  const total = ve.perps + ve.spot + ve.staked
  return (
    <table className="w-full text-[11px] font-mono tabular-nums">
      <thead className="sticky top-0 bg-panel-alt border-b border-border z-10">
        <tr className="text-[10px] uppercase tracking-wider text-text-secondary">
          <Th>Venue</Th>
          <Th align="right">Value</Th>
          <Th align="right">Available</Th>
          <Th align="right">Allocation</Th>
        </tr>
      </thead>
      <tbody>
        {rows.map((r) => (
          <tr key={r.name} className="border-b border-border-subtle hover:bg-hover">
            <Td>{r.name}</Td>
            <Td align="right">{fmtUsd(r.value)}</Td>
            <Td align="right">{fmtUsd(r.available)}</Td>
            <Td align="right" className="text-text-secondary">
              {fmtPct(total > 0 ? r.value / total : 0, { decimals: 1 })}
            </Td>
          </tr>
        ))}
        <tr className="border-t border-border bg-panel-alt">
          <Td className="text-text-primary font-medium">TOTAL</Td>
          <Td align="right" className="text-text-primary font-medium">
            {fmtUsd(total)}
          </Td>
          <Td align="right" />
          <Td align="right" />
        </tr>
      </tbody>
    </table>
  )
}
