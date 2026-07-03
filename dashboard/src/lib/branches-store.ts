// Branch persistence + mutations. localStorage only.

import type {
  Branch,
  BranchPosition,
  PendingOrder,
  PortfolioImport,
} from './types'
import { accountStateAt, computeBranchEquity } from './margin-engine'
import { dateIndex } from './price-data'

const KEY = '**********************'
const SELECTED_KEY = '****************************'
const VERSION_KEY = 'hypertrade-portfolio-seed-version'
const CURRENT_VERSION = '2026-05-12-genesis'

const COLORS = [
  '#ed3602',
  '#38a67c',
  '#ffb800',
  '#3aa6ff',
  '#b07cff',
  '#ff6fad',
  '#5fd9c2',
  '#f6c453',
]

export function pickColor(existing: Branch[]): string {
  const used = new Set(existing.map((b) => b.color))
  for (const c of COLORS) if (!used.has(c)) return c
  return COLORS[existing.length % COLORS.length]
}

export function loadBranches(): Branch[] {
  try {
    const ver = localStorage.getItem(VERSION_KEY)
    if (ver !== CURRENT_VERSION) {
      localStorage.removeItem(KEY)
      localStorage.removeItem(SELECTED_KEY)
      localStorage.setItem(VERSION_KEY, CURRENT_VERSION)
      return seed()
    }
    const raw = localStorage.getItem(KEY)
    if (!raw) return seed()
    const parsed = JSON.parse(raw) as Branch[]
    if (!Array.isArray(parsed) || parsed.length === 0) return seed()
    return parsed
  } catch {
    return seed()
  }
}

export function saveBranches(branches: Branch[]) {
  localStorage.setItem(KEY, JSON.stringify(branches))
}

export function loadSelected(): string | null {
  return localStorage.getItem(SELECTED_KEY)
}
export function saveSelected(id: string) {
  localStorage.setItem(SELECTED_KEY, id)
}

function uid(): string {
  return Math.random().toString(36).slice(2, 10) + Date.now().toString(36).slice(-4)
}

function seed(): Branch[] {
  const entryDate = Date.UTC(2025, 3, 14) // April 14, 2025
  // TWAP exit dates: 4 chunks spread across mid-July → late August 2025
  const twapExits = [
    Date.UTC(2025, 6, 14), // Jul 14
    Date.UTC(2025, 6, 28), // Jul 28
    Date.UTC(2025, 7, 11), // Aug 11
    Date.UTC(2025, 7, 25), // Aug 25
  ]

  function twapLong(asset: string, totalMargin: number): BranchPosition[] {
    const chunk = totalMargin / twapExits.length
    return twapExits.map((exitDate) => ({
      id: uid(),
      asset,
      side: 'long' as const,
      marginMode: 'cross' as const,
      leverage: 10,
      marginUsd: chunk,
      entryPrice: 0, // hydrated from real prices on load
      entryDate,
      exitDate,
    }))
  }

  const genesis: Branch = {
    id: 'genesis',
    name: 'Genesis',
    color: COLORS[0],
    createdAt: entryDate,
    startingBalance: 1_000_000,
    positions: [...twapLong('ETH', 500_000), ...twapLong('BTC', 500_000)],
  }

  const branches: Branch[] = [genesis]
  saveBranches(branches)
  saveSelected(genesis.id)
  return branches
}

// Hydrate entry price from synthetic data if zero
export function hydrateEntryPrices(
  branches: Branch[],
  priceAtDate: (asset: string, date: number) => number,
): Branch[] {
  let mutated = false
  const out = branches.map((b) => {
    const positions = b.positions.map((p) => {
      let next = p
      if (!next.entryPrice || next.entryPrice <= 0) {
        const px = priceAtDate(next.asset, next.entryDate)
        if (px > 0) {
          mutated = true
          next = { ...next, entryPrice: px }
        }
      }
      if (
        next.exitDate !== undefined &&
        (next.exitPrice === undefined || next.exitPrice <= 0)
      ) {
        const px = priceAtDate(next.asset, next.exitDate)
        if (px > 0) {
          mutated = true
          next = { ...next, exitPrice: px }
        }
      }
      return next
    })
    return { ...b, positions }
  })
  if (mutated) saveBranches(out)
  return out
}

export function newBranchId(): string {
  return uid()
}
export function newPositionId(): string {
  return uid()
}

export function forkBranch(
  parent: Branch,
  opts: {
    name: string
    forkDate: number
    existing: Branch[]
  },
): Branch {
  const idx = dateIndex(opts.forkDate)
  const equity = computeBranchEquity(parent)
  const startingBalance = Math.max(
    accountStateAt(parent, idx).totalEquity,
    equity[idx] ?? parent.startingBalance,
  )
  // Inherit positions whose entryDate <= forkDate
  const inherited: BranchPosition[] = parent.positions
    .filter((p) => p.entryDate <= opts.forkDate)
    .map((p) => ({ ...p, id: newPositionId() }))

  return {
    id: newBranchId(),
    name: opts.name,
    color: pickColor(opts.existing),
    parentId: parent.id,
    forkDate: opts.forkDate,
    createdAt: Date.now(),
    startingBalance,
    positions: inherited,
  }
}

export function importBranch(
  data: PortfolioImport,
  existing: Branch[],
): Branch {
  return {
    id: newBranchId(),
    name: data.name,
    color: data.color ?? pickColor(existing),
    createdAt: data.startDate ?? Date.now(),
    startingBalance: data.startingBalance,
    positions: data.positions.map((p) => ({
      ...p,
      id: p.id ?? newPositionId(),
    })),
    pendingOrders: (data.pendingOrders ?? []).map((o) => ({
      ...o,
      id: o.id ?? newPositionId(),
    })) as PendingOrder[],
  }
}

export function addPositionToBranch(
  branches: Branch[],
  branchId: string,
  pos: Omit<BranchPosition, 'id'>,
): Branch[] {
  const out = branches.map((b) =>
    b.id === branchId
      ? { ...b, positions: [...b.positions, { ...pos, id: newPositionId() }] }
      : b,
  )
  saveBranches(out)
  return out
}

export function addPendingOrderToBranch(
  branches: Branch[],
  branchId: string,
  order: Omit<PendingOrder, 'id'>,
): Branch[] {
  const out = branches.map((b) =>
    b.id === branchId
      ? {
          ...b,
          pendingOrders: [
            ...(b.pendingOrders ?? []),
            { ...order, id: newPositionId() },
          ],
        }
      : b,
  )
  saveBranches(out)
  return out
}

export function cancelPendingOrder(
  branches: Branch[],
  branchId: string,
  orderId: string,
): Branch[] {
  const out = branches.map((b) =>
    b.id === branchId
      ? {
          ...b,
          pendingOrders: (b.pendingOrders ?? []).filter(
            (o) => o.id !== orderId,
          ),
        }
      : b,
  )
  saveBranches(out)
  return out
}

export function closePositionInBranch(
  branches: Branch[],
  branchId: string,
  positionId: string,
  exitPrice: number,
  exitDate: number,
): Branch[] {
  const out = branches.map((b) =>
    b.id === branchId
      ? {
          ...b,
          positions: b.positions.map((p) =>
            p.id === positionId && p.exitDate === undefined
              ? { ...p, exitPrice, exitDate }
              : p,
          ),
        }
      : b,
  )
  saveBranches(out)
  return out
}

export function deletePositionFromBranch(
  branches: Branch[],
  branchId: string,
  positionId: string,
): Branch[] {
  const out = branches.map((b) =>
    b.id === branchId
      ? { ...b, positions: b.positions.filter((p) => p.id !== positionId) }
      : b,
  )
  saveBranches(out)
  return out
}

export function renameBranch(
  branches: Branch[],
  branchId: string,
  name: string,
): Branch[] {
  const out = branches.map((b) => (b.id === branchId ? { ...b, name } : b))
  saveBranches(out)
  return out
}

export function deleteBranch(
  branches: Branch[],
  branchId: string,
): Branch[] {
  const out = branches.filter((b) => b.id !== branchId)
  saveBranches(out)
  return out
}

export function validateImport(json: unknown): {
  ok: boolean
  data?: PortfolioImport
  error?: string
} {
  if (!json || typeof json !== 'object')
    return { ok: false, error: 'Not an object' }
  const o = json as Record<string, unknown>
  if (typeof o.name !== 'string') return { ok: false, error: 'Missing name' }
  if (typeof o.startingBalance !== 'number')
    return { ok: false, error: 'Missing startingBalance' }
  if (!Array.isArray(o.positions))
    return { ok: false, error: 'Missing positions array' }
  for (const p of o.positions) {
    if (typeof p !== 'object' || !p)
      return { ok: false, error: 'Invalid position' }
    const pp = p as Record<string, unknown>
    for (const k of [
      'asset',
      'side',
      'marginMode',
      'leverage',
      'marginUsd',
      'entryPrice',
      'entryDate',
    ]) {
      if (!(k in pp)) return { ok: false, error: `Position missing ${k}` }
    }
  }
  return { ok: true, data: json as PortfolioImport }
}
