import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { Branch } from '../../lib/types'
import { computeBranchMetrics } from '../../lib/margin-engine'
import { classForPnl, fmtPct, fmtUsd } from '../../lib/metric-fmt'

type Mode = 'switch' | 'create'

interface Props {
  branches: Branch[]
  selectedId: string
  initialMode?: Mode
  onClose: () => void
  onSelect: (id: string) => void
  onCreate: (name: string, startingBalance: number) => void
}

export function CommandPalette({
  branches,
  selectedId,
  initialMode = 'switch',
  onClose,
  onSelect,
  onCreate,
}: Props) {
  const [mode, setMode] = useState<Mode>(initialMode)
  const [query, setQueryRaw] = useState('')
  const [idx, setIdx] = useState(0)
  const [name, setName] = useState('')
  const [balance, setBalance] = useState('1000000')
  const inputRef = useRef<HTMLInputElement>(null)
  const createNameRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    if (mode === 'switch') inputRef.current?.focus()
    else createNameRef.current?.focus()
  }, [mode])

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return branches
    return branches.filter((b) => b.name.toLowerCase().includes(q))
  }, [branches, query])

  const items = useMemo(
    () => [
      ...filtered.map((b) => ({ kind: 'branch' as const, branch: b })),
      { kind: 'create' as const },
    ],
    [filtered],
  )

  const setQuery = useCallback((q: string) => {
    setQueryRaw(q)
    setIdx(0)
  }, [])

  const choose = (i: number) => {
    const item = items[i]
    if (!item) return
    if (item.kind === 'create') {
      setMode('create')
      setName(query.trim() || 'New Portfolio')
      return
    }
    onSelect(item.branch.id)
  }

  const submitCreate = () => {
    const n = name.trim() || 'New Portfolio'
    const b = Math.max(1, Number(balance) || 1_000_000)
    onCreate(n, b)
  }

  return (
    <div
      className="fixed inset-0 z-[55] flex items-start justify-center pt-[12vh]"
      style={{ background: 'rgba(0,0,0,0.55)' }}
      onClick={onClose}
    >
      <div
        className="w-[480px] max-w-[92vw] bg-panel border border-border shadow-xl"
        onClick={(e) => e.stopPropagation()}
      >
        {mode === 'switch' ? (
          <>
            <div className="border-b border-border bg-panel-alt px-3 py-2 flex items-center gap-2">
              <span className="text-[10px] uppercase tracking-wider text-text-secondary">
                Portfolio
              </span>
              <input
                ref={inputRef}
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'ArrowDown') {
                    e.preventDefault()
                    setIdx((i) => Math.min(items.length - 1, i + 1))
                  } else if (e.key === 'ArrowUp') {
                    e.preventDefault()
                    setIdx((i) => Math.max(0, i - 1))
                  } else if (e.key === 'Enter') {
                    e.preventDefault()
                    choose(idx)
                  } else if (e.key === 'Escape') {
                    e.preventDefault()
                    onClose()
                  } else if (e.key === 'Tab') {
                    e.preventDefault()
                    setMode('create')
                    setName(query.trim() || 'New Portfolio')
                  }
                }}
                placeholder="Search portfolios… (Enter to switch · Tab to create)"
                className="flex-1 bg-transparent border-0 p-0 text-[12px] font-mono text-text-primary"
              />
              <span className="text-[9px] uppercase tracking-wider text-text-secondary border border-border px-1.5 py-0.5">
                ⌘K
              </span>
            </div>
            <ul className="max-h-[50vh] overflow-auto">
              {items.length === 0 && (
                <li className="px-3 py-2 text-text-secondary text-[11px] uppercase tracking-wider">
                  No matches
                </li>
              )}
              {items.map((item, i) => {
                const active = i === idx
                if (item.kind === 'create') {
                  return (
                    <li
                      key="__create__"
                      onClick={() => choose(i)}
                      onMouseEnter={() => setIdx(i)}
                      className={`flex items-center gap-2 px-3 py-2 cursor-pointer border-t border-border ${
                        active ? 'bg-elevated' : 'hover:bg-hover'
                      }`}
                    >
                      <span className="w-[3px] h-4 flex-shrink-0 bg-amber" />
                      <span className="text-[12px] text-text-primary">
                        + Create new portfolio
                      </span>
                      <span className="ml-auto text-[9px] uppercase tracking-wider text-text-secondary">
                        ⌘N
                      </span>
                    </li>
                  )
                }
                const b = item.branch
                const m = computeBranchMetrics(b)
                const isSel = b.id === selectedId
                return (
                  <li
                    key={b.id}
                    onClick={() => choose(i)}
                    onMouseEnter={() => setIdx(i)}
                    className={`flex items-center gap-2 px-3 py-2 cursor-pointer ${
                      active ? 'bg-elevated' : 'hover:bg-hover'
                    }`}
                  >
                    <span
                      className="w-[3px] h-4 flex-shrink-0"
                      style={{ background: b.color }}
                    />
                    <span className="flex-1 truncate text-[12px] text-text-primary">
                      {b.name}
                      {isSel && (
                        <span className="ml-2 text-[9px] uppercase tracking-wider text-text-secondary">
                          current
                        </span>
                      )}
                    </span>
                    <span className="text-[10px] font-mono tabular-nums text-text-secondary">
                      {fmtUsd(m.finalValue, { compact: true, decimals: 2 })}
                    </span>
                    <span
                      className={`text-[10px] font-mono tabular-nums w-16 text-right ${classForPnl(
                        m.totalReturn,
                      )}`}
                    >
                      {fmtPct(m.totalReturn, { sign: true, decimals: 1 })}
                    </span>
                  </li>
                )
              })}
            </ul>
            <div className="border-t border-border bg-panel-alt px-3 py-1.5 flex items-center gap-3 text-[9px] uppercase tracking-wider text-text-secondary">
              <ShortcutHint k="↑↓" l="navigate" />
              <ShortcutHint k="↵" l="select" />
              <ShortcutHint k="tab" l="create" />
              <ShortcutHint k="esc" l="close" />
            </div>
          </>
        ) : (
          <>
            <div className="border-b border-border bg-panel-alt px-3 py-2 flex items-center gap-2">
              <span className="text-[10px] uppercase tracking-wider text-text-secondary">
                New Portfolio
              </span>
              <button
                onClick={() => setMode('switch')}
                className="ml-auto text-[9px] uppercase tracking-wider text-text-secondary hover:text-text-primary"
              >
                ← back
              </button>
            </div>
            <div className="p-3 space-y-2">
              <div>
                <div className="label mb-1">Name</div>
                <input
                  ref={createNameRef}
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') submitCreate()
                    else if (e.key === 'Escape') onClose()
                  }}
                  className="w-full"
                />
              </div>
              <div>
                <div className="label mb-1">Starting Balance (USD)</div>
                <input
                  value={balance}
                  onChange={(e) => setBalance(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') submitCreate()
                    else if (e.key === 'Escape') onClose()
                  }}
                  className="w-full"
                />
              </div>
            </div>
            <div className="border-t border-border bg-panel-alt px-3 py-2 flex justify-end gap-2">
              <button
                onClick={onClose}
                className="text-[10px] uppercase tracking-wider px-3 py-1 border border-border text-text-secondary hover:text-text-primary"
              >
                Cancel
              </button>
              <button
                onClick={submitCreate}
                className="text-[10px] uppercase tracking-wider px-3 py-1 border border-border bg-elevated text-text-primary"
              >
                Create
              </button>
            </div>
          </>
        )}
      </div>
    </div>
  )
}

function ShortcutHint({ k, l }: { k: string; l: string }) {
  return (
    <span className="inline-flex items-center gap-1">
      <span className="px-1 py-0.5 border border-border text-text-primary font-mono">
        {k}
      </span>
      <span>{l}</span>
    </span>
  )
}
