// The app shell: sidebar, top bar, task detail, search palette, shortcuts — the Pocket Calendar layout.
import { useEffect, useRef, useState } from 'react'
import { Outlet, useLocation, useNavigate, useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { today } from '../lib/dates'
import { colorFor } from '../lib/format'
import { Icon } from './icons'
import { Sidebar } from './Sidebar'
import { CommandPalette, type PaletteCommand } from './CommandPalette'
import { ShortcutsSheet } from './ShortcutsSheet'
import { ErrorBoundary } from './ErrorBoundary'
import { TaskDetail } from './TaskDetail'
import { QuickAdd } from './QuickAdd'
import { Avatar, type MenuItem, MenuButton, Modal, modalOpen, useIsMobile, useMediaQuery } from './ui'
import { AskButton } from './AskButton'

const SIDEBAR_KEY = 'pt-sidebar'
const section = (path: string) => path.split('/')[1] || 'today'

export default function Layout() {
  const navigate = useNavigate()
  const loc = useLocation()
  const [params, setParams] = useSearchParams()
  const mobile = useIsMobile()
  const narrow = useMediaQuery('(max-width: 1099px)')
  const drawer = mobile || narrow
  const [sidebarOpen, setSidebarOpen] = useState(() => !drawer && localStorage.getItem(SIDEBAR_KEY) !== '0')
  useEffect(() => { setSidebarOpen(!drawer && localStorage.getItem(SIDEBAR_KEY) !== '0') }, [drawer])
  const drawerOpenedAt = useRef(0)
  const toggleSidebar = () => setSidebarOpen(o => { drawerOpenedAt.current = performance.now(); const n = !o; if (!drawer) localStorage.setItem(SIDEBAR_KEY, n ? '1' : '0'); return n })
  const closeDrawer = () => { if (drawer) setSidebarOpen(false) }
  useEffect(() => { if (drawer) setSidebarOpen(false) }, [loc.pathname]) // eslint-disable-line react-hooks/exhaustive-deps

  const [palette, setPalette] = useState(false)
  const [shortcuts, setShortcuts] = useState(false)
  const [newTask, setNewTask] = useState(false)
  const openTask = Number(params.get('task')) || null
  const closeDetail = () => setParams(prev => { const n = new URLSearchParams(prev); n.delete('task'); return n })
  const isTaskPage = /^\/(inbox|today|upcoming|anytime|someday|logbook|list|tag|filter|search|calendar)/.test(loc.pathname)
  const detail = !!openTask && isTaskPage

  useEffect(() => {
    const open = () => setShortcuts(true)
    window.addEventListener('pt:shortcuts', open)
    return () => window.removeEventListener('pt:shortcuts', open)
  }, [])
  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      const mod = e.metaKey || e.ctrlKey
      if (mod && e.key.toLowerCase() === 'k' && !e.shiftKey) { e.preventDefault(); setPalette(p => !p); return }
      if (mod && e.shiftKey && e.key.toLowerCase() === 'n') { e.preventDefault(); setNewTask(true); return }
      if (mod || e.altKey) return
      const t = e.target as HTMLElement
      if (t.closest?.('input, textarea, select, [contenteditable="true"]') || modalOpen() || document.querySelector('.menu, .popover, .select-pop, .sheet-backdrop')) return
      switch (e.key) {
        case '?': e.preventDefault(); setShortcuts(true); break
        case ',': e.preventDefault(); navigate('/settings'); break
        case '[': e.preventDefault(); toggleSidebar(); break
      }
    }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }) // eslint-disable-line react-hooks/exhaustive-deps

  const commands: PaletteCommand[] = [
    { id: 'new', label: 'New task', icon: 'plus', hint: '⌘⇧N', keywords: 'add create todo', run: () => setNewTask(true) },
    { id: 'search', label: 'Search page', icon: 'search', keywords: 'find full text', run: () => navigate('/search') },
    { id: 'trash', label: 'Trash', icon: 'trash', keywords: 'deleted restore', run: () => navigate('/trash') },
    { id: 'settings', label: 'Settings', icon: 'settings', hint: ',', run: () => navigate('/settings') },
    { id: 'lists', label: 'Manage lists and areas', icon: 'folder', keywords: 'settings reorder archive', run: () => navigate('/settings/lists') },
    { id: 'apps', label: 'Connect your apps (CalDAV)', icon: 'smartphone', keywords: 'reminders iphone thunderbird feed webcal', run: () => navigate('/settings/calendar') },
    { id: 'shortcuts', label: 'Keyboard shortcuts', icon: 'keyboard', hint: '?', run: () => setShortcuts(true) },
    { id: 'sidebar', label: sidebarOpen ? 'Hide the sidebar' : 'Show the sidebar', icon: 'sidebar', hint: '[', run: toggleSidebar },
  ]

  const listId = loc.pathname.startsWith('/list/') ? Number(loc.pathname.split('/')[2]) || null : null
  return (
    <div className={`app ${sidebarOpen ? 'with-sidebar' : 'no-sidebar'} ${mobile ? 'is-mobile' : ''} ${drawer ? 'drawer-mode' : ''} ${detail ? 'has-detail' : ''}`}>
      {drawer && sidebarOpen && <div className="drawer-backdrop" onClick={e => { if (e.timeStamp - drawerOpenedAt.current > 400) setSidebarOpen(false) }} />}
      <div className={`sidebar-wrap ${sidebarOpen ? 'open' : ''}`}>
        <Sidebar onNewTask={() => { closeDrawer(); setNewTask(true) }} onNavigate={closeDrawer} />
      </div>
      <div className="main">
        <TopBar onMenu={toggleSidebar} onPalette={() => setPalette(true)} onNewTask={() => setNewTask(true)} mobile={mobile} compact={narrow} sidebarOpen={sidebarOpen} />
        <div className="content-row">
          <main className="content"><ErrorBoundary key={section(loc.pathname)} where={loc.pathname}><Outlet /></ErrorBoundary></main>
          <TaskDetailPanel id={detail ? openTask : null} onClose={closeDetail} />
        </div>
      </div>
      {mobile && !detail && <button type="button" className="fab" aria-label="New task" title="New task" onClick={() => setNewTask(true)}><Icon name="plus" size={26} strokeWidth={2.2} /></button>}
      {palette && <CommandPalette onClose={() => setPalette(false)} commands={commands} />}
      {shortcuts && <ShortcutsSheet onClose={() => setShortcuts(false)} />}
      {newTask && <Modal title="New task" onClose={() => setNewTask(false)} className="new-task-modal">
        <QuickAdd autoFocus defaults={{ list_id: listId, start: loc.pathname === '/today' ? today() : null, someday: loc.pathname === '/someday' || undefined }} onAdded={() => setNewTask(false)} onCancel={() => setNewTask(false)} />
      </Modal>}
    </div>
  )
}

function TaskDetailPanel({ id, onClose }: { id: number | null; onClose: () => void }) {
  const [lastId, setLastId] = useState(id)
  // Keep the contents mounted until the closing transition finishes.
  useEffect(() => {
    if (id !== null) { setLastId(id); return }
    const timer = window.setTimeout(() => setLastId(null), 300)
    return () => window.clearTimeout(timer)
  }, [id])
  const shownId = id ?? lastId
  return (
    <div className={`task-detail-panel ${id !== null ? 'is-open' : ''}`} inert={id === null} aria-hidden={id === null}>
      {shownId !== null && <ErrorBoundary key={'t' + shownId} where="detail"><TaskDetail id={shownId} onClose={onClose} /></ErrorBoundary>}
    </div>
  )
}

const VIEW_TITLES: Record<string, string> = { inbox: 'Inbox', today: 'Today', upcoming: 'Upcoming', anytime: 'Anytime', someday: 'Someday', logbook: 'Logbook', calendar: 'Calendar', search: 'Search', trash: 'Trash' }

function TopBar({ onMenu, onPalette, onNewTask, mobile, compact, sidebarOpen }: { onMenu: () => void; onPalette: () => void; onNewTask: () => void; mobile: boolean; compact: boolean; sidebarOpen: boolean }) {
  const { user, themeMode, setThemeMode, status, setUser, lists, areas, filters } = useApp()
  const navigate = useNavigate()
  const loc = useLocation()
  const [, first, second] = loc.pathname.split('/')
  const signOut = async () => { await api.post(`${V1}/auth/logout`).catch(() => {}); setUser(null); navigate('/login') }
  const avatarMenu = (): MenuItem[] => [
    { heading: user?.name || user?.email || '' },
    { label: 'Profile', icon: 'user', onSelect: () => navigate('/settings/profile') },
    { label: 'Settings', icon: 'settings', onSelect: () => navigate('/settings'), hint: ',' },
    { label: 'Keyboard shortcuts', icon: 'keyboard', onSelect: () => window.dispatchEvent(new CustomEvent('pt:shortcuts')), hint: '?' },
    'sep',
    { heading: 'Appearance' },
    { label: 'System', checked: themeMode === 'system', onSelect: () => setThemeMode('system') },
    { label: 'Light', checked: themeMode === 'light', onSelect: () => setThemeMode('light') },
    { label: 'Dark', checked: themeMode === 'dark', onSelect: () => setThemeMode('dark') },
    ...(status?.auth_mode === 'local' ? ['sep' as const, { label: 'Sign out', icon: 'log-out', onSelect: signOut }] : []),
  ]

  // Breadcrumb: Area › List, Tags › #tag, Filters › name, else the view.
  let crumbs: { label: string; muted?: boolean }[] = []
  if (first === 'list') {
    const l = lists.find(x => String(x.id) === second)
    const a = l && areas.find(x => x.id === l.area_id)
    crumbs = [...(a ? [{ label: a.name, muted: true }] : l?.archived ? [{ label: 'Archive', muted: true }] : []), { label: l?.name || 'List' }]
  } else if (first === 'tag') crumbs = [{ label: 'Tags', muted: true }, { label: '#' + decodeURIComponent(second || '') }]
  else if (first === 'filter') crumbs = [{ label: 'Filters', muted: true }, { label: filters.find(f => String(f.id) === second)?.name || 'Filter' }]
  else crumbs = [{ label: VIEW_TITLES[first || 'today'] || '' }]

  return (
    <header className="topbar">
      <button type="button" className="btn ghost icon-only" onClick={onMenu} aria-label={sidebarOpen ? 'Hide the sidebar' : 'Show the sidebar'} title="Menu ([)"><Icon name={mobile || compact ? 'menu' : 'sidebar'} size={18} /></button>
      <h1 className="range-title">
        {crumbs.map((c, i) => <span key={i} className="crumb-part">{i > 0 && <Icon name="chevron-right" size={16} className="crumb-sep" />}<span className={`ellipsis ${c.muted ? 'crumb' : ''}`}>{c.label}</span></span>)}
      </h1>
      <span className="spacer" />
      {!mobile && (!sidebarOpen || compact) && <button type="button" className="btn ghost icon-only" onClick={onNewTask} aria-label="New task" title="New task (⌘⇧N)"><Icon name="plus" size={19} /></button>}
      <button type="button" className="btn ghost icon-only" onClick={onPalette} aria-label="Search" title="Search (⌘K)"><Icon name="search" size={18} /></button>
      <AskButton />
      <MenuButton label="Account" className="avatar-btn" align="right" width={220} items={avatarMenu}>
        <Avatar name={user?.name} email={user?.email} size={30} color={user ? colorFor(user.id) : undefined} />
      </MenuButton>
    </header>
  )
}
