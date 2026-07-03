import { useState } from 'react'
import type { Branch } from '../../lib/types'
import { PositionsTable } from './PositionsTable'
import { OrdersTable } from './OrdersTable'
import { FillsTable } from './FillsTable'
import { TradesTable } from './TradesTable'
import { BalancesTable } from './BalancesTable'
import { PerformanceTable } from './PerformanceTable'

type Tab = 'POSITIONS' | 'BALANCES' | 'ORDERS' | 'FILLS' | 'TRADES' | 'PERFORMANCE'

interface Props {
  branch: Branch
  onOpenModal: (asset: string) => void
  onClose: (positionId: string) => void
  onDelete: (positionId: string) => void
  onCancelOrder: (orderId: string) => void
}

export function LedgerTabs({
  branch,
  onOpenModal,
  onClose,
  onDelete,
  onCancelOrder,
}: Props) {
  const [tab, setTab] = useState<Tab>('POSITIONS')

  return (
    <div className="bg-panel border border-border flex flex-col min-h-[260px] h-full">
      <div className="flex items-end border-b border-border bg-panel-alt">
        {(['POSITIONS', 'BALANCES', 'ORDERS', 'FILLS', 'TRADES', 'PERFORMANCE'] as Tab[]).map(
          (t) => (
            <button
              key={t}
              onClick={() => setTab(t)}
              className={`px-3 py-2 text-[11px] uppercase tracking-wider ${
                tab === t
                  ? 'text-text-primary tab-active'
                  : 'text-text-secondary hover:text-text-primary'
              }`}
            >
              {t}
            </button>
          ),
        )}
        <div className="ml-auto pr-2 py-1">
          <button
            onClick={() => onOpenModal('BTC')}
            className="text-[10px] uppercase tracking-wider px-2 py-1 bg-elevated text-text-primary border border-border hover:bg-hover"
          >
            + Position
          </button>
        </div>
      </div>
      <div className="flex-1 min-h-0 overflow-auto">
        {tab === 'POSITIONS' && (
          <PositionsTable branch={branch} onClose={onClose} onDelete={onDelete} />
        )}
        {tab === 'BALANCES' && <BalancesTable branch={branch} />}
        {tab === 'ORDERS' && (
          <OrdersTable branch={branch} onCancel={onCancelOrder} />
        )}
        {tab === 'FILLS' && <FillsTable branch={branch} />}
        {tab === 'TRADES' && <TradesTable branch={branch} />}
        {tab === 'PERFORMANCE' && <PerformanceTable branch={branch} />}
      </div>
    </div>
  )
}
