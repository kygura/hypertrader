import { useEffect, useMemo, useState } from 'react'
import { useUserState } from '../../hooks/useUserState'
import { useAllMids } from '../../hooks/useHLStream'
import type { HLPosition } from '../../lib/types'

const STORAGE_KEY = 'hypertrade.wallets.v1'
const MAX_WALLETS = 4
const ADDR_RE = /^0x[0-9a-fA-F]{40}$/

interface Stored {
  wallets: string[]
  active: number
}

function loadStored(): Stored {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return { wallets: [], active: 0 }
    const parsed = JSON.parse(raw) as Stored
    if (!Array.isArray(parsed.wallets)) return { wallets: [], active: 0 }
    return {
      wallets: parsed.wallets.filter((w) => ADDR_RE.test(w)).slice(0, MAX_WALLETS),
      active:
        typeof parsed.active === 'number' && parsed.active >= 0
          ? parsed.active
          : 0,
    }
  } catch {
    return { wallets: [], active: 0 }
  }
}

function truncate(addr: string) {
  return addr.slice(0, 6) + '…' + addr.slice(-4)
}

function fmtCompact(n: number): string {
  if (!isFinite(n)) return '$0'
  const abs = Math.abs(n)
  const sign = n < 0 ? '-' : ''
  if (abs >= 1e9) return sign + '$' + (abs / 1e9).toFixed(2) + 'B'
  if (abs >= 1e6) return sign + '$' + (abs / 1e6).toFixed(2) + 'M'
  if (abs >= 1e3) return sign + '$' + (abs / 1e3).toFixed(2) + 'K'
  return sign + '$' + abs.toFixed(2)
}

function fmtNum(n: number, d = 4): string {
  if (!isFinite(n)) return '--'
  if (Math.abs(n) >= 1000) return n.toFixed(2)
  if (Math.abs(n) >= 1) return n.toFixed(d)
  return n.toFixed(6)
}

export function WalletTracker() {
  const [{ wallets, active }, setStored] = useState<Stored>(() => loadStored())
  const [input, setInput] = useState('')
  const [err, setErr] = useState<string | null>(null)
  const { mids } = useAllMids()

  useEffect(() => {
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify({ wallets, active }))
    } catch {
      // ignore
    }
  }, [wallets, active])

  const activeAddr = wallets[active] ?? null
  const { state, loading, error } = useUserState(activeAddr)

  const addWallet = () => {
    const v = input.trim()
    if (!ADDR_RE.test(v)) {
      setErr('Invalid address')
      return
    }
    if (wallets.includes(v)) {
      setErr('Already added')
      return
    }
    if (wallets.length >= MAX_WALLETS) {
      setErr('Max 4 wallets')
      return
    }
    const next = [...wallets, v]
    setStored({ wallets: next, active: next.length - 1 })
    setInput('')
    setErr(null)
  }

  const removeWallet = (idx: number) => {
    const next = wallets.filter((_, i) => i !== idx)
    let nextActive = active
    if (idx === active) nextActive = 0
    else if (idx < active) nextActive = active - 1
    setStored({
      wallets: next,
      active: next.length > 0 ? Math.min(nextActive, next.length - 1) : 0,
    })
  }

  const positions = useMemo(() => {
    if (!state) return [] as HLPosition[]
    return state.assetPositions.map((p) => p.position)
  }, [state])

  const summary = state?.marginSummary
  const account = summary ? Number(summary.accountValue) : 0
  const margin = summary ? Number(summary.totalMarginUsed) : 0
  const withdrawable = state ? Number(state.withdrawable) : 0
  const upnl = positions.reduce((s, p) => s + Number(p.unrealizedPnl || 0), 0)

  const Stat = ({
    label,
    value,
    valueClass,
  }: {
    label: string
    value: string
    valueClass?: string
  }) => (
    <div className="flex flex-col gap-0.5 px-3 py-2 border border-border bg-panel-alt min-w-[140px] flex-shrink-0">
      <span className="text-[10px] uppercase tracking-wider text-text-secondary font-medium">
        {label}
      </span>
      <span className={`font-mono tabular-nums text-[13px] ${valueClass ?? 'text-text-primary'}`}>
        {value}
      </span>
    </div>
  )

  return (
    <div className="flex flex-col h-full w-full">
      <div className="flex items-center px-2 py-1 border-b border-border bg-panel-alt flex-shrink-0 gap-1 overflow-x-auto">
        {wallets.map((w, i) => (
          <div
            key={w}
            className={`inline-flex items-center gap-1 px-2 py-1 border ${
              i === active
                ? 'border-red-accent text-text-primary'
                : 'border-border text-text-secondary hover:text-text-primary'
            }`}
          >
            <button
              onClick={() => setStored({ wallets, active: i })}
              className="font-mono text-[11px] tabular-nums"
            >
              {truncate(w)}
            </button>
            <button
              onClick={() => removeWallet(i)}
              className="text-text-secondary hover:text-red-accent text-[12px] leading-none"
              aria-label={`Remove ${truncate(w)}`}
            >
              ×
            </button>
          </div>
        ))}
        {wallets.length < MAX_WALLETS && (
          <div className="flex items-center gap-1 ml-1">
            <input
              value={input}
              onChange={(e) => {
                setInput(e.target.value)
                setErr(null)
              }}
              onKeyDown={(e) => {
                if (e.key === 'Enter') addWallet()
              }}
              placeholder="0x…"
              className="text-[11px] w-[280px]"
            />
            <button
              onClick={addWallet}
              className="px-2 py-1 text-[10px] uppercase tracking-wider border border-border text-text-primary hover:bg-elevated"
            >
              + ADD
            </button>
            {err && (
              <span className="text-[10px] uppercase tracking-wider text-red-accent ml-1">
                {err}
              </span>
            )}
          </div>
        )}
      </div>

      <div className="flex-1 min-h-0 min-w-0 flex flex-col">
        {!activeAddr ? (
          <div className="flex-1 flex items-center justify-center text-text-secondary text-[11px] uppercase tracking-wider">
            ADD A WALLET TO TRACK
          </div>
        ) : (
          <>
            <div className="flex gap-2 px-2 py-2 overflow-x-auto border-b border-border flex-shrink-0">
              <Stat label="ACCOUNT VALUE" value={fmtCompact(account)} />
              <Stat
                label="TOTAL UPNL"
                value={fmtCompact(upnl)}
                valueClass={upnl >= 0 ? 'text-green' : 'text-red'}
              />
              <Stat label="MARGIN USED" value={fmtCompact(margin)} />
              <Stat label="WITHDRAWABLE" value={fmtCompact(withdrawable)} />
            </div>

            <div className="flex-1 min-h-0 overflow-auto">
              {loading && positions.length === 0 ? (
                <div className="h-full flex items-center justify-center text-text-secondary text-[11px] uppercase tracking-wider">
                  LOADING…
                </div>
              ) : error ? (
                <div className="h-full flex items-center justify-center text-red-accent text-[11px] uppercase tracking-wider">
                  ERROR
                </div>
              ) : positions.length === 0 ? (
                <div className="h-full flex items-center justify-center text-text-secondary text-[11px] uppercase tracking-wider">
                  NO POSITIONS
                </div>
              ) : (
                <table className="w-full text-[11px] font-mono tabular-nums">
                  <thead className="sticky top-0 bg-panel-alt">
                    <tr className="text-text-secondary">
                      {[
                        'ASSET',
                        'DIR',
                        'LEV',
                        'SIZE',
                        'ENTRY',
                        'MARK',
                        'LIQ',
                        'PNL',
                        'PNL%',
                      ].map((h) => (
                        <th
                          key={h}
                          className="text-[10px] uppercase tracking-wider font-medium px-2 py-1.5 text-right first:text-left border-b border-border"
                        >
                          {h}
                        </th>
                      ))}
                    </tr>
                  </thead>
                  <tbody>
                    {positions.map((p) => {
                      const szi = Number(p.szi)
                      const long = szi >= 0
                      const entry = Number(p.entryPx ?? 0)
                      const live = mids?.[p.coin]
                      const mark = live ? Number(live) : entry
                      const pnl = Number(p.unrealizedPnl || 0)
                      const roe = Number(p.returnOnEquity || 0)
                      const liq = p.liquidationPx ? Number(p.liquidationPx) : null
                      const bg = long
                        ? 'rgba(56,166,124,0.06)'
                        : 'rgba(188,38,62,0.06)'
                      return (
                        <tr key={p.coin} style={{ background: bg }}>
                          <td className="px-2 py-1.5 text-text-primary">{p.coin}</td>
                          <td
                            className={`px-2 py-1.5 text-right ${
                              long ? 'text-green' : 'text-red'
                            }`}
                          >
                            {long ? 'LONG' : 'SHORT'}
                          </td>
                          <td className="px-2 py-1.5 text-right text-text-primary">
                            {p.leverage.value}x
                          </td>
                          <td className="px-2 py-1.5 text-right text-text-primary">
                            {fmtNum(Math.abs(szi))}
                          </td>
                          <td className="px-2 py-1.5 text-right text-text-primary">
                            {fmtNum(entry)}
                          </td>
                          <td className="px-2 py-1.5 text-right text-text-primary">
                            {fmtNum(mark)}
                          </td>
                          <td className="px-2 py-1.5 text-right text-text-secondary">
                            {liq != null && isFinite(liq) ? fmtNum(liq) : '--'}
                          </td>
                          <td
                            className={`px-2 py-1.5 text-right ${
                              pnl >= 0 ? 'text-green' : 'text-red'
                            }`}
                          >
                            {fmtCompact(pnl)}
                          </td>
                          <td
                            className={`px-2 py-1.5 text-right ${
                              roe >= 0 ? 'text-green' : 'text-red'
                            }`}
                          >
                            {(roe * 100).toFixed(2)}%
                          </td>
                        </tr>
                      )
                    })}
                  </tbody>
                </table>
              )}
            </div>
          </>
        )}
      </div>
    </div>
  )
}
