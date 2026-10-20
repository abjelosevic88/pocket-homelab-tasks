import { useEffect, useMemo, useState } from 'react'
import { NavLink, Outlet, useLocation, useNavigate, useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Area, List, NcSync } from '../lib/types'
import { ago, tint } from '../lib/format'
import { today } from '../lib/dates'
import { Avatar } from './people'
import { Modal, useToast } from './ui'
import { Palette } from './Palette'
import { ErrorBoundary } from './ErrorBoundary'
import { TaskDetail } from './TaskDetail'
import { QuickAdd } from './QuickAdd'
import { ListMenuItems, useListDialogs } from './ListMenu'
import { Dropdown } from './pickers'

const section = (path: string) => path.split('/')[1] || 'today'

export default function Layout() {
  const { user, settings, status, theme, toggleTheme, setUser, lists, areas, tags, filters, dash, sync, setSync, bump } = useApp()
  const { toast } = useToast()
  const [open, setOpen] = useState(false)
  const [palette, setPalette] = useState(false)
  const [newTask, setNewTask] = useState(false)
  const [tagsOpen, setTagsOpen] = useState(() => localStorage.getItem('pt-tags') !== '0')
  const [filtersOpen, setFiltersOpen] = useState(() => localStorage.getItem('pt-filters') !== '0')
  const [syncing, setSyncing] = useState(false)
  const { actions, dialogs } = useListDialogs()
  const navigate = useNavigate()
  const loc = useLocation()
  const [params, setParams] = useSearchParams()
  const title = settings?.title || 'Pocket Tasks'
  const openTask = Number(params.get('task')) || null
  const closeDetail = () => setParams(prev => { const n = new URLSearchParams(prev); n.delete('task'); return n })

  useEffect(() => { localStorage.setItem('pt-tags', tagsOpen ? '1' : '0') }, [tagsOpen])
  useEffect(() => { localStorage.setItem('pt-filters', filtersOpen ? '1' : '0') }, [filtersOpen])
  useEffect(() => { setOpen(false) }, [loc.pathname])

  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      const mod = e.metaKey || e.ctrlKey
      if (mod && e.key.toLowerCase() === 'k' && !e.shiftKey) { e.preventDefault(); setPalette(true) }
      else if (mod && e.shiftKey && e.key.toLowerCase() === 'n') { e.preventDefault(); setNewTask(true) }
    }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [])

  const logout = async () => { await api.post(`${V1}/auth/logout`); setUser(null); navigate('/login') }
  const syncNow = async () => {
    setSyncing(true)
    try { const s = await api.post<NcSync['status']>(`${V1}/sync/nextcloud/run`); setSync(sync ? { ...sync, status: s } : sync); bump(); toast(s.last_error ? `Sync failed: ${s.last_error}` : `Synced: ${s.pulled} in, ${s.pushed} out`, s.last_error ? 'err' : 'ok') } catch (e) { toast((e as Error).message, 'err') } finally { setSyncing(false) }
  }

  const live = useMemo(() => lists.filter(l => !l.archived), [lists])
  const loose = live.filter(l => !l.area_id || !areas.some(a => a.id === l.area_id))
  const syncDot = sync?.status.last_error ? 'bad' : syncing || sync?.status.running ? 'busy' : 'ok'
  const isTaskPage = /^\/(inbox|today|upcoming|anytime|someday|logbook|list|tag|filter|search|calendar)/.test(loc.pathname)

  return (
    <div className={`app ${openTask ? 'has-detail' : ''}`}>
      <aside className={`sidebar ${open ? 'open' : ''}`}>
        <div className="brand"><span className="logo"><img src="/favicon.svg" alt="" /></span><span className="brand-name">{title}</span></div>
        <div className="side-actions">
          <button className="btn primary grow" onClick={() => setNewTask(true)} title="New task (⌘⇧N)">＋ New task</button>
          <button className="btn icon" onClick={() => setPalette(true)} title="Search (⌘K)">⌕</button>
        </div>
        <nav className="side-nav">
          <NavLink to="/inbox" className="nav-item"><span className="ico">▤</span><span className="grow">Inbox</span>{!!dash?.inbox && <small className="count">{dash.inbox}</small>}</NavLink>
          <NavLink to="/today" className="nav-item"><span className="ico">☀</span><span className="grow">Today</span>{!!dash?.today && <small className={`count ${dash.overdue ? 'warn' : ''}`}>{dash.today}</small>}</NavLink>
          <NavLink to="/upcoming" className="nav-item"><span className="ico">▦</span><span className="grow">Upcoming</span></NavLink>
          <NavLink to="/anytime" className="nav-item"><span className="ico">☰</span><span className="grow">Anytime</span></NavLink>
          <NavLink to="/someday" className="nav-item"><span className="ico">☾</span><span className="grow">Someday</span>{!!dash?.someday && <small className="count dim">{dash.someday}</small>}</NavLink>
          <NavLink to="/logbook" className="nav-item"><span className="ico">☑</span><span className="grow">Logbook</span></NavLink>
          <NavLink to="/calendar" className="nav-item"><span className="ico">▦</span><span className="grow">Calendar</span></NavLink>
        </nav>
        <div className="side-scroll">
          <div className="side-head"><span>Areas</span><span className="row" style={{ gap: 2 }}><button className="btn ghost xs" title="New area" onClick={() => actions.newArea()}>＋ area</button></span></div>
          <ul className="tree">
            {areas.map(a => <AreaBranch key={a.id} area={a} lists={live.filter(l => l.area_id === a.id)} actions={actions} />)}
            {loose.map(l => <ListLeaf key={l.id} list={l} actions={actions} />)}
          </ul>
          <button className="side-item-add" onClick={() => actions.newList(null)}>＋ New list</button>
          {tags.length > 0 && <>
            <button className="side-head as-btn" onClick={() => setTagsOpen(o => !o)}><span>Tags</span><span className="muted">{tagsOpen ? '▾' : '▸'}</span></button>
            {tagsOpen && <div className="tag-cloud">{tags.map(t => <NavLink key={t.tag} to={`/tag/${encodeURIComponent(t.tag)}`} className={({ isActive }) => `tag ${isActive ? 'active' : ''}`} style={{ ['--tag' as string]: tint(t.tag) }}>#{t.tag}<small>{t.count}</small></NavLink>)}</div>}
          </>}
          {filters.length > 0 && <>
            <button className="side-head as-btn" onClick={() => setFiltersOpen(o => !o)}><span>Filters</span><span className="muted">{filtersOpen ? '▾' : '▸'}</span></button>
            {filtersOpen && <ul className="tree">{filters.map(f => <li key={f.id}><div className="tree-row"><NavLink to={`/filter/${f.id}`} className="tree-link" style={{ paddingLeft: 8 }}><span className="folder-ico">{f.icon || '⌕'}</span><span className="grow">{f.name}</span></NavLink></div></li>)}</ul>}
          </>}
        </div>
        <nav className="side-nav bottom">
          <NavLink to="/trash" className="nav-item"><span className="ico">🗑</span>Trash</NavLink>
          <NavLink to="/settings" className="nav-item"><span className="ico">⚙</span>Settings</NavLink>
        </nav>
        {sync && sync.mode !== 'off' && <button className={`sync-state ${syncDot}`} onClick={syncNow} title={sync.status.last_error || 'Sync with Nextcloud now'} disabled={syncing}>
          <span className="dot" />
          <span className="grow">{sync.status.last_error ? 'Nextcloud sync failed' : `Nextcloud · ${sync.mode === 'import' ? 'import' : 'two-way'}`}</span>
          <small className="muted">{syncing || sync.status.running ? 'syncing…' : sync.status.last_run ? ago(sync.status.last_run) : 'never'}</small>
        </button>}
        <div className="foot">
          <span className="row" title={user?.email}>{user && <Avatar user={user} />}<span className="foot-id"><span>{user?.name || user?.email}</span>{status?.version && <a className="ver" href="https://github.com/abjelosevic88/pocket-homelab-tasks/releases" target="_blank" rel="noreferrer">v{status.version.replace(/^v/, '')}</a>}</span></span>
          <span className="row">
            <button className="btn ghost sm" onClick={toggleTheme} title="Toggle theme">{theme === 'dark' ? '☀' : '☾'}</button>
            {status?.auth_mode === 'local' && <button className="btn ghost sm" onClick={logout} title="Sign out">⎋</button>}
          </span>
        </div>
      </aside>
      {open && <div className="scrim" onClick={() => setOpen(false)} />}
      <div className="main">
        <header className="topbar">
          <button className="btn ghost icon" onClick={() => setOpen(o => !o)} aria-label="Menu">☰</button>
          <span className="topbar-title">{title}</span>
          <span className="spacer" />
          <button className="btn ghost icon" onClick={() => setPalette(true)} aria-label="Search">⌕</button>
          <button className="btn ghost icon" onClick={() => setNewTask(true)} aria-label="New task">＋</button>
        </header>
        <div className="content-row">
          <main className="content"><ErrorBoundary key={section(loc.pathname)} where={loc.pathname}><Outlet /></ErrorBoundary></main>
          {openTask && isTaskPage && <ErrorBoundary key={'t' + openTask} where="detail"><TaskDetail id={openTask} onClose={closeDetail} /></ErrorBoundary>}
        </div>
        <nav className="tabbar">
          <NavLink to="/inbox"><span className="ico">▤</span>Inbox</NavLink>
          <NavLink to="/today"><span className="ico">☀</span>Today</NavLink>
          <NavLink to="/upcoming"><span className="ico">▦</span>Upcoming</NavLink>
          <button onClick={() => setOpen(true)}><span className="ico">☰</span>Lists</button>
          <button onClick={() => setPalette(true)}><span className="ico">⌕</span>Search</button>
        </nav>
      </div>
      {palette && <Palette onClose={() => setPalette(false)} onNew={() => { setPalette(false); setNewTask(true) }} />}
      {newTask && <Modal title="New task" onClose={() => setNewTask(false)}>
        <QuickAdd autoFocus defaults={{ list_id: loc.pathname.startsWith('/list/') ? Number(loc.pathname.split('/')[2]) || null : null, start: loc.pathname === '/today' ? today() : null, someday: loc.pathname === '/someday' || undefined }} onAdded={() => setNewTask(false)} onCancel={() => setNewTask(false)} />
        <p className="muted small mt-s">Plain words work: <code>tomorrow 9am</code>, <code>!high</code>, <code>#tag</code>, <code>/List</code>, <code>every week</code>. ⌘↵ for all fields.</p>
      </Modal>}
      {dialogs}
    </div>
  )
}

function AreaBranch({ area, lists, actions }: { area: Area; lists: List[]; actions: ReturnType<typeof useListDialogs>['actions'] }) {
  const key = 'pt-area:' + area.id
  const [open, setOpen] = useState(() => localStorage.getItem(key) !== '0')
  useEffect(() => { localStorage.setItem(key, open ? '1' : '0') }, [open, key])
  const total = lists.reduce((s, l) => s + (l.open_count || 0), 0)
  return (
    <li>
      <div className="tree-row area">
        <button className="twist" onClick={() => setOpen(o => !o)} aria-label={open ? 'Collapse' : 'Expand'}>{open ? '▾' : '▸'}</button>
        <button className="tree-link as-btn" onClick={() => setOpen(o => !o)}><span className="dot" style={{ background: area.color || 'var(--muted)' }} /><span className="grow"><b>{area.name}</b></span><small className="muted">{total || ''}</small></button>
        <Dropdown className="tree-more" align="right" label="⋯">{close => <>
          <button onClick={() => { close(); actions.newList(area.id) }}>New list here…</button>
          <button onClick={() => { close(); actions.renameArea(area) }}>Rename…</button>
          <button onClick={() => { close(); actions.colorArea(area) }}>Colour…</button>
          <button className="danger-text" onClick={() => { close(); actions.delArea(area) }}>Delete area…</button>
        </>}</Dropdown>
      </div>
      {open && <ul>{lists.map(l => <ListLeaf key={l.id} list={l} actions={actions} />)}{!lists.length && <li className="muted small" style={{ padding: '2px 10px' }}>No lists</li>}</ul>}
    </li>
  )
}

function ListLeaf({ list, actions }: { list: List; actions: ReturnType<typeof useListDialogs>['actions'] }) {
  const [menu, setMenu] = useState(false)
  return (
    <li>
      <div className="tree-row" data-drop-list={list.id} onContextMenu={e => { e.preventDefault(); setMenu(true) }}>
        <NavLink to={`/list/${list.id}`} className="tree-link" style={{ paddingLeft: 8 }}><span className="dot" style={{ background: list.color }} /><span className="grow">{list.name}</span><small className="muted">{list.open_count || ''}</small></NavLink>
        <Dropdown className="tree-more" align="right" label="⋯" open={menu || undefined} onOpenChange={o => { if (!o) setMenu(false) }}>{close => <ListMenuItems list={list} actions={actions} close={() => { close(); setMenu(false) }} />}</Dropdown>
      </div>
    </li>
  )
}
