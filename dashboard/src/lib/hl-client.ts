// Hyperliquid public REST + WS client. No auth required.

import type { AllMids, AssetCtx, Candle, HLUserState } from './types'

export const HL_REST = 'https://api.hyperliquid.xyz/info'
export const HL_WS = 'wss://api.hyperliquid.xyz/ws'

async function post<T>(body: unknown): Promise<T> {
  const r = await fetch(HL_REST, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (!r.ok) throw new Error(`HL ${r.status}`)
  return (await r.json()) as T
}

export function fetchAllMids(): Promise<AllMids> {
  return post<AllMids>({ type: 'allMids' })
}

interface RawPerpMeta {
  universe: Array<{ name: string; szDecimals: number; maxLeverage: number }>
}
interface RawSpotMeta {
  tokens: Array<{ name: string; szDecimals: number; index: number }>
  universe: Array<{ name: string; tokens: [number, number]; index: number }>
}

interface RawAssetCtxPerp {
  funding: string
  openInterest: string
  prevDayPx: string
  dayNtlVlm: string
  premium: string
  oraclePx: string
  markPx: string
  midPx?: string
  dayBaseVlm: string
}

interface RawAssetCtxSpot {
  prevDayPx: string
  dayNtlVlm: string
  markPx: string
  midPx?: string
  dayBaseVlm: string
  circulatingSupply?: string
}

export async function fetchPerpMetaAndCtxs(): Promise<{
  meta: RawPerpMeta
  ctxs: AssetCtx[]
}> {
  const res = await post<[RawPerpMeta, RawAssetCtxPerp[]]>({
    type: 'metaAndAssetCtxs',
  })
  const meta = res[0]
  const ctxs: AssetCtx[] = meta.universe.map((u, i) => {
    const ctx = res[1][i]
    const markPx = Number(ctx.markPx)
    const prev = Number(ctx.prevDayPx)
    const change = prev > 0 ? (markPx - prev) / prev : 0
    const oi = Number(ctx.openInterest)
    return {
      name: u.name,
      kind: 'PERP' as const,
      markPx,
      oraclePx: Number(ctx.oraclePx),
      midPx: Number(ctx.midPx ?? ctx.markPx),
      dayNtlVlm: Number(ctx.dayNtlVlm),
      prevDayPx: prev,
      openInterest: oi,
      funding: Number(ctx.funding),
      premium: Number(ctx.premium),
      dayChange: change,
      dayHigh: 0,
      dayLow: 0,
    }
  })
  return { meta, ctxs }
}

export async function fetchSpotMetaAndCtxs(): Promise<{ ctxs: AssetCtx[] }> {
  try {
    const res = await post<[RawSpotMeta, RawAssetCtxSpot[]]>({
      type: 'spotMetaAndAssetCtxs',
    })
    const meta = res[0]
    const ctxs: AssetCtx[] = meta.universe.map((u, i) => {
      const ctx = res[1][i]
      const markPx = Number(ctx.markPx)
      const prev = Number(ctx.prevDayPx)
      const change = prev > 0 ? (markPx - prev) / prev : 0
      return {
        name: u.name,
        kind: 'SPOT' as const,
        markPx,
        oraclePx: markPx,
        midPx: Number(ctx.midPx ?? ctx.markPx),
        dayNtlVlm: Number(ctx.dayNtlVlm),
        prevDayPx: prev,
        openInterest: 0,
        funding: 0,
        premium: 0,
        dayChange: change,
        dayHigh: 0,
        dayLow: 0,
      }
    })
    return { ctxs }
  } catch {
    return { ctxs: [] }
  }
}

export interface CandleInterval {
  label: string
  value: '1m' | '5m' | '15m' | '1h' | '4h' | '1d' | '1w'
  ms: number
}

export const INTERVALS: CandleInterval[] = [
  { label: '1H', value: '1h', ms: 60 * 60 * 1000 },
  { label: '4H', value: '4h', ms: 4 * 60 * 60 * 1000 },
  { label: '1D', value: '1d', ms: 24 * 60 * 60 * 1000 },
  { label: '1W', value: '1w', ms: 7 * 24 * 60 * 60 * 1000 },
]

export function fetchCandles(
  coin: string,
  interval: CandleInterval['value'],
  startTime: number,
  endTime: number = Date.now(),
): Promise<Candle[]> {
  return post<Candle[]>({
    type: 'candleSnapshot',
    req: { coin, interval, startTime, endTime },
  })
}

export function fetchUserState(user: string): Promise<HLUserState> {
  return post<HLUserState>({ type: 'clearinghouseState', user })
}

// ─── WebSocket ───

type WsHandler = (data: unknown) => void

export class HLSocket {
  private ws?: WebSocket
  private handlers = new Map<string, Set<WsHandler>>()
  private subs: Array<Record<string, unknown>> = []
  private reconnectMs = 1000
  private maxReconnectMs = 30_000
  private alive = true
  private onConnChange?: (connected: boolean) => void

  constructor(onConnChange?: (connected: boolean) => void) {
    this.onConnChange = onConnChange
    this.connect()
  }

  private connect() {
    if (!this.alive) return
    const ws = new WebSocket(HL_WS)
    this.ws = ws
    ws.onopen = () => {
      this.reconnectMs = 1000
      this.onConnChange?.(true)
      for (const s of this.subs) {
        ws.send(JSON.stringify({ method: 'subscribe', subscription: s }))
      }
    }
    ws.onclose = () => {
      this.onConnChange?.(false)
      if (!this.alive) return
      setTimeout(() => this.connect(), this.reconnectMs)
      this.reconnectMs = Math.min(this.reconnectMs * 2, this.maxReconnectMs)
    }
    ws.onerror = () => ws.close()
    ws.onmessage = (e) => {
      try {
        const msg = JSON.parse(e.data) as { channel?: string; data?: unknown }
        if (!msg.channel) return
        const set = this.handlers.get(msg.channel)
        if (set) for (const fn of set) fn(msg.data)
      } catch {
        // ignore
      }
    }
  }

  subscribe(sub: Record<string, unknown>, channel: string, handler: WsHandler) {
    this.subs.push(sub)
    let set = this.handlers.get(channel)
    if (!set) {
      set = new Set()
      this.handlers.set(channel, set)
    }
    set.add(handler)
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify({ method: 'subscribe', subscription: sub }))
    }
    return () => {
      set!.delete(handler)
    }
  }

  close() {
    this.alive = false
    this.ws?.close()
  }
}

let sharedSocket: HLSocket | undefined
const connListeners = new Set<(c: boolean) => void>()

export function getSharedSocket(): HLSocket {
  if (!sharedSocket) {
    sharedSocket = new HLSocket((c) => {
      for (const fn of connListeners) fn(c)
    })
  }
  return sharedSocket
}

export function onConnectionChange(fn: (c: boolean) => void): () => void {
  connListeners.add(fn)
  return () => connListeners.delete(fn)
}
