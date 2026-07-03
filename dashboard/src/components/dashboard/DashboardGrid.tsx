import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Responsive,
  WidthProvider,
  type Layout,
  type LayoutItem,
} from 'react-grid-layout/legacy'
import { MarketTreemap, type TreemapSortBy } from './MarketTreemap'
import { AssetComparison } from './AssetComparison'
import { MarketStatsStrip } from './MarketStatsStrip'
import { AssetDetail } from './AssetDetail'
import { WalletTracker } from './WalletTracker'

const ResponsiveGridLayout = WidthProvider(Responsive)

const LAYOUT_KEY = '********************'

const DEFAULT_LAYOUT: LayoutItem[] = [
  { i: 'treemap', x: 0, y: 0, w: 8, h: 14 },
  { i: 'compare', x: 8, y: 0, w: 4, h: 14 },
  { i: 'stats', x: 0, y: 14, w: 12, h: 4 },
  { i: 'wallet', x: 0, y: 18, w: 12, h: 8 },
]

function loadLayout(): LayoutItem[] {
  try {
    const raw = localStorage.getItem(LAYOUT_KEY)
    if (!raw) return DEFAULT_LAYOUT
    const parsed = JSON.parse(raw) as LayoutItem[]
    if (!Array.isArray(parsed)) return DEFAULT_LAYOUT
    const ids = new Set(DEFAULT_LAYOUT.map((l) => l.i))
    const filtered = parsed.filter((l) => ids.has(l.i))
    for (const def of DEFAULT_LAYOUT) {
      if (!filtered.find((l) => l.i === def.i)) filtered.push(def)
    }
    return filtered
  } catch {
    return DEFAULT_LAYOUT
  }
}

function DragDots() {
  return (
    <span className="inline-flex flex-col gap-[2px] cursor-grab active:cursor-grabbing">
      <span className="flex gap-[2px]">
        <span className="w-[2px] h-[2px] bg-text-secondary/60" />
        <span className="w-[2px] h-[2px] bg-text-secondary/60" />
      </span>
      <span className="flex gap-[2px]">
        <span className="w-[2px] h-[2px] bg-text-secondary/60" />
        <span className="w-[2px] h-[2px] bg-text-secondary/60" />
      </span>
      <span className="flex gap-[2px]">
        <span className="w-[2px] h-[2px] bg-text-secondary/60" />
        <span className="w-[2px] h-[2px] bg-text-secondary/60" />
      </span>
    </span>
  )
}

function PanelShell({
  title,
  right,
  children,
}: {
  title: string
  right?: React.ReactNode
  children: React.ReactNode
}) {
  return (
    <div className="panel h-full">
      <div className="panel-header">
        <div className="flex items-center gap-2">
          <span className="panel-title">{title}</span>
        </div>
        <div className="flex items-center gap-2">
          {right}
          <DragDots />
        </div>
      </div>
      <div className="panel-body">{children}</div>
    </div>
  )
}

export function DashboardGrid() {
  const [layout, setLayout] = useState<LayoutItem[]>(() => loadLayout())
  const [selectedAsset, setSelectedAsset] = useState<string | null>(null)
  const [sortBy, setSortBy] = useState<TreemapSortBy>('volume')

  const onLayoutChange = useCallback((next: Layout) => {
    const arr = next as LayoutItem[]
    setLayout(arr)
    try {
      localStorage.setItem(LAYOUT_KEY, JSON.stringify(arr))
    } catch {
      // ignore
    }
  }, [])

  const layouts = useMemo(() => ({ lg: layout, md: layout, sm: layout }), [layout])

  useEffect(() => {
    if (!selectedAsset) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setSelectedAsset(null)
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [selectedAsset])

  return (
    <div className="h-full w-full overflow-auto bg-body">
      <ResponsiveGridLayout
        className="layout"
        layouts={layouts}
        breakpoints={{ lg: 1200, md: 996, sm: 768 }}
        cols={{ lg: 12, md: 12, sm: 12 }}
        rowHeight={24}
        margin={[6, 6]}
        containerPadding={[6, 6]}
        compactType="vertical"
        draggableHandle=".panel-header"
        onLayoutChange={onLayoutChange}
      >
        <div key="treemap">
          <PanelShell title="MARKET TREEMAP">
            <MarketTreemap
              sortBy={sortBy}
              onSortByChange={setSortBy}
              onSelect={(name) => setSelectedAsset(name)}
            />
          </PanelShell>
        </div>
        <div key="compare">
          <PanelShell title="ASSET COMPARISON">
            <AssetComparison />
          </PanelShell>
        </div>
        <div key="stats">
          <PanelShell title="MARKET STATS">
            <MarketStatsStrip
              onMetricClick={(m) => {
                if (m) setSortBy(m)
              }}
            />
          </PanelShell>
        </div>
        <div key="wallet">
          <PanelShell title="WALLET TRACKER">
            <WalletTracker />
          </PanelShell>
        </div>
      </ResponsiveGridLayout>

      {selectedAsset && (
        <AssetDetail
          asset={selectedAsset}
          onClose={() => setSelectedAsset(null)}
        />
      )}
    </div>
  )
}
