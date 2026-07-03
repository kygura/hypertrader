// Formatters for portfolio numbers.

export function fmtUsd(
  n: number,
  opts: { decimals?: number; sign?: boolean; compact?: boolean } = {},
): string {
  const { decimals = 2, sign = false, compact = false } = opts
  if (!isFinite(n)) return '$--'
  const abs = Math.abs(n)
  let body: string
  if (compact && abs >= 1_000_000) {
    body = `$${(abs / 1_000_000).toFixed(2)}M`
  } else if (compact && abs >= 1_000) {
    body = `$${(abs / 1_000).toFixed(2)}K`
  } else {
    body = `$${abs.toLocaleString(undefined, {
      minimumFractionDigits: decimals,
      maximumFractionDigits: decimals,
    })}`
  }
  if (n < 0) return `-${body}`
  if (sign && n > 0) return `+${body}`
  return body
}

export function fmtPct(
  n: number,
  opts: { decimals?: number; sign?: boolean } = {},
): string {
  const { decimals = 2, sign = false } = opts
  if (!isFinite(n)) return '--%'
  const v = n * 100
  const body = `${Math.abs(v).toFixed(decimals)}%`
  if (v < 0) return `-${body}`
  if (sign && v > 0) return `+${body}`
  return body
}

export function fmtLev(n: number): string {
  if (!isFinite(n) || n <= 0) return '0×'
  return `${n.toFixed(n < 10 ? 2 : 1)}×`
}

export function fmtPrice(n: number, decimals?: number): string {
  if (!isFinite(n)) return '--'
  const d = decimals ?? (n >= 1000 ? 2 : n >= 1 ? 3 : 5)
  return n.toLocaleString(undefined, {
    minimumFractionDigits: d,
    maximumFractionDigits: d,
  })
}

export function fmtSize(n: number, decimals = 4): string {
  if (!isFinite(n)) return '--'
  return n.toLocaleString(undefined, {
    minimumFractionDigits: 0,
    maximumFractionDigits: decimals,
  })
}

export function fmtDuration(ms: number): string {
  if (!isFinite(ms) || ms <= 0) return '0m'
  const sec = Math.floor(ms / 1000)
  const min = Math.floor(sec / 60)
  const hr = Math.floor(min / 60)
  const day = Math.floor(hr / 24)
  if (day >= 1) return `${day}d ${hr % 24}h`
  if (hr >= 1) return `${hr}h ${min % 60}m`
  if (min >= 1) return `${min}m`
  return `${sec}s`
}

export function fmtDate(ms: number): string {
  return new Date(ms).toLocaleDateString(undefined, {
    month: 'short',
    day: 'numeric',
    year: 'numeric',
  })
}

export function fmtDateShort(ms: number): string {
  return new Date(ms).toLocaleDateString(undefined, {
    month: 'short',
    day: 'numeric',
  })
}

export function fmtDateTime(ms: number): string {
  return new Date(ms).toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

export function colorForPnl(n: number): string {
  if (n > 0) return 'var(--green)'
  if (n < 0) return 'var(--red)'
  return 'var(--text-secondary)'
}

export function classForPnl(n: number): string {
  if (n > 0) return 'text-green'
  if (n < 0) return 'text-red'
  return 'text-text-secondary'
}
