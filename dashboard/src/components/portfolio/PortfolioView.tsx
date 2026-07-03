import { useEffect, useMemo, useState } from 'react'
import type { Branch, BranchPosition, PendingOrder } from '../../lib/types'
import {
  addPendingOrderToBranch,
  addPositionToBranch,
  cancelPendingOrder,
  closePositionInBranch,
  deleteBranch,
  deletePositionFromBranch,
  forkBranch,
  hydrateEntryPrices,
  importBranch,
  loadBranches,
  loadSelected,
  renameBranch,
  saveBranches,
  saveSelected,
} from '../../lib/branches-store'
import { priceAt, priceAtDate } from '../../lib/price-data'
import { lastIdx } from '../../lib/portfolio-derive'
import { PortfolioSidebar } from './PortfolioSidebar'
import { PortfolioHeader } from './PortfolioHeader'
import { StatsCards } from './StatsCards'
import { PortfolioTabs } from './PortfolioTabs'
import { LedgerTabs } from './LedgerTabs'
import { PositionModal } from './PositionModal'
import { ForkDialog } from './ForkDialog'
import { ImportDialog } from './ImportDialog'
import { CommandPalette } from './CommandPalette'
import { newBranchId, pickColor } from '../../lib/branches-store'

export function PortfolioView() {
  const [branches, setBranches] = useState<Branch[]>(() => {
    const initial = loadBranches()
    return hydrateEntryPrices(initial, (asset, date) => priceAtDate(asset, date))
  })
  const [selectedId, setSelectedId] = useState<string>(
    () => loadSelected() ?? loadBranches()[0]?.id ?? '',
  )
  const [modalOpen, setModalOpen] = useState(false)
  const [modalAsset, setModalAsset] = useState<string>('BTC')
  const [forkOpen, setForkOpen] = useState(false)
  const [importOpen, setImportOpen] = useState(false)
  const [paletteOpen, setPaletteOpen] = useState(false)
  const [paletteMode, setPaletteMode] = useState<'switch' | 'create'>('switch')

  const selected = useMemo(
    () => branches.find((b) => b.id === selectedId) ?? branches[0],
    [branches, selectedId],
  )

  useEffect(() => {
    if (selected?.id) saveSelected(selected.id)
  }, [selected?.id])

  useEffect(() => {
    saveBranches(branches)
  }, [branches])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const mod = e.metaKey || e.ctrlKey
      if (!mod) return
      const key = e.key.toLowerCase()
      if (key === 'k') {
        e.preventDefault()
        setPaletteMode('switch')
        setPaletteOpen(true)
      } else if (key === 'n' && e.shiftKey) {
        e.preventDefault()
        setPaletteMode('create')
        setPaletteOpen(true)
      } else if (key === '[' || (key === 'arrowup' && e.shiftKey)) {
        e.preventDefault()
        const i = branches.findIndex((b) => b.id === selectedId)
        const next = branches[(i - 1 + branches.length) % branches.length]
        if (next) setSelectedId(next.id)
      } else if (key === ']' || (key === 'arrowdown' && e.shiftKey)) {
        e.preventDefault()
        const i = branches.findIndex((b) => b.id === selectedId)
        const next = branches[(i + 1) % branches.length]
        if (next) setSelectedId(next.id)
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [branches, selectedId])

  if (!selected) {
    return (
      <div className="h-full w-full flex items-center justify-center text-text-secondary text-[11px] uppercase tracking-wider">
        No portfolio loaded
      </div>
    )
  }

  const onSelect = (id: string) => setSelectedId(id)

  const onAddPosition = (
    pos: Omit<BranchPosition, 'id'>,
  ) => {
    setBranches((bs) => addPositionToBranch(bs, selected.id, pos))
  }

  const onAddOrder = (order: Omit<PendingOrder, 'id'>) => {
    setBranches((bs) => addPendingOrderToBranch(bs, selected.id, order))
  }

  const onCancelOrder = (orderId: string) => {
    setBranches((bs) => cancelPendingOrder(bs, selected.id, orderId))
  }

  const onClosePosition = (positionId: string) => {
    const pos = selected.positions.find((p) => p.id === positionId)
    if (!pos) return
    const mark = priceAt(pos.asset, lastIdx())
    setBranches((bs) =>
      closePositionInBranch(bs, selected.id, positionId, mark, Date.now()),
    )
  }

  const onDeletePosition = (positionId: string) => {
    setBranches((bs) => deletePositionFromBranch(bs, selected.id, positionId))
  }

  const onRename = (name: string) => {
    setBranches((bs) => renameBranch(bs, selected.id, name))
  }

  const onForkSubmit = (name: string, forkDate: number) => {
    const child = forkBranch(selected, { name, forkDate, existing: branches })
    setBranches((bs) => {
      const next = [...bs, child]
      saveBranches(next)
      return next
    })
    setSelectedId(child.id)
    setForkOpen(false)
  }

  const onImportSubmit = (
    data: Parameters<typeof importBranch>[0],
  ) => {
    const child = importBranch(data, branches)
    setBranches((bs) => {
      const next = [...bs, child]
      saveBranches(next)
      return next
    })
    setSelectedId(child.id)
    setImportOpen(false)
  }

  const onDelete = (id: string) => {
    if (branches.length <= 1) return
    if (!confirm('Delete this portfolio?')) return
    const next = deleteBranch(branches, id)
    setBranches(next)
    if (selectedId === id) setSelectedId(next[0].id)
  }

  const openModal = (asset: string) => {
    setModalAsset(asset)
    setModalOpen(true)
  }

  const onCreatePortfolio = (name: string, startingBalance: number) => {
    const fresh: Branch = {
      id: newBranchId(),
      name,
      color: pickColor(branches),
      createdAt: Date.now(),
      startingBalance,
      positions: [],
      pendingOrders: [],
    }
    setBranches((bs) => {
      const next = [...bs, fresh]
      saveBranches(next)
      return next
    })
    setSelectedId(fresh.id)
    setPaletteOpen(false)
  }

  return (
    <div className="flex h-full w-full bg-body">
      <PortfolioSidebar
        branches={branches}
        selected={selected}
        onSelect={onSelect}
        onAdd={() => openModal('BTC')}
        onFork={() => setForkOpen(true)}
        onImport={() => setImportOpen(true)}
        onRename={onRename}
        onDelete={onDelete}
      />

      <div className="flex-1 min-w-0 min-h-0 flex flex-col overflow-auto">
        <PortfolioHeader
          branch={selected}
          onAdd={() => openModal('BTC')}
          onFork={() => setForkOpen(true)}
          onImport={() => setImportOpen(true)}
          onExport={() => {}}
          onOpenPalette={() => {
            setPaletteMode('switch')
            setPaletteOpen(true)
          }}
          onCreate={() => {
            setPaletteMode('create')
            setPaletteOpen(true)
          }}
        />

        <div className="px-3 pt-2">
          <StatsCards branch={selected} branches={branches} />
        </div>

        <div className="px-3 pt-3">
          <PortfolioTabs branch={selected} branches={branches} onSelect={onSelect} />
        </div>

        <div className="px-3 pt-3 pb-4 flex-1 min-h-0">
          <LedgerTabs
            branch={selected}
            onOpenModal={openModal}
            onClose={onClosePosition}
            onDelete={onDeletePosition}
            onCancelOrder={onCancelOrder}
          />
        </div>
      </div>

      {modalOpen && (
        <PositionModal
          branch={selected}
          initialAsset={modalAsset}
          onClose={() => setModalOpen(false)}
          onSubmitPosition={(pos) => {
            onAddPosition(pos)
            setModalOpen(false)
          }}
          onSubmitOrder={(order) => {
            onAddOrder(order)
            setModalOpen(false)
          }}
        />
      )}

      {forkOpen && (
        <ForkDialog
          branch={selected}
          onCancel={() => setForkOpen(false)}
          onSubmit={onForkSubmit}
        />
      )}

      {importOpen && (
        <ImportDialog
          onCancel={() => setImportOpen(false)}
          onSubmit={onImportSubmit}
        />
      )}

      {paletteOpen && (
        <CommandPalette
          branches={branches}
          selectedId={selected.id}
          initialMode={paletteMode}
          onClose={() => setPaletteOpen(false)}
          onSelect={(id) => {
            setSelectedId(id)
            setPaletteOpen(false)
          }}
          onCreate={onCreatePortfolio}
        />
      )}
    </div>
  )
}
