// Pure margin engine. No React. Computes position + account state across time.

import type {
  AccountState,
  Branch,
  BranchPosition,
  PositionState,
} from './types'
import {
  dateIndex,
  getAllSyntheticDates,
  priceAt,
} from './price-data'

export function maintenanceMarginRate(leverage: number): number {
  return leverage <= 10 ? 0.005 : 0.01
}

export function liqPrice(p: {
  side: 'long' | 'short'
  entryPrice: number
  leverage: number
}): number {
  const mmr = maintenanceMarginRate(p.leverage)
  if (p.side === 'long') {
    return p.entryPrice * (1 - 1 / p.leverage + mmr)
  }
  return p.entryPrice * (1 + 1 / p.leverage - mmr)
}

export function notionalSize(p: BranchPosition): number {
  // size in base asset units
  return (p.marginUsd * p.leverage) / p.entryPrice
}

export function posStateAt(
  position: BranchPosition,
  idx: number,
): PositionState {
  const dates = getAllSyntheticDates()
  const clampedIdx = Math.max(0, Math.min(idx, dates.length - 1))
  const ts = dates[clampedIdx]

  // Check if position is active at this idx
  const entryIdx = dateIndex(position.entryDate)
  const exitIdx =
    position.exitDate !== undefined ? dateIndex(position.exitDate) : Infinity

  const liq = liqPrice(position)
  const size = notionalSize(position)
  const mmr = maintenanceMarginRate(position.leverage)

  // Walk forward from entry to detect liquidation
  let liquidatedAtIdx: number | null = null
  let liquidatedPrice = 0
  const walkEnd = Math.min(clampedIdx, exitIdx)
  if (clampedIdx >= entryIdx) {
    for (let i = entryIdx; i <= walkEnd; i++) {
      const px = priceAt(position.asset, i)
      if (position.side === 'long' ? px <= liq : px >= liq) {
        liquidatedAtIdx = i
        liquidatedPrice = liq
        break
      }
    }
  }

  const isClosed =
    position.exitDate !== undefined && clampedIdx >= exitIdx
  const isLiquidated = liquidatedAtIdx !== null

  // Mark price at idx
  let markPrice: number
  if (isLiquidated) {
    markPrice = liquidatedPrice
  } else if (isClosed && position.exitPrice !== undefined) {
    markPrice = position.exitPrice
  } else {
    markPrice = priceAt(position.asset, clampedIdx)
  }

  // Before entry → no position effect
  if (clampedIdx < entryIdx) {
    return {
      markPrice: priceAt(position.asset, clampedIdx),
      notional: 0,
      upnl: 0,
      marginUsed: 0,
      maintenanceMargin: 0,
      liqPrice: liq,
      isLiquidated: false,
      isClosed: false,
      pnlPct: 0,
    }
  }

  const dir = position.side === 'long' ? 1 : -1
  const rawPnl = (markPrice - position.entryPrice) * size * dir

  // For liquidation, PnL ≈ -margin (loss of full margin)
  const upnl = isLiquidated ? -position.marginUsd : rawPnl

  const notional = size * markPrice
  const marginUsed = isClosed || isLiquidated ? 0 : position.marginUsd
  const maintenanceMargin = isClosed || isLiquidated ? 0 : notional * mmr
  const pnlPct = position.marginUsd > 0 ? upnl / position.marginUsd : 0

  void ts
  return {
    markPrice,
    notional,
    upnl,
    marginUsed,
    maintenanceMargin,
    liqPrice: liq,
    isLiquidated,
    isClosed,
    pnlPct,
  }
}

export function realizedPnlUpToIdx(
  position: BranchPosition,
  idx: number,
): number {
  const exitIdx =
    position.exitDate !== undefined ? dateIndex(position.exitDate) : Infinity
  const st = posStateAt(position, idx)
  if (st.isLiquidated) return -position.marginUsd
  if (position.exitDate !== undefined && idx >= exitIdx) {
    const exitPx = position.exitPrice ?? priceAt(position.asset, exitIdx)
    const size = notionalSize(position)
    const dir = position.side === 'long' ? 1 : -1
    return (exitPx - position.entryPrice) * size * dir
  }
  return 0
}

export function accountStateAt(branch: Branch, idx: number): AccountState {
  let crossEquity = 0
  let isoEquity = 0
  let marginUsed = 0
  let upnl = 0
  let maintenanceMargin = 0
  let realized = 0
  let liquidatedAll = false

  for (const p of branch.positions) {
    const st = posStateAt(p, idx)
    const r = realizedPnlUpToIdx(p, idx)
    realized += r
    if (!st.isClosed && !st.isLiquidated) {
      upnl += st.upnl
      marginUsed += st.marginUsed
      maintenanceMargin += st.maintenanceMargin
      if (p.marginMode === 'cross') crossEquity += st.upnl
      else isoEquity += st.upnl
    }
  }

  const totalEquity = branch.startingBalance + realized + upnl
  const available = Math.max(totalEquity - marginUsed, 0)
  const totalNotional = (() => {
    let n = 0
    for (const p of branch.positions) {
      const st = posStateAt(p, idx)
      if (!st.isClosed && !st.isLiquidated) n += st.notional
    }
    return n
  })()
  const leverage = totalEquity > 0 ? totalNotional / totalEquity : 0
  liquidatedAll = totalEquity <= maintenanceMargin && marginUsed > 0

  return {
    crossEquity,
    isoEquity,
    totalEquity,
    marginUsed,
    available,
    upnl,
    maintenanceMargin,
    isLiquidated: liquidatedAll,
    maxWithdraw: available,
    leverage,
  }
}

export function availableMarginAtDate(branch: Branch, dateMs: number): number {
  const idx = dateIndex(dateMs)
  if (idx < 0) return branch.startingBalance
  const st = accountStateAt(branch, idx)
  return st.available + st.marginUsed
}

export function availableMarginAtIdx(branch: Branch, idx: number): number {
  const st = accountStateAt(branch, idx)
  return st.available + st.marginUsed
}

export function equityAtDate(branch: Branch, dateMs: number): number {
  const idx = dateIndex(dateMs)
  if (idx < 0) return branch.startingBalance
  return accountStateAt(branch, idx).totalEquity
}

export function computeBranchEquity(branch: Branch): number[] {
  const dates = getAllSyntheticDates()
  const out = new Array<number>(dates.length)
  for (let i = 0; i < dates.length; i++) {
    out[i] = accountStateAt(branch, i).totalEquity
  }
  return out
}

// ─── Metrics ───

export interface BranchMetrics {
  totalReturn: number // fraction
  annualizedReturn: number
  maxDrawdown: number
  winRate: number
  avgWin: number
  avgLoss: number
  profitFactor: number
  totalTrades: number
  sharpe: number
  sortino: number
  calmar: number
  volatility: number // annualized
  bestDay: number
  worstDay: number
  avgHoldTimeDays: number
  finalValue: number
}

export function computeBranchMetrics(branch: Branch): BranchMetrics {
  const equity = computeBranchEquity(branch)
  const start = branch.startingBalance
  const end = equity[equity.length - 1] ?? start
  const totalReturn = start > 0 ? (end - start) / start : 0
  const days = equity.length
  const annFactor = 365 / Math.max(days, 1)
  const annualizedReturn =
    start > 0 ? Math.pow(end / start, annFactor) - 1 : 0

  // Daily returns
  const rets: number[] = []
  for (let i = 1; i < equity.length; i++) {
    const prev = equity[i - 1]
    if (prev > 0) rets.push((equity[i] - prev) / prev)
  }
  const mean = avg(rets)
  const std = stdev(rets)
  const downside = stdev(rets.filter((r) => r < 0))
  const volatility = std * Math.sqrt(365)
  const sharpe = std > 0 ? (mean / std) * Math.sqrt(365) : 0
  const sortino = downside > 0 ? (mean / downside) * Math.sqrt(365) : 0
  const bestDay = rets.length ? Math.max(...rets) : 0
  const worstDay = rets.length ? Math.min(...rets) : 0

  let peak = equity[0] ?? start
  let maxDD = 0
  for (const v of equity) {
    if (v > peak) peak = v
    const dd = peak > 0 ? (peak - v) / peak : 0
    if (dd > maxDD) maxDD = dd
  }
  const calmar = maxDD > 0 ? annualizedReturn / maxDD : 0

  // Trade-level stats — count closed/liquidated positions
  const wins: number[] = []
  const losses: number[] = []
  let totalHold = 0
  let countHold = 0
  for (const p of branch.positions) {
    const finalIdx = equity.length - 1
    const st = posStateAt(p, finalIdx)
    if (st.isClosed || st.isLiquidated) {
      const pnl = realizedPnlUpToIdx(p, finalIdx)
      if (pnl > 0) wins.push(pnl)
      else losses.push(pnl)
      const exitMs = p.exitDate ?? Date.now()
      totalHold += (exitMs - p.entryDate) / (1000 * 60 * 60 * 24)
      countHold++
    }
  }
  const totalTrades = wins.length + losses.length
  const winRate = totalTrades > 0 ? wins.length / totalTrades : 0
  const avgWin = wins.length ? avg(wins) : 0
  const avgLoss = losses.length ? avg(losses) : 0
  const grossWin = wins.reduce((a, b) => a + b, 0)
  const grossLoss = Math.abs(losses.reduce((a, b) => a + b, 0))
  const profitFactor = grossLoss > 0 ? grossWin / grossLoss : grossWin > 0 ? Infinity : 0
  const avgHoldTimeDays = countHold > 0 ? totalHold / countHold : 0

  return {
    totalReturn,
    annualizedReturn,
    maxDrawdown: maxDD,
    winRate,
    avgWin,
    avgLoss,
    profitFactor,
    totalTrades,
    sharpe,
    sortino,
    calmar,
    volatility,
    bestDay,
    worstDay,
    avgHoldTimeDays,
    finalValue: end,
  }
}

function avg(xs: number[]): number {
  if (!xs.length) return 0
  let s = 0
  for (const x of xs) s += x
  return s / xs.length
}

function stdev(xs: number[]): number {
  if (xs.length < 2) return 0
  const m = avg(xs)
  let s = 0
  for (const x of xs) s += (x - m) * (x - m)
  return Math.sqrt(s / (xs.length - 1))
}
