import type { Branch } from '../../lib/types'
import { DirChip, Td, Th } from './PositionsTable'
import {
  fmtDateTime,
  fmtLev,
  fmtPrice,
  fmtSize,
  fmtUsd,
} from '../../lib/metric-fmt'

interface Props {
  branch: Branch
  onCancel: (orderId: string) => void
}

export function OrdersTable({ branch, onCancel }: Props) {
  const orders = branch.pendingOrders ?? []
  if (orders.length === 0) {
    return (
      <div className="h-full flex items-center justify-center text-text-secondary text-[11px] uppercase tracking-wider">
        No pending orders
      </div>
    )
  }
  return (
    <table className="w-full text-[11px] font-mono tabular-nums">
      <thead className="sticky top-0 bg-panel-alt border-b border-border z-10">
        <tr className="text-[10px] uppercase tracking-wider text-text-secondary">
          <Th>Asset</Th>
          <Th>Type</Th>
          <Th>Side</Th>
          <Th>Lev</Th>
          <Th align="right">Size</Th>
          <Th align="right">Price</Th>
          <Th align="right">Value</Th>
          <Th align="right">Margin</Th>
          <Th align="right">Placed</Th>
          <Th />
        </tr>
      </thead>
      <tbody>
        {orders.map((o) => (
          <tr key={o.id} className="border-b border-border-subtle hover:bg-hover">
            <Td>{o.asset}</Td>
            <Td className="uppercase text-text-secondary">{o.type}</Td>
            <Td>
              <DirChip side={o.side} />
            </Td>
            <Td>{fmtLev(o.leverage)}</Td>
            <Td align="right">{fmtSize(o.size, 4)}</Td>
            <Td align="right">{fmtPrice(o.price)}</Td>
            <Td align="right">{fmtUsd(o.size * o.price)}</Td>
            <Td align="right">{fmtUsd(o.marginUsd)}</Td>
            <Td align="right" className="text-text-secondary">
              {fmtDateTime(o.createdAt)}
            </Td>
            <Td align="right">
              <button
                onClick={() => onCancel(o.id)}
                className="text-[10px] uppercase tracking-wider px-1.5 py-0.5 border border-border text-text-secondary hover:text-red"
              >
                Cancel
              </button>
            </Td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}
