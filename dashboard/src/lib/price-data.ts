/**
 * Historical price data.
 *
 * Primary source: src/data/prices.json — real OHLCV candles fetched from
 * Hyperliquid (365 days, 1D interval).  Run `bun scripts/fetch-prices.ts`
 * to refresh.
 *
 * Fallback: seeded geometric random walk (kept for assets not in the dataset
 * and for unit tests that don't want network access).
 *
 * All exported functions keep the same synchronous API so the margin engine,
 * portfolio derivation and UI components don't need to change.
 */

import rawPrices from '../data/prices.json'
import type { Candle } from './types'

export const ASSETS = ['BTC', 'ETH', 'SOL', 'HYPE'] as const
export type SyntheticAsset = (typeof ASSETS)[number]

const DAY_MS = 24 * 60 * 60 * 1000

// ─── Candle shape that comes from the JSON ───────────────────────────────────

interface RawCandle {
  t: number
  T: number
  o: number
  h: number
  l: number
  c: number
  v: number
}

// ─── Build the real-data series map from the imported JSON ──────────────────

interface PriceSeries {
  candles: Candle[]
  closes: number[]
  dates: number[] // UTC midnight ms (candle open)
}

function buildSeries(raw: RawCandle[]): PriceSeries {
  // Sort ascending, deduplicate by UTC-day key
  const seen = new Set<number>()
  const sorted: RawCandle[] = []
  for (const c of [...raw].sort((a, b) => a.t - b.t)) {
    const dayKey = Math.floor(c.t / DAY_MS) * DAY_MS
    if (seen.has(dayKey)) continue
    seen.add(dayKey)
    sorted.push({ ...c, t: dayKey, T: dayKey + DAY_MS - 1 })
  }
  return {
    candles: sorted as Candle[],
    closes: sorted.map((c) => c.c),
    dates: sorted.map((c) => c.t),
  }
}

const realSeries: Partial<Record<string, PriceSeries>> = {}
for (const [asset, candles] of Object.entries(
  rawPrices as Record<string, RawCandle[]>,
)) {
  if (Array.isArray(candles) && candles.length > 0) {
    realSeries[asset] = buildSeries(candles)
  }
}

// All real assets share the same date grid (BTC drives it; others align).
// Any asset missing a date gets the previous close carried forward.
const masterDates: number[] =
  realSeries['BTC']?.dates ?? []

// ─── Synthetic fallback (seeded geometric random walk) ──────────────────────

const BASE_PRICES: Record<SyntheticAsset, number> = {
  BTC: 95000,
  ETH: 3500,
  SOL: 180,
  HYPE: 25,
}
const VOL: Record<SyntheticAsset, number> = {
  BTC: 0.025,
  ETH: 0.032,
  SOL: 0.045,
  HYPE: 0.06,
}
const DRIFT: Record<SyntheticAsset, number> = {
  BTC: 0.0009,
  ETH: 0.0006,
  SOL: 0.0012,
  HYPE: 0.002,
}
const SEED: Record<SyntheticAsset, number> = {
  BTC: 1337,
  ETH: 2424,
  SOL: 9091,
  HYPE: 17171,
}

function mulberry32(seed: number) {
  let a = seed >>> 0
  return function () {
    a |= 0
    a = (a + 0x6d2b79f5) | 0
    let t = a
    t = Math.imul(t ^ (t >>> 15), t | 1)
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61)
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296
  }
}

function gauss(rng: () => number): number {
  const u = Math.max(rng(), 1e-9)
  const v = rng()
  return Math.sqrt(-2 * Math.log(u)) * Math.cos(2 * Math.PI * v)
}

const syntheticCache: Partial<Record<SyntheticAsset, PriceSeries>> = {}

function buildSyntheticSeries(asset: SyntheticAsset): PriceSeries {
  const cached = syntheticCache[asset]
  if (cached) return cached
  const days = masterDates.length || 365
  // Anchor to the same date range as real data when available
  const endDay =
    masterDates.length > 0
      ? masterDates[masterDates.length - 1]
      : Math.floor(Date.now() / DAY_MS) * DAY_MS
  const startDay = endDay - DAY_MS * (days - 1)

  const rng = mulberry32(SEED[asset])
  let price = BASE_PRICES[asset]
  const candles: Candle[] = []
  const closes: number[] = []
  const dates: number[] = []

  for (let i = 0; i < days; i++) {
    const t = startDay + i * DAY_MS
    const T = t + DAY_MS - 1
    const open = price
    const ret = DRIFT[asset] + VOL[asset] * gauss(rng)
    const close = Math.max(open * Math.exp(ret), 0.0001)
    const swing = Math.abs(gauss(rng)) * VOL[asset] * 0.6
    const high = Math.max(open, close) * (1 + swing)
    const low = Math.min(open, close) * (1 - swing)
    const v = (10000 + rng() * 50000) * (BASE_PRICES[asset] / 100)
    candles.push({ t, T, o: open, h: high, l: low, c: close, v })
    closes.push(close)
    dates.push(t)
    price = close
  }
  const series: PriceSeries = { candles, closes, dates }
  syntheticCache[asset] = series
  return series
}

// ─── Unified accessor ────────────────────────────────────────────────────────

function getSeriesFor(asset: string): PriceSeries {
  const real = realSeries[asset]
  if (real) return real
  if ((ASSETS as readonly string[]).includes(asset)) {
    return buildSyntheticSeries(asset as SyntheticAsset)
  }
  // Unknown asset — return synthetic BTC-scale data
  return buildSyntheticSeries('BTC')
}

// ─── Exported API (unchanged signatures) ────────────────────────────────────

/**
 * Returns the shared date grid (UTC midnight ms, ascending).
 * Driven by BTC real data (365 days) or the synthetic fallback.
 */
export function getAllSyntheticDates(): number[] {
  return masterDates.length > 0 ? masterDates : buildSyntheticSeries('BTC').dates
}

/**
 * Close price for `asset` at index `idx` into getAllSyntheticDates().
 * Real data is used when available; synthetic otherwise.
 * For real assets with a date grid shorter than master (unlikely), carries
 * forward the last known close.
 */
export function priceAt(asset: string, idx: number): number {
  const series = getSeriesFor(asset)
  const allDates = getAllSyntheticDates()

  if (series.dates === allDates) {
    // Fast path — same date grid
    const ci = Math.max(0, Math.min(idx, series.closes.length - 1))
    return series.closes[ci]
  }

  // Map master idx → asset's own date → asset close (carry-forward)
  const targetMs = allDates[Math.max(0, Math.min(idx, allDates.length - 1))]
  return closestClose(series, targetMs)
}

/**
 * Close price for `asset` at a specific UTC-ms timestamp.
 */
export function priceAtDate(asset: string, dateMs: number): number {
  const series = getSeriesFor(asset)
  return closestClose(series, dateMs)
}

function closestClose(series: PriceSeries, targetMs: number): number {
  if (series.closes.length === 0) return 0
  if (targetMs <= series.dates[0]) return series.closes[0]
  if (targetMs >= series.dates[series.dates.length - 1])
    return series.closes[series.closes.length - 1]
  // Binary search for floor match
  let lo = 0
  let hi = series.dates.length - 1
  while (lo < hi) {
    const mid = (lo + hi + 1) >>> 1
    if (series.dates[mid] <= targetMs) lo = mid
    else hi = mid - 1
  }
  return series.closes[lo]
}

/**
 * Convert a UTC-ms timestamp to an index into getAllSyntheticDates().
 */
export function dateIndex(dateMs: number): number {
  const dates = getAllSyntheticDates()
  if (dateMs <= dates[0]) return 0
  if (dateMs >= dates[dates.length - 1]) return dates.length - 1
  let lo = 0
  let hi = dates.length - 1
  while (lo < hi) {
    const mid = (lo + hi) >>> 1
    if (dates[mid] < dateMs) lo = mid + 1
    else hi = mid
  }
  return lo
}

export function isSupportedAsset(name: string): name is SyntheticAsset {
  return (ASSETS as readonly string[]).includes(name)
}

export function getMaxLeverage(asset: string): number {
  const m: Record<string, number> = { BTC: 50, ETH: 50, SOL: 20, HYPE: 10 }
  return m[asset] ?? 10
}

/**
 * Returns OHLCV candles for `asset` over the shared date grid.
 * Kept for backward compatibility — returns real data when available.
 */
export function getSyntheticSeries(asset: SyntheticAsset): PriceSeries {
  return getSeriesFor(asset) as PriceSeries
}

/**
 * Returns the underlying candles for a given asset (real or synthetic).
 * Used by the portfolio performance tabs when the AssetComparison falls back
 * to local data instead of hitting the live HL candle endpoint.
 */
export function getCandlesFor(asset: string): Candle[] {
  return getSeriesFor(asset).candles
}

/**
 * Whether `asset` has real (downloaded) price data.
 */
export function hasRealData(asset: string): boolean {
  return !!realSeries[asset]
}

/**
 * Summary of the loaded real data — useful for dev/debug.
 */
export function realDataSummary(): Array<{
  asset: string
  days: number
  from: string
  to: string
  lastClose: number
}> {
  return Object.entries(realSeries).map(([asset, s]) => ({
    asset,
    days: s!.dates.length,
    from: new Date(s!.dates[0]).toISOString().slice(0, 10),
    to: new Date(s!.dates[s!.dates.length - 1]).toISOString().slice(0, 10),
    lastClose: s!.closes[s!.closes.length - 1],
  }))
}
