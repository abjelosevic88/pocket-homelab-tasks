import React from 'react'
import ReactDOM from 'react-dom/client'
import { BrowserRouter, Navigate, Route, Routes, useLocation } from 'react-router-dom'
import './styles/base.css'
import './styles/app.css'
import './styles/tasks.css'
import './styles/settings.css'
import { AppProvider, useApp } from './lib/app-context'
import { Loading, ToastProvider } from './components/ui'
import Layout from './components/Layout'
import Login from './pages/Login'
import { ViewPage, ListPage, TagPage, FilterPage } from './pages/Views'
import Calendar from './pages/Calendar'
import Search from './pages/Search'
import Trash from './pages/Trash'
import Settings from './pages/Settings'
import { ErrorBoundary } from './components/ErrorBoundary'

function Gate({ children }: { children: React.ReactNode }) {
  const { status, user } = useApp()
  const loc = useLocation()
  if (!status) return <div className="boot"><img src="/favicon.svg" alt="" /><Loading /></div>
  if (status.needs_setup) return <Navigate to="/setup" replace />
  if (!user) return <Navigate to="/login" replace state={{ from: loc.pathname + loc.search }} />
  return <>{children}</>
}

function App() {
  return (
    <Routes>
      <Route path="/login" element={<Login mode="login" />} />
      <Route path="/setup" element={<Login mode="setup" />} />
      <Route path="/settings/:tab?" element={<Gate><ErrorBoundary where="settings"><Settings /></ErrorBoundary></Gate>} />
      <Route element={<Gate><Layout /></Gate>}>
        <Route path="/" element={<Navigate to="/today" replace />} />
        <Route path="/inbox" element={<ViewPage view="inbox" />} />
        <Route path="/today" element={<ViewPage view="today" />} />
        <Route path="/upcoming" element={<ViewPage view="upcoming" />} />
        <Route path="/anytime" element={<ViewPage view="anytime" />} />
        <Route path="/someday" element={<ViewPage view="someday" />} />
        <Route path="/logbook" element={<ViewPage view="logbook" />} />
        <Route path="/list/:id" element={<ListPage />} />
        <Route path="/tag/:tag" element={<TagPage />} />
        <Route path="/filter/:id" element={<FilterPage />} />
        <Route path="/calendar" element={<Calendar />} />
        <Route path="/search" element={<Search />} />
        <Route path="/trash" element={<Trash />} />
        <Route path="*" element={<Navigate to="/today" replace />} />
      </Route>
    </Routes>
  )
}

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <BrowserRouter>
      <ErrorBoundary where="app"><ToastProvider><AppProvider><App /></AppProvider></ToastProvider></ErrorBoundary>
    </BrowserRouter>
  </React.StrictMode>,
)
