// Thin client for the hyperagent daemon's unified backend core
// (tui/internal/api). The dashboard keeps its direct Hyperliquid link for
// public market data (see hl-client.ts) — this client only reaches for what
// the daemon uniquely owns: connection/health state and its push stream.

export const CORE_URL =
  (import.meta.env.VITE_CORE_URL as string | undefined) ??
  'http://127.0.0.1:8787'

const CORE_WS_URL = `${CORE_URL.replace(/^http/, 'ws')}/api/ws`

export interface CoreHealth {
  connected: boolean
  mode: string
  providers: { batch: string; chat: string }
  version: string
}

export interface CoreFrame {
  topic: string
  data: unknown
}

// fetchHealth probes the daemon. Any failure (unreachable, timeout, bad
// response) resolves to null rather than throwing — callers treat null as
// "offline," not an error to handle.
export async function fetchHealth(): Promise<CoreHealth | null> {
  try {
    const res = await fetch(`${CORE_URL}/api/health`, {
      signal: AbortSignal.timeout(1500),
    })
    if (!res.ok) return null
    return (await res.json()) as CoreHealth
  } catch {
    return null
  }
}

const FETCH_TIMEOUT_MS = 5000
const CHAT_TIMEOUT_MS = 60_000

// ─── Types ──────────────────────────────────────────────────────────────
// TS types below are hand-derived from the Go JSON producers, not guessed —
// each comment names the source file/type it mirrors.

// Mirrors tui/internal/metrics/verdict.go Action (string enum).
export type CoreAction =
  | 'open_short'
  | 'open_long'
  | 'close'
  | 'scale'
  | 'hold'
  | 'alert_only'

// Mirrors tui/internal/metrics/verdict.go Entry (json tags: type, price,omitempty).
export interface CoreEntry {
  type: string
  price?: number
}

// Mirrors tui/internal/metrics/verdict.go Verdict. At/Provider/RawText carry
// `json:"-"` in Go and are excluded here — the wire body never has them.
export interface CoreVerdict {
  asset: string
  timeframe: string
  action: CoreAction
  size_usd: number
  entry: CoreEntry
  stop: number
  take_profit: number
  thesis: string
  reading: string
  confidence: number
  requires_confirmation: boolean
}

// Mirrors tui/internal/metrics/types.go Bar. Bar has no json tags in Go, so
// field names on the wire are the Go field names verbatim (PascalCase), and
// the two time.Time fields marshal as RFC3339 strings.
export interface CoreBar {
  Coin: string
  Timeframe: string
  OpenTime: string
  CloseTime: string
  Final: boolean
  Open: number
  High: number
  Low: number
  Close: number
  Volume: number
  BuyVolume: number
  SellVolume: number
  TradeCount: number
  LargePrint: boolean
  CVD: number
  TradeImbal: number
  Funding: number
  FundingDelta: number
  OpenInterest: number
  OIDelta: number
  Basis: number
  MarkPrice: number
  Return: number
  RealizedVol: number
  RangePos: number
  LiqProx: number
  BTCCorr: number
  RelStrength: number
}

// Mirrors tui/internal/metrics/types.go AssetCtx — also untagged, PascalCase
// wire fields; Time marshals as RFC3339.
export interface CoreAssetCtx {
  Coin: string
  MarkPrice: number
  OraclePrice: number
  Funding: number
  OpenInterest: number
  Premium: number
  DayVolume: number
  Time: string
}

// Mirrors the unexported marketEntry in tui/internal/api/read.go
// (GET /api/markets): json tags coin/bar/mid/asset_ctx wrapping the untagged
// Bar/AssetCtx structs above.
export interface CoreMarket {
  coin: string
  bar: CoreBar
  mid: number
  asset_ctx: CoreAssetCtx
}

// Mirrors tui/internal/journal/journal.go Entry.
export interface CoreJournalEntry {
  time: string
  coin: string
  kind: string
  summary: string
  verdict?: CoreVerdict
}

// Mirrors tui/internal/executor/proposals.go Proposal.
export interface CoreProposal {
  id: string
  verdict: CoreVerdict
  created: string
  expires: string
}

// Mirrors the chatTurn wire struct in tui/internal/api/act.go (json tags
// role/text).
export interface ChatTurn {
  role: 'user' | 'assistant'
  text: string
}

// Mirrors the map[string]string handleChat writes on success in
// tui/internal/api/act.go (reply/provider/model).
export interface ChatReply {
  reply: string
  provider: string
  model: string
}

// extractError reads the `{"error":"..."}` envelope every non-2xx handler in
// tui/internal/api writes (see writeErr in server.go). Falls back to the
// status text if the body isn't JSON-shaped as expected.
async function extractError(res: Response): Promise<string> {
  try {
    const body = (await res.json()) as { error?: string }
    if (body && typeof body.error === 'string') return body.error
  } catch {
    // not a JSON body
  }
  return `${res.status} ${res.statusText}`
}

function errMessage(err: unknown): string {
  if (err instanceof DOMException && err.name === 'TimeoutError') return 'request timed out'
  if (err instanceof Error) return err.message
  return 'network error'
}

// fetchVerdicts serves GET /api/verdicts — the latest verdict per asset,
// newest-first. An empty watchlist is a normal 200 [] on the Go side; this
// still surfaces as [] here (not null) since it's a legitimate empty state,
// not a failure.
export async function fetchVerdicts(): Promise<CoreVerdict[] | null> {
  try {
    const res = await fetch(`${CORE_URL}/api/verdicts`, {
      signal: AbortSignal.timeout(FETCH_TIMEOUT_MS),
    })
    if (!res.ok) return null
    return (await res.json()) as CoreVerdict[]
  } catch {
    return null
  }
}

// fetchJournal serves GET /api/journal?date=YYYY-MM-DD (date defaults server
// side to today UTC when omitted).
export async function fetchJournal(date?: string): Promise<CoreJournalEntry[] | null> {
  try {
    const qs = date ? `?date=${encodeURIComponent(date)}` : ''
    const res = await fetch(`${CORE_URL}/api/journal${qs}`, {
      signal: AbortSignal.timeout(FETCH_TIMEOUT_MS),
    })
    if (!res.ok) return null
    return (await res.json()) as CoreJournalEntry[]
  } catch {
    return null
  }
}

// fetchProposals serves GET /api/proposals — 503 if no signer is configured,
// surfaced here as null same as any other unreachable/failed state.
export async function fetchProposals(): Promise<CoreProposal[] | null> {
  try {
    const res = await fetch(`${CORE_URL}/api/proposals`, {
      signal: AbortSignal.timeout(FETCH_TIMEOUT_MS),
    })
    if (!res.ok) return null
    return (await res.json()) as CoreProposal[]
  } catch {
    return null
  }
}

// approveProposal calls POST /api/proposals/{id}/approve. Returns null on
// success (200), or the verbatim `{error}` string from the daemon on a
// 404 (expired/unknown id) or 422 (risk-gate rejection) — that gate message
// is the product story per the spec and must never be swallowed.
export async function approveProposal(id: string): Promise<string | null> {
  try {
    const res = await fetch(`${CORE_URL}/api/proposals/${encodeURIComponent(id)}/approve`, {
      method: 'POST',
      signal: AbortSignal.timeout(FETCH_TIMEOUT_MS),
    })
    if (res.ok) return null
    return await extractError(res)
  } catch (err) {
    return errMessage(err)
  }
}

// rejectProposal calls POST /api/proposals/{id}/reject; same error contract
// as approveProposal.
export async function rejectProposal(id: string): Promise<string | null> {
  try {
    const res = await fetch(`${CORE_URL}/api/proposals/${encodeURIComponent(id)}/reject`, {
      method: 'POST',
      signal: AbortSignal.timeout(FETCH_TIMEOUT_MS),
    })
    if (res.ok) return null
    return await extractError(res)
  } catch (err) {
    return errMessage(err)
  }
}

// fetchCoreMarkets serves GET /api/markets — one row per tracked coin with a
// finalized bar. 404 ("store warming up") and any network failure both
// surface as null; callers render the same offline/skeleton state either way.
export async function fetchCoreMarkets(): Promise<CoreMarket[] | null> {
  try {
    const res = await fetch(`${CORE_URL}/api/markets`, {
      signal: AbortSignal.timeout(FETCH_TIMEOUT_MS),
    })
    if (!res.ok) return null
    return (await res.json()) as CoreMarket[]
  } catch {
    return null
  }
}

// fetchCoreBars serves GET /api/bars/{coin}?tf=&n= — tf/n both optional
// server side (tf defaults to the coin's configured timeframe, n defaults to
// 100, capped at 1000).
export async function fetchCoreBars(
  coin: string,
  tf?: string,
  n?: number,
): Promise<CoreBar[] | null> {
  try {
    const params = new URLSearchParams()
    if (tf) params.set('tf', tf)
    if (n) params.set('n', String(n))
    const qs = params.toString()
    const res = await fetch(
      `${CORE_URL}/api/bars/${encodeURIComponent(coin)}${qs ? `?${qs}` : ''}`,
      { signal: AbortSignal.timeout(FETCH_TIMEOUT_MS) },
    )
    if (!res.ok) return null
    return (await res.json()) as CoreBar[]
  } catch {
    return null
  }
}

// postChat calls POST /api/chat with a 60s timeout (LLM round trip). Unlike
// the other fetchers this never resolves to null — a failure (network,
// timeout, or a non-2xx response) resolves to `{error}` so the chat drawer
// can render the failure as a message in the transcript rather than losing it.
export async function postChat(
  message: string,
  history: ChatTurn[],
): Promise<ChatReply | { error: string }> {
  try {
    const res = await fetch(`${CORE_URL}/api/chat`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ message, history }),
      signal: AbortSignal.timeout(CHAT_TIMEOUT_MS),
    })
    if (!res.ok) {
      return { error: await extractError(res) }
    }
    return (await res.json()) as ChatReply
  } catch (err) {
    return { error: errMessage(err) }
  }
}

// coreWS opens the daemon's push stream and calls onFrame for each decoded
// frame, reconnecting with exponential backoff (capped 30s) if the daemon is
// down or restarts. Returns a cleanup function that stops reconnecting and
// closes the socket.
export function coreWS(onFrame: (frame: CoreFrame) => void): () => void {
  let alive = true
  let ws: WebSocket | undefined
  let reconnectMs = 1000
  const maxReconnectMs = 30_000
  let timer: ReturnType<typeof setTimeout> | undefined

  const connect = () => {
    if (!alive) return
    ws = new WebSocket(CORE_WS_URL)
    ws.onopen = () => {
      reconnectMs = 1000
    }
    ws.onclose = () => {
      if (!alive) return
      timer = setTimeout(connect, reconnectMs)
      reconnectMs = Math.min(reconnectMs * 2, maxReconnectMs)
    }
    ws.onerror = () => ws?.close()
    ws.onmessage = (e) => {
      try {
        onFrame(JSON.parse(e.data as string) as CoreFrame)
      } catch {
        // ignore malformed frame
      }
    }
  }
  connect()

  return () => {
    alive = false
    if (timer) clearTimeout(timer)
    ws?.close()
  }
}
