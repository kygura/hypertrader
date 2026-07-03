#!/usr/bin/env bun
/**
 * Fetches 365 days of daily OHLCV candles from the Hyperliquid public API for
 * BTC, ETH, SOL and HYPE, then writes the result to public/data/prices.json.
 *
 * Run with:  bun scripts/fetch-prices.ts
 */

import { writeFile, mkdir } from 'node:fs/promises'
import { join } from 'node:path'

const HL_REST = 'https://api.hyperliquid.xyz/info'
const DAY_MS = 24 * 60 * 60 * 1000
const DAYS = 540

interface RawCandle {
  t: number  // open ms
  T: number  // close ms
  s: string  // symbol
  i: string  // interval
  o: string
  h: string
  l: string
  c: string
  v: string
  n: number  // num trades
}

interface Candle {
  t: number
  T: number
  o: number
  h: number
  l: number
  c: number
  v: number
}

async function fetchCandles(
  coin: string,
  interval: string,
  startTime: number,
  endTime: number,
): Promise<Candle[]> {
  const res = await fetch(HL_REST, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({
      type: 'candleSnapshot',
      req: { coin, interval, startTime, endTime },
    }),
  })
  if (!res.ok) throw new Error(`HTTP ${res.status} for ${coin}`)
  const raw = (await res.json()) as RawCandle[]
  return raw.map((r) => ({
    t: r.t,
    T: r.T,
    o: Number(r.o),
    h: Number(r.h),
    l: Number(r.l),
    c: Number(r.c),
    v: Number(r.v),
  }))
}

const ASSETS = ['BTC', 'ETH', 'SOL', 'HYPE'] as const

const end = Math.floor(Date.now() / DAY_MS) * DAY_MS + DAY_MS - 1
const start = end - DAY_MS * (DAYS - 1)

console.log(
  `Fetching ${DAYS}d candles from ${new Date(start).toISOString().slice(0, 10)} → ${new Date(end).toISOString().slice(0, 10)}`,
)

const result: Record<string, Candle[]> = {}

for (const asset of ASSETS) {
  process.stdout.write(`  ${asset}… `)
  try {
    const candles = await fetchCandles(asset, '1d', start, end)
    result[asset] = candles
    console.log(`${candles.length} candles  (${new Date(candles[0]?.t ?? 0).toISOString().slice(0, 10)} → ${new Date(candles[candles.length - 1]?.T ?? 0).toISOString().slice(0, 10)})`)
  } catch (e) {
    console.error(`FAILED: ${e}`)
    result[asset] = []
  }
  // brief pause to avoid rate-limiting
  await Bun.sleep(200)
}

const outDir = join(import.meta.dir, '..', 'src', 'data')
await mkdir(outDir, { recursive: true })
const outFile = join(outDir, 'prices.json')
await writeFile(outFile, JSON.stringify(result, null, 2))

const totalCandles = Object.values(result).reduce((s, v) => s + v.length, 0)
console.log(`\nWrote ${totalCandles} total candles to ${outFile}`)
