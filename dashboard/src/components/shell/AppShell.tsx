import { Outlet } from 'react-router-dom'
import { TopNav } from './TopNav'
import { BottomTicker } from './BottomTicker'

export function AppShell() {
  return (
    <div className="flex flex-col h-full w-full">
      <TopNav />
      <main className="flex-1 min-h-0 min-w-0 overflow-hidden">
        <Outlet />
      </main>
      <BottomTicker />
    </div>
  )
}
