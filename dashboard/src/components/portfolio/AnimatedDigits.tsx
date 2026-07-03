import { useEffect, useRef, useState } from 'react'

interface Props {
  text: string
  charWidth?: string
  height?: string
  className?: string
}

let idCounter = 0
function nextId() {
  return ++idCounter
}

interface CellItem {
  ch: string
  id: number
}

function AnimatedChar({
  ch,
  width,
  height,
}: {
  ch: string
  width: string
  height: string
}) {
  const [stack, setStack] = useState<CellItem[]>(() => [{ ch, id: nextId() }])
  const lastRef = useRef(ch)
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => {
    if (lastRef.current === ch) return
    lastRef.current = ch
    setStack((prev) => {
      const tail = prev[prev.length - 1]
      if (tail.ch === ch) return prev
      return [...prev, { ch, id: nextId() }]
    })
    if (timerRef.current) clearTimeout(timerRef.current)
    timerRef.current = setTimeout(() => {
      setStack((prev) => prev.slice(-1))
    }, 240)
    return () => {
      if (timerRef.current) clearTimeout(timerRef.current)
    }
  }, [ch])

  return (
    <span className="digit-cell" style={{ width, height, lineHeight: height }}>
      {stack.map((item, i) => {
        const isLatest = i === stack.length - 1
        const isAnimating = stack.length > 1
        const cls = !isAnimating
          ? ''
          : isLatest
            ? 'digit-in'
            : 'digit-out'
        return (
          <span key={item.id} className={`digit-slot ${cls}`}>
            {item.ch}
          </span>
        )
      })}
    </span>
  )
}

export function AnimatedDigits({
  text,
  charWidth = '0.6em',
  height = '1em',
  className = '',
}: Props) {
  const chars = text.split('')
  return (
    <span className={`inline-flex items-baseline ${className}`}>
      {chars.map((c, i) => (
        <AnimatedChar
          key={`pos-${i}-${chars.length}`}
          ch={c}
          width={charWidth}
          height={height}
        />
      ))}
    </span>
  )
}
