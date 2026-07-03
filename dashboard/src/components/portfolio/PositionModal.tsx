import { useEffect, useMemo, useState } from 'react'
import type {
  Branch,
  BranchPosition,
  MarginMode,
  PendingOrder,
  Side,
} from '../../lib/types'
import { ASSETS, getMaxLeverage, priceAt } from '../../lib/price-data'
import { accountStateAt, liqPrice } from '../../lib/margin-engine'
import { lastIdx, openPositions } from '../../lib/portfolio-derive'
import {
  classForPnl,
  fmtPct,
  fmtPrice,
  fmtSize,
  fmtUsd,
} from '../../lib/metric-fmt'
import { CustomSlider } from './CustomSlider'
import { AnimatedDigits } from './AnimatedDigits'
import { LeverageDialog } from './LeverageDialog'

type OrderKind = 'MARKET' | 'LIMIT' | 'PRO'

interface Props {
  branch: Branch
  initialAsset: string
  onClose: () => void
  onSubmitPosition: (pos: Omit<BranchPosition, 'id'>) => void
  onSubmitOrder: (order: Omit<PendingOrder, 'id'>) => void
}

export function PositionModal({
  branch,
  initialAsset,
  onClose,
  onSubmitPosition,
  onSubmitOrder,
}: Props) {
  const [marginMode, setMarginMode] = useState<MarginMode>('cross')
  const [asset, setAsset] = useState(
    ASSETS.includes(initialAsset as (typeof ASSETS)[number]) ? initialAsset : 'BTC',
  )
  const maxLev = getMaxLeverage(asset)
  const [leverageRaw, setLeverage] = useState<number>(Math.min(10, maxLev))
  const leverage = Math.min(leverageRaw, maxLev)
  const [levDialogOpen, setLevDialogOpen] = useState(false)
  const [orderKind, setOrderKind] = useState<OrderKind>('MARKET')
  const [side, setSide] = useState<Side>('long')
  const mark = priceAt(asset, lastIdx())
  const [limitPrice, setLimitPrice] = useState<string>('')
  const [sizeUnit, setSizeUnit] = useState<'USD' | 'COIN'>('USD')
  const [sizeInput, setSizeInput] = useState<string>('')
  const [pct, setPct] = useState<number>(0)
  const [tpsl, setTpsl] = useState(false)
  const [tp, setTp] = useState('')
  const [sl, setSl] = useState('')

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  const acct = useMemo(() => accountStateAt(branch, lastIdx()), [branch])
  const available = acct.available
  const currentPosition = useMemo(() => {
    const open = openPositions(branch).filter((p) => p.asset === asset)
    let netUsd = 0
    for (const p of open) netUsd += (p.side === 'long' ? 1 : -1) * p.marginUsd * p.leverage
    return netUsd
  }, [branch, asset])

  const entryPrice = orderKind === 'LIMIT' && Number(limitPrice) > 0 ? Number(limitPrice) : mark

  const marginUsd = useMemo(() => {
    if (pct > 0) return (pct / 100) * available
    const n = Number(sizeInput)
    if (!isFinite(n) || n <= 0) return 0
    if (sizeUnit === 'USD') return n / leverage
    return (n * entryPrice) / leverage
  }, [pct, sizeInput, sizeUnit, leverage, available, entryPrice])

  const orderValue = marginUsd * leverage
  const sizeCoin = entryPrice > 0 ? orderValue / entryPrice : 0
  const liq = entryPrice > 0 ? liqPrice({ side, entryPrice, leverage }) : 0
  const maintenanceRate = leverage <= 10 ? 0.005 : 0.01
  const maintenance = orderValue * maintenanceRate
  void maintenance
  const fee = orderValue * (orderKind === 'MARKET' ? 0.00045 : 0.00015)
  const slippage = orderKind === 'MARKET' ? entryPrice * 0.0005 : 0

  const isValid =
    marginUsd > 0 &&
    marginUsd <= available + 1e-6 &&
    entryPrice > 0 &&
    leverage >= 1 &&
    leverage <= maxLev

  const submitLabel = `${side === 'long' ? 'LONG' : 'SHORT'} ${asset}`

  const submit = () => {
    if (!isValid) return
    if (orderKind === 'LIMIT') {
      onSubmitOrder({
        asset,
        type: 'limit',
        side,
        marginMode,
        leverage,
        marginUsd,
        price: entryPrice,
        size: sizeCoin,
        createdAt: Date.now(),
        tp: tpsl && Number(tp) > 0 ? Number(tp) : undefined,
        sl: tpsl && Number(sl) > 0 ? Number(sl) : undefined,
      })
    } else {
      onSubmitPosition({
        asset,
        side,
        marginMode,
        leverage,
        marginUsd,
        entryPrice,
        entryDate: Date.now(),
      })
    }
  }

  return (
    <div
      className="fixed inset-0 z-50 flex"
      style={{ background: 'rgba(0,0,0,0.4)' }}
      onClick={onClose}
    >
      <div
        className="ml-auto h-full w-[360px] bg-panel border-l border-border flex flex-col overflow-auto"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div className="flex items-center justify-between px-3 py-2 border-b border-border bg-panel-alt">
          <div className="flex items-center gap-1">
            <Segmented
              value={marginMode}
              options={[
                { v: 'cross', l: 'Cross' },
                { v: 'isolated', l: 'Isolated' },
              ]}
              onChange={(v) => setMarginMode(v as MarginMode)}
            />
            <button
              onClick={() => setLevDialogOpen(true)}
              className="text-[11px] font-mono px-2 py-1 bg-elevated border border-border text-text-primary inline-flex items-baseline"
              title="Adjust leverage"
            >
              <AnimatedDigits
                text={String(leverage)}
                charWidth="0.62em"
                height="1.05em"
              />
              <span>×</span>
            </button>
          </div>
          <button
            onClick={onClose}
            className="text-text-secondary hover:text-text-primary text-[14px] px-2"
            aria-label="Close"
          >
            ×
          </button>
        </div>

        {/* Asset selector */}
        <div className="px-3 py-2 border-b border-border">
          <div className="flex items-center justify-between">
            <span className="label">Asset</span>
            <select
              value={asset}
              onChange={(e) => setAsset(e.target.value)}
              className="text-[12px] font-mono px-2 py-1"
            >
              {ASSETS.map((a) => (
                <option key={a} value={a}>
                  {a}
                </option>
              ))}
            </select>
          </div>
        </div>

        {/* Order type tabs */}
        <div className="flex border-b border-border bg-panel-alt">
          {(['MARKET', 'LIMIT', 'PRO'] as OrderKind[]).map((k) => (
            <button
              key={k}
              disabled={k === 'PRO'}
              onClick={() => setOrderKind(k)}
              className={`px-3 py-2 text-[10px] uppercase tracking-wider ${
                orderKind === k
                  ? 'text-text-primary tab-active'
                  : k === 'PRO'
                    ? 'text-text-secondary/50 cursor-not-allowed'
                    : 'text-text-secondary hover:text-text-primary'
              }`}
            >
              {k}
            </button>
          ))}
        </div>

        {/* Stat rows */}
        <div className="px-3 py-2 border-b border-border space-y-1">
          <KvRow label="Available to Trade" value={fmtUsd(available)} />
          <KvRow
            label="Current Position"
            value={fmtUsd(currentPosition, { sign: true })}
            colorClass={classForPnl(currentPosition)}
          />
        </div>

        {/* Long / Short split */}
        <div className="px-3 py-2 flex gap-1">
          <button
            onClick={() => setSide('long')}
            className={`flex-1 py-2 text-[11px] uppercase tracking-wider ${
              side === 'long'
                ? 'bg-green/15 text-green border border-green/40'
                : 'bg-panel-alt text-text-secondary border border-border hover:text-text-primary'
            }`}
          >
            Long
          </button>
          <button
            onClick={() => setSide('short')}
            className={`flex-1 py-2 text-[11px] uppercase tracking-wider ${
              side === 'short'
                ? 'bg-red/15 text-red border border-red/40'
                : 'bg-panel-alt text-text-secondary border border-border hover:text-text-primary'
            }`}
          >
            Short
          </button>
        </div>

        {/* Limit price */}
        {orderKind === 'LIMIT' && (
          <div className="px-3 pb-2">
            <div className="flex items-center justify-between mb-1">
              <span className="label">Limit Price (USD)</span>
              <button
                onClick={() => setLimitPrice(String(mark.toFixed(2)))}
                className="text-[9px] uppercase tracking-wider px-1.5 py-0.5 border border-border text-text-secondary hover:text-text-primary"
              >
                Mid
              </button>
            </div>
            <input
              value={limitPrice}
              onChange={(e) => setLimitPrice(e.target.value)}
              placeholder={fmtPrice(mark)}
              className="w-full"
            />
          </div>
        )}

        {/* Size */}
        <div className="px-3 pb-2">
          <div className="flex items-center justify-between mb-1">
            <span className="label">Size</span>
            <Segmented
              value={sizeUnit}
              options={[
                { v: 'USD', l: 'USD' },
                { v: 'COIN', l: asset },
              ]}
              onChange={(v) => setSizeUnit(v as 'USD' | 'COIN')}
            />
          </div>
          <input
            value={sizeInput}
            onChange={(e) => {
              setSizeInput(e.target.value)
              setPct(0)
            }}
            placeholder="0.00"
            className="w-full"
          />
        </div>

        {/* Percent slider */}
        <div className="px-3 pb-2">
          <div className="flex items-center gap-2">
            <div className="flex-1">
              <CustomSlider
                value={pct}
                min={0}
                max={100}
                step={1}
                ticks={[25, 50, 75]}
                onChange={(v) => {
                  setPct(v)
                  setSizeInput('')
                }}
                ariaLabel="Position size percent"
              />
            </div>
            <div className="w-14 h-8 px-2 flex items-center justify-end gap-0.5 border border-border bg-panel-alt">
              <input
                value={pct}
                onChange={(e) => {
                  const n = Math.max(0, Math.min(100, Number(e.target.value) || 0))
                  setPct(n)
                  setSizeInput('')
                }}
                className="w-full text-right bg-transparent border-0 p-0 text-[11px]"
              />
              <span className="text-[10px] text-text-secondary">%</span>
            </div>
          </div>
        </div>

        {/* TP/SL */}
        <div className="px-3 pb-2 border-b border-border">
          <label className="flex items-center gap-2 text-[11px] cursor-pointer">
            <input
              type="checkbox"
              checked={tpsl}
              onChange={(e) => setTpsl(e.target.checked)}
            />
            <span className="text-text-secondary uppercase tracking-wider text-[10px]">
              Take Profit / Stop Loss
            </span>
          </label>
          {tpsl && (
            <div className="grid grid-cols-2 gap-2 mt-2">
              <div>
                <div className="label mb-1">TP Price</div>
                <input value={tp} onChange={(e) => setTp(e.target.value)} className="w-full" />
              </div>
              <div>
                <div className="label mb-1">SL Price</div>
                <input value={sl} onChange={(e) => setSl(e.target.value)} className="w-full" />
              </div>
            </div>
          )}
        </div>

        {/* Submit */}
        <div className="px-3 py-3">
          <button
            onClick={submit}
            disabled={!isValid}
            className={`w-full py-2.5 text-[12px] uppercase tracking-wider font-medium ${
              !isValid
                ? 'bg-elevated text-text-secondary cursor-not-allowed'
                : side === 'long'
                  ? 'bg-green/20 text-green border border-green/60 hover:bg-green/30'
                  : 'bg-red/20 text-red border border-red/60 hover:bg-red/30'
            }`}
          >
            {submitLabel}
          </button>
        </div>

        {/* Footer key/values */}
        <div className="px-3 pb-3 space-y-1 border-t border-border pt-2">
          <KvRow label="Liquidation Price" value={fmtPrice(liq)} colorClass="text-red" />
          <KvRow label="Order Value" value={fmtUsd(orderValue)} />
          <KvRow label="Margin Required" value={fmtUsd(marginUsd)} />
          <KvRow label="Size" value={`${fmtSize(sizeCoin, 6)} ${asset}`} />
          {orderKind === 'MARKET' && (
            <KvRow label="Slippage" value={fmtPrice(slippage)} />
          )}
          <KvRow
            label="Fees"
            value={`${fmtPct(orderKind === 'MARKET' ? 0.00045 : 0.00015, { decimals: 4 })} · ${fmtUsd(fee, { decimals: 4 })}`}
          />
        </div>
      </div>

      {levDialogOpen && (
        <LeverageDialog
          asset={asset}
          current={leverage}
          max={maxLev}
          onCancel={() => setLevDialogOpen(false)}
          onConfirm={(v) => {
            setLeverage(v)
            setLevDialogOpen(false)
          }}
        />
      )}
    </div>
  )
}

function KvRow({
  label,
  value,
  colorClass,
}: {
  label: string
  value: string
  colorClass?: string
}) {
  return (
    <div className="flex items-center justify-between">
      <span className="text-[10px] uppercase tracking-wider text-text-secondary">
        {label}
      </span>
      <span
        className={`text-[11px] font-mono tabular-nums ${colorClass ?? 'text-text-primary'}`}
      >
        {value}
      </span>
    </div>
  )
}

function Segmented<T extends string>({
  value,
  options,
  onChange,
}: {
  value: T
  options: Array<{ v: T; l: string }>
  onChange: (v: T) => void
}) {
  return (
    <div className="inline-flex border border-border">
      {options.map((o) => (
        <button
          key={o.v}
          onClick={() => onChange(o.v)}
          className={`px-2 py-1 text-[10px] uppercase tracking-wider ${
            o.v === value
              ? 'bg-elevated text-text-primary'
              : 'text-text-secondary hover:text-text-primary'
          }`}
        >
          {o.l}
        </button>
      ))}
    </div>
  )
}
