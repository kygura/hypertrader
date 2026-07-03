// Shared types for HYPERTRADE

export type AssetKind = 'PERP' | 'SPOT'

export interface AssetMeta {
  name: string
  kind: AssetKind
  szDecimals: number
  maxLeverage: number
}

export interface AssetCtx {
  name: string
  kind: AssetKind
  markPx: number
  oraclePx: number
  midPx: number
  dayNtlVlm: number // 24h notional volume USD
  prevDayPx: number
  openInterest: number // base asset units (perp)
  funding: number // perp only
  premium: number
  dayChange: number // fraction
  dayHigh: number
  dayLow: number
}

export interface Candle {
  t: number // open ms
  T: number // close ms
  o: number
  h: number
  l: number
  c: number
  v: number
}

export interface AllMids {
  [coin: string]: string
}

// User state from Hyperliquid clearinghouse
export interface HLPosition {
  coin: string
  szi: string // signed size
  entryPx?: string
  positionValue: string
  unrealizedPnl: string
  liquidationPx?: string | null
  leverage: { type: 'cross' | 'isolated'; value: number; rawUsd?: string }
  marginUsed: string
  returnOnEquity: string
}

export interface HLUserState {
  marginSummary: {
    accountValue: string
    totalNtlPos: string
    totalRawUsd: string
    totalMarginUsed: string
  }
  crossMarginSummary: {
    accountValue: string
    totalNtlPos: string
    totalRawUsd: string
    totalMarginUsed: string
  }
  withdrawable: string
  assetPositions: Array<{
    position: HLPosition
    type: 'oneWay'
  }>
}

// ─── Branching engine ───

export type Side = 'long' | 'short'
export type MarginMode = 'cross' | 'isolated'

export interface BranchPosition {
  id: string
  asset: string
  side: Side
  marginMode: MarginMode
  leverage: number
  marginUsd: number
  entryPrice: number
  entryDate: number // unix ms
  exitPrice?: number
  exitDate?: number
  liquidatedAt?: number
  liquidatedPrice?: number
  notes?: string
}

export type OrderType = 'limit' | 'stop'

export interface PendingOrder {
  id: string
  asset: string
  type: OrderType
  side: Side
  marginMode: MarginMode
  leverage: number
  marginUsd: number
  price: number
  size: number
  createdAt: number
  tp?: number
  sl?: number
}

export interface Branch {
  id: string
  name: string
  color: string
  parentId?: string
  forkDate?: number // unix ms — only set for forked branches
  createdAt: number
  startingBalance: number
  positions: BranchPosition[]
  pendingOrders?: PendingOrder[]
}

// Alias surface — same shape, exposed to UI as "Portfolio"
export type Portfolio = Branch
export type PortfolioPosition = BranchPosition

export interface PositionState {
  markPrice: number
  notional: number // current notional value (USD)
  upnl: number
  marginUsed: number
  maintenanceMargin: number
  liqPrice: number
  isLiquidated: boolean
  isClosed: boolean
  pnlPct: number
}

export interface AccountState {
  crossEquity: number
  isoEquity: number
  totalEquity: number
  marginUsed: number
  available: number
  upnl: number
  maintenanceMargin: number
  isLiquidated: boolean
  maxWithdraw: number
  leverage: number
}

export interface PortfolioImport {
  name: string
  color?: string
  startingBalance: number
  startDate?: number
  positions: Array<Omit<BranchPosition, 'id'> & { id?: string }>
  pendingOrders?: Array<Omit<PendingOrder, 'id'> & { id?: string }>
}
