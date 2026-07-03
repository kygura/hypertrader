// Derivation helpers for portfolio views.

import type { Branch, BranchPosition, PendingOrder } from './types'
import {
  accountStateAt,
  computeBranchEquity,
  notionalSize,
  posStateAt,
  realizedPnlUpToIdx,
} from './margin-engine'
import { dateIndex, getAllSyntheticDates, priceAt } from './price-data'

export interface FillEvent {
  id: string
  positionId: string
  asset: string
  side: 'long' | 'short'
  kind: 'OPEN' | 'CLOSE' | 'LIQ'
  time: number
  price: number
  size: number
  value: number
  fee: number
  leverage: number
}

export interface TradeRow {
  id: string
  asset: string
  side: 'long' | 'short'
  marginMode: 'cross' | 'isolated'
  leverage: number
  entryDate: number
  exitDate: number
  entryPrice: number
  exitPrice: number
  size: number
  marginUsd: number
  pnl: number
  pnlPct: number
  durationMs: number
  liquidated: boolean
}

export interface DirectionBias {
  longNtl: number
  shortNtl: number
  netNtl: number
  label:
    | 'Very Bullish'
    | 'Bullish'
    | 'Neutral'
    | 'Bearish'
    | 'Very Bearish'
}

export interface DayPnl {
  date: number
  pnl: number
  pnlPct: number
  equity: number
}

export interface VenueEquity {
  perps: number
  spot: number
  staked: number
}

const FEE_RATE = 0.00045 // 0.045% taker

export function lastIdx(): number {
  return getAllSyntheticDates().length - 1
}

export function fills(branch: Branch): FillEvent[] {
  const out: FillEvent[] = []
  const lastI = lastIdx()
  for (const p of branch.positions) {
    const size = notionalSize(p)
    const entryValue = size * p.entryPrice
    out.push({
      id: `${p.id}-open`,
      positionId: p.id,
      asset: p.asset,
      side: p.side,
      kind: 'OPEN',
      time: p.entryDate,
      price: p.entryPrice,
      size,
      value: entryValue,
      fee: entryValue * FEE_RATE,
      leverage: p.leverage,
    })
    const st = posStateAt(p, lastI)
    if (st.isLiquidated) {
      const liqIdx = findLiqIdx(p)
      out.push({
        id: `${p.id}-liq`,
        positionId: p.id,
        asset: p.asset,
        side: p.side,
        kind: 'LIQ',
        time:
          liqIdx >= 0
            ? getAllSyntheticDates()[liqIdx]
            : p.exitDate ?? p.entryDate,
        price: st.liqPrice,
        size,
        value: size * st.liqPrice,
        fee: 0,
        leverage: p.leverage,
      })
    } else if (p.exitDate !== undefined) {
      const exitPx = p.exitPrice ?? priceAt(p.asset, dateIndex(p.exitDate))
      const exitValue = size * exitPx
      out.push({
        id: `${p.id}-close`,
        positionId: p.id,
        asset: p.asset,
        side: p.side,
        kind: 'CLOSE',
        time: p.exitDate,
        price: exitPx,
        size,
        value: exitValue,
        fee: exitValue * FEE_RATE,
        leverage: p.leverage,
      })
    }
  }
  return out.sort((a, b) => b.time - a.time)
}

function findLiqIdx(p: BranchPosition): number {
  const entryIdx = dateIndex(p.entryDate)
  const exitIdx =
    p.exitDate !== undefined ? dateIndex(p.exitDate) : lastIdx()
  const liq = liquidationPrice(p)
  for (let i = entryIdx; i <= exitIdx; i++) {
    const px = priceAt(p.asset, i)
    if (p.side === 'long' ? px <= liq : px >= liq) return i
  }
  return -1
}

function liquidationPrice(p: BranchPosition): number {
  const mmr = p.leverage <= 10 ? 0.005 : 0.01
  return p.side === 'long'
    ? p.entryPrice * (1 - 1 / p.leverage + mmr)
    : p.entryPrice * (1 + 1 / p.leverage - mmr)
}

export function trades(branch: Branch): TradeRow[] {
  const out: TradeRow[] = []
  const lastI = lastIdx()
  for (const p of branch.positions) {
    const st = posStateAt(p, lastI)
    if (!st.isClosed && !st.isLiquidated) continue
    const size = notionalSize(p)
    const pnl = realizedPnlUpToIdx(p, lastI)
    let exitPrice = p.exitPrice ?? 0
    let exitDate = p.exitDate ?? Date.now()
    if (st.isLiquidated) {
      const liqI = findLiqIdx(p)
      exitPrice = st.liqPrice
      exitDate = liqI >= 0 ? getAllSyntheticDates()[liqI] : exitDate
    } else if (exitPrice <= 0) {
      exitPrice = priceAt(p.asset, dateIndex(exitDate))
    }
    out.push({
      id: p.id,
      asset: p.asset,
      side: p.side,
      marginMode: p.marginMode,
      leverage: p.leverage,
      entryDate: p.entryDate,
      exitDate,
      entryPrice: p.entryPrice,
      exitPrice,
      size,
      marginUsd: p.marginUsd,
      pnl,
      pnlPct: p.marginUsd > 0 ? pnl / p.marginUsd : 0,
      durationMs: Math.max(exitDate - p.entryDate, 0),
      liquidated: st.isLiquidated,
    })
  }
  return out.sort((a, b) => b.exitDate - a.exitDate)
}

export function openPositions(branch: Branch): BranchPosition[] {
  const lastI = lastIdx()
  return branch.positions.filter((p) => {
    const st = posStateAt(p, lastI)
    return !st.isClosed && !st.isLiquidated
  })
}

export function pendingOrders(branch: Branch): PendingOrder[] {
  return branch.pendingOrders ?? []
}

export function directionBias(branch: Branch, idx = lastIdx()): DirectionBias {
  let longNtl = 0
  let shortNtl = 0
  for (const p of branch.positions) {
    const st = posStateAt(p, idx)
    if (st.isClosed || st.isLiquidated) continue
    if (p.side === 'long') longNtl += st.notional
    else shortNtl += st.notional
  }
  const total = longNtl + shortNtl
  const netNtl = longNtl - shortNtl
  const ratio = total > 0 ? netNtl / total : 0
  let label: DirectionBias['label'] = 'Neutral'
  if (ratio > 0.7) label = 'Very Bullish'
  else if (ratio > 0.2) label = 'Bullish'
  else if (ratio < -0.7) label = 'Very Bearish'
  else if (ratio < -0.2) label = 'Bearish'
  return { longNtl, shortNtl, netNtl, label }
}

export function dailyPnlSeries(branch: Branch): DayPnl[] {
  const dates = getAllSyntheticDates()
  const equity = computeBranchEquity(branch)
  const out: DayPnl[] = []
  for (let i = 0; i < dates.length; i++) {
    const prev = i === 0 ? branch.startingBalance : equity[i - 1]
    const pnl = equity[i] - prev
    out.push({
      date: dates[i],
      pnl,
      pnlPct: prev > 0 ? pnl / prev : 0,
      equity: equity[i],
    })
  }
  return out
}

export function venueEquity(branch: Branch, idx = lastIdx()): VenueEquity {
  const acct = accountStateAt(branch, idx)
  return { perps: acct.totalEquity, spot: 0, staked: 0 }
}

export interface AssetPerf {
  asset: string
  trades: number
  wins: number
  pnl: number
  winRate: number
  volume: number
}

export function perfByAsset(branch: Branch): AssetPerf[] {
  const lastI = lastIdx()
  const map = new Map<string, AssetPerf>()
  for (const p of branch.positions) {
    const st = posStateAt(p, lastI)
    if (!st.isClosed && !st.isLiquidated) continue
    const pnl = realizedPnlUpToIdx(p, lastI)
    const size = notionalSize(p)
    const vol = size * p.entryPrice * 2
    const cur =
      map.get(p.asset) ??
      ({
        asset: p.asset,
        trades: 0,
        wins: 0,
        pnl: 0,
        winRate: 0,
        volume: 0,
      } as AssetPerf)
    cur.trades++
    cur.pnl += pnl
    cur.volume += vol
    if (pnl > 0) cur.wins++
    map.set(p.asset, cur)
  }
  for (const v of map.values()) v.winRate = v.trades ? v.wins / v.trades : 0
  return [...map.values()].sort((a, b) => b.pnl - a.pnl)
}

export function totalVolume(branch: Branch): number {
  let v = 0
  for (const p of branch.positions) {
    v += notionalSize(p) * p.entryPrice
    if (p.exitDate !== undefined && p.exitPrice !== undefined) {
      v += notionalSize(p) * p.exitPrice
    }
  }
  return v
}

export function longestWinStreak(branch: Branch): number {
  const ts = trades(branch).sort((a, b) => a.exitDate - b.exitDate)
  let best = 0
  let cur = 0
  for (const t of ts) {
    if (t.pnl > 0) {
      cur++
      if (cur > best) best = cur
    } else cur = 0
  }
  return best
}

export function tradingStyle(branch: Branch): string {
  const ts = trades(branch)
  if (ts.length === 0) return '—'
  const avgMs = ts.reduce((s, t) => s + t.durationMs, 0) / ts.length
  const days = avgMs / (1000 * 60 * 60 * 24)
  if (days < 1) return 'Scalper'
  if (days < 7) return 'Day Trader'
  if (days < 30) return 'Swing Trader'
  return 'Position Trader'
}

export function avgTradeDuration(branch: Branch): number {
  const ts = trades(branch)
  if (!ts.length) return 0
  return ts.reduce((s, t) => s + t.durationMs, 0) / ts.length
}

export function medianTradeDuration(branch: Branch): number {
  const ts = trades(branch)
    .map((t) => t.durationMs)
    .sort((a, b) => a - b)
  if (!ts.length) return 0
  const mid = Math.floor(ts.length / 2)
  return ts.length % 2 ? ts[mid] : (ts[mid - 1] + ts[mid]) / 2
}

export function pnlCohort(branch: Branch): 'Profitable' | 'Unprofitable' | '—' {
  const eq = computeBranchEquity(branch)
  const last = eq[eq.length - 1]
  if (!isFinite(last)) return '—'
  return last >= branch.startingBalance ? 'Profitable' : 'Unprofitable'
}

export function sizeCohort(branch: Branch): 'Apex' | 'Whale' | 'Retail' {
  if (branch.startingBalance >= 1_000_000) return 'Apex'
  if (branch.startingBalance >= 100_000) return 'Whale'
  return 'Retail'
}
