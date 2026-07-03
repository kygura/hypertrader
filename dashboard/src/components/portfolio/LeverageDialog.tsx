import { useEffect, useState } from 'react'
import { CustomSlider } from './CustomSlider'
import { AnimatedDigits } from './AnimatedDigits'

interface Props {
  asset: string
  current: number
  max: number
  onCancel: () => void
  onConfirm: (v: number) => void
}

export function LeverageDialog({ asset, current, max, onCancel, onConfirm }: Props) {
  const [value, setValue] = useState(current)

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onCancel()
      if (e.key === 'Enter') onConfirm(value)
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onCancel, onConfirm, value])

  const ticks = [1, max]
  for (const q of [0.25, 0.5, 0.75]) {
    ticks.push(Math.round(1 + (max - 1) * q))
  }

  const highRisk = value >= Math.max(20, Math.floor(max * 0.5))

  return (
    <div
      className="fixed inset-0 z-[60] flex items-center justify-center"
      style={{ background: 'rgba(0,0,0,0.55)' }}
      onClick={onCancel}
    >
      <div
        className="bg-panel border border-border w-[420px]"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="px-4 pt-4 pb-3 border-b border-border">
          <div className="text-[14px] font-semibold text-text-primary">
            Adjust Leverage
          </div>
          <div className="text-[11px] text-text-secondary mt-1 leading-snug">
            Set your trading leverage for {asset}. The maximum leverage is {max}×.
          </div>
        </div>

        <div className="px-4 py-4">
          <div className="flex items-center justify-between mb-2">
            <span className="text-[12px] text-text-primary font-medium">Leverage</span>
            <span className="text-[22px] font-mono font-bold text-text-primary tabular-nums inline-flex items-baseline">
              <AnimatedDigits
                text={String(value)}
                charWidth="0.62em"
                height="1.1em"
              />
              <span className="ml-0.5">×</span>
            </span>
          </div>

          <CustomSlider
            value={value}
            min={1}
            max={max}
            step={1}
            ticks={ticks}
            onChange={setValue}
            ariaLabel="Leverage"
          />

          <div className="flex items-center justify-between mt-1.5 text-[10px] font-mono tabular-nums text-text-secondary">
            <span>1×</span>
            <span>{max}×</span>
          </div>

          {highRisk && (
            <div
              className="mt-3 px-3 py-2 text-[11px] leading-snug border"
              style={{
                background: 'rgba(255, 184, 0, 0.08)',
                borderColor: 'rgba(255, 184, 0, 0.35)',
                color: 'var(--amber)',
              }}
            >
              Using higher leverage increases the risk of liquidation.
            </div>
          )}
        </div>

        <div className="px-4 pb-4">
          <button
            onClick={() => onConfirm(value)}
            className="w-full py-2.5 text-[12px] uppercase tracking-wider font-medium bg-red-accent/20 border border-red-accent/60 text-text-primary hover:bg-red-accent/30"
          >
            Confirm
          </button>
        </div>
      </div>
    </div>
  )
}
