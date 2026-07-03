import { useCallback, useRef } from 'react'

interface Props {
  value: number
  min: number
  max: number
  step?: number
  ticks?: number[]
  onChange: (v: number) => void
  height?: number
  ariaLabel?: string
}

export function CustomSlider({
  value,
  min,
  max,
  step = 1,
  ticks,
  onChange,
  height = 32,
  ariaLabel,
}: Props) {
  const ref = useRef<HTMLDivElement>(null)
  const clamped = Math.max(min, Math.min(max, value))
  const pct = max > min ? ((clamped - min) / (max - min)) * 100 : 0

  const update = useCallback(
    (clientX: number) => {
      const el = ref.current
      if (!el) return
      const rect = el.getBoundingClientRect()
      const ratio = Math.max(0, Math.min(1, (clientX - rect.left) / rect.width))
      let v = min + ratio * (max - min)
      if (step > 0) v = Math.round(v / step) * step
      onChange(Math.max(min, Math.min(max, v)))
    },
    [min, max, step, onChange],
  )

  const onPointerDown = (e: React.PointerEvent<HTMLDivElement>) => {
    e.preventDefault()
    e.currentTarget.setPointerCapture(e.pointerId)
    update(e.clientX)
  }

  const onPointerMove = (e: React.PointerEvent<HTMLDivElement>) => {
    if ((e.buttons & 1) === 0) return
    update(e.clientX)
  }

  const onKeyDown = (e: React.KeyboardEvent<HTMLDivElement>) => {
    const big = (max - min) / 20
    if (e.key === 'ArrowRight' || e.key === 'ArrowUp') {
      e.preventDefault()
      onChange(Math.min(max, clamped + step))
    } else if (e.key === 'ArrowLeft' || e.key === 'ArrowDown') {
      e.preventDefault()
      onChange(Math.max(min, clamped - step))
    } else if (e.key === 'PageUp') {
      e.preventDefault()
      onChange(Math.min(max, clamped + Math.max(step, big)))
    } else if (e.key === 'PageDown') {
      e.preventDefault()
      onChange(Math.max(min, clamped - Math.max(step, big)))
    } else if (e.key === 'Home') {
      e.preventDefault()
      onChange(min)
    } else if (e.key === 'End') {
      e.preventDefault()
      onChange(max)
    }
  }

  return (
    <div
      ref={ref}
      role="slider"
      aria-label={ariaLabel}
      aria-valuemin={min}
      aria-valuemax={max}
      aria-valuenow={clamped}
      tabIndex={0}
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onKeyDown={onKeyDown}
      className="cs-track"
      style={{ height }}
    >
      <div className="cs-fill" style={{ width: `${pct}%` }} />
      {ticks?.map((t) => {
        const p = max > min ? ((t - min) / (max - min)) * 100 : 0
        return <div key={t} className="cs-tick" style={{ left: `${p}%` }} />
      })}
      <div className="cs-thumb" style={{ left: `${pct}%` }} />
    </div>
  )
}
