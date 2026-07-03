import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom'
import './index.css'
import { AppShell } from './components/shell/AppShell'
import DashboardPage from './pages/DashboardPage'
import PortfolioPage from './pages/PortfolioPage'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <BrowserRouter>
      <Routes>
        <Route element={<AppShell />}>
          <Route path="/" element={<Navigate to="/dashboard" replace />} />
          <Route path="/dashboard" element={<DashboardPage />} />
          <Route path="/dashboard/portfolio" element={<PortfolioPage />} />
          <Route
            path="/dashboard/branches"
            element={<Navigate to="/dashboard/portfolio" replace />}
          />
        </Route>
      </Routes>
    </BrowserRouter>
  </StrictMode>,
)
