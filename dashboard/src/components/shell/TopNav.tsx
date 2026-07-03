import { NavLink } from 'react-router-dom'
import { useAllMids } from '../../hooks/useHLStream'

const navItems = [
  { to: '/dashboard', label: 'DASHBOARD' },
  { to: '/dashboard/portfolio', label: 'PORTFOLIO' },
]

export function TopNav() {
  const { mids, connected } = useAllMids()
  const btc = mids?.['BTC'] ? Number(mids['BTC']) : null
  const eth = mids?.['ETH'] ? Number(mids['ETH']) : null

  return (
    <header className="h-10 flex-shrink-0 flex items-center px-3 border-b border-border bg-panel-alt">
      {/* Logo */}
      <div className="flex items-center gap-2 pr-4 mr-2 border-r border-border h-full">
        <span className="font-mono text-[13px] font-semibold tracking-tight text-text-primary">
          HYPERTRADE
        </span>
        <span className="inline-block w-1.5 h-1.5 bg-red-accent" />
      </div>

      {/* Nav */}
      <nav className="flex h-full">
        {navItems.map((item) => (
          <NavLink
            key={item.to}
            to={item.to}
            end={item.to === '/dashboard'}
            className={({ isActive }) =>
              `px-3 flex items-center text-[10px] uppercase tracking-wider font-medium h-full transition-colors ${
                isActive
                  ? 'text-text-primary border-b-2 border-red-accent'
                  : 'text-text-secondary hover:text-text-primary'
              }`
            }
          >
            {item.label}
          </NavLink>
        ))}
      </nav>

      {/* Right side */}
      <div className="ml-auto flex items-center gap-4 text-[10px] uppercase tracking-wider">
        <div className="flex items-center gap-1.5">
          <span
            className={`inline-block w-1.5 h-1.5 ${
              connected ? 'bg-green' : 'bg-red'
            }`}
            style={{ boxShadow: connected ? '0 0 6px var(--green)' : undefined }}
          />
          <span className="text-text-secondary">
            {connected ? 'CONNECTED' : 'OFFLINE'}
          </span>
        </div>
        <div className="flex items-center gap-3 font-mono text-[11px] tabular-nums">
          <span className="text-text-secondary">
            ₿{' '}
            <span className="text-text-primary">
              {btc ? `$${btc.toLocaleString(undefined, { maximumFractionDigits: 0 })}` : '--'}
            </span>
          </span>
          <span className="text-text-secondary">
            Ξ{' '}
            <span className="text-text-primary">
              {eth ? `$${eth.toLocaleString(undefined, { maximumFractionDigits: 0 })}` : '--'}
            </span>
          </span>
        </div>
      </div>
    </header>
  )
}
