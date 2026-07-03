import type { Branch } from '../../lib/types'
import { downloadBranchFile } from '../../lib/file-store'

interface Props {
  branch: Branch
  onAdd: () => void
  onFork: () => void
  onImport: () => void
  onExport: () => void
  onOpenPalette: () => void
  onCreate: () => void
}

export function PortfolioHeader({
  branch,
  onAdd,
  onFork,
  onImport,
  onOpenPalette,
  onCreate,
}: Props) {
  return (
    <div className="px-3 py-2 flex items-center gap-2 border-b border-border bg-panel-alt">
      <button
        onClick={onOpenPalette}
        className="flex items-center gap-2 px-2 py-1 border border-border bg-panel hover:bg-hover text-left min-w-[180px]"
        title="Switch portfolio (⌘K)"
      >
        <span
          className="inline-block w-2 h-2"
          style={{ background: branch.color }}
        />
        <span className="text-[12px] font-medium text-text-primary truncate">
          {branch.name}
        </span>
        <span className="ml-auto text-text-secondary text-[10px]">▾</span>
      </button>
      <button
        onClick={onCreate}
        className="text-[10px] uppercase tracking-wider px-2 py-1 border border-border bg-panel text-text-secondary hover:text-text-primary"
        title="Create new portfolio (⌘⇧N)"
      >
        +
      </button>
      <span className="ml-2 text-[9px] uppercase tracking-wider text-text-secondary hidden md:inline">
        <KbdHint k="⌘K" l="switch" />
        <span className="mx-2 text-text-secondary/40">·</span>
        <KbdHint k="⌘⇧N" l="new" />
        <span className="mx-2 text-text-secondary/40">·</span>
        <KbdHint k="⌘]" l="next" />
      </span>
      <div className="ml-auto flex items-center gap-1">
        <ActionButton onClick={onAdd} primary>
          + Position
        </ActionButton>
        <ActionButton onClick={onFork}>Fork</ActionButton>
        <ActionButton onClick={onImport}>↑ Import</ActionButton>
        <ActionButton onClick={() => downloadBranchFile([branch], `${branch.name}.json`)}>
          ↓ Export
        </ActionButton>
      </div>
    </div>
  )
}

function KbdHint({ k, l }: { k: string; l: string }) {
  return (
    <span className="inline-flex items-center gap-1">
      <span className="px-1 py-0.5 border border-border text-text-primary font-mono text-[9px]">
        {k}
      </span>
      <span>{l}</span>
    </span>
  )
}

function ActionButton({
  children,
  onClick,
  primary,
}: {
  children: React.ReactNode
  onClick: () => void
  primary?: boolean
}) {
  return (
    <button
      onClick={onClick}
      className={`text-[10px] uppercase tracking-wider px-2 py-1 border border-border ${
        primary
          ? 'bg-elevated text-text-primary hover:bg-hover'
          : 'bg-panel text-text-secondary hover:text-text-primary'
      }`}
    >
      {children}
    </button>
  )
}
