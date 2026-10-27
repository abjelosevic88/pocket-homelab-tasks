// Left column: new task, the smart views, areas → lists, tags, filters, sync and settings — the Pocket Calendar sidebar.
import { useEffect, useMemo, useState } from 'react'
import { Link, NavLink } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Area, List, NcSync } from '../lib/types'
import { ago, tint } from '../lib/format'
import { Icon, ListRing, ViewIcon } from './icons'
import { listMenuItems, useListDialogs } from './ListMenu'
import { type Anchor, type MenuItem, Menu, rectOf, useToast } from './ui'

type Actions = ReturnType<typeof useListDialogs>['actions']

/** A sidebar section that remembers whether it is folded. */
function useFold(key: string) {
  const [open, setOpen] = useState(() => localStorage.getItem(key) !== '0')
  useEffect(() => { localStorage.setItem(key, open ? '1' : '0') }, [key, open])
  return [open, setOpen] as const
}

export function Sidebar({ onNewTask, onNavigate }: { onNewTask: () => void; onNavigate: () => void }) {
  const { lists, areas, tags, filters, dash, status, settings, sync, setSync, bump } = useApp()
  const { toast } = useToast()
  const { actions, dialogs } = useListDialogs()
  const [addMenu, setAddMenu] = useState<Anchor | null>(null)
  const [listsOpen, setListsOpen] = useFold('pt-lists')
  const [tagsOpen, setTagsOpen] = useFold('pt-tags')
  const [filtersOpen, setFiltersOpen] = useFold('pt-filters')
  const [syncing, setSyncing] = useState(false)
  const title = settings?.title || status?.title || 'Pocket Tasks'

  const live = useMemo(() => lists.filter(l => !l.archived), [lists])
  const loose = live.filter(l => !l.is_inbox && (!l.area_id || !areas.some(a => a.id === l.area_id)))
  const hasLists = live.some(l => !l.is_inbox)

  const syncNow = async () => {
    setSyncing(true)
    try { const s = await api.post<NcSync['status']>(`${V1}/sync/nextcloud/run`); setSync(sync ? { ...sync, status: s } : sync); bump(); toast(s.last_error ? `Sync failed: ${s.last_error}` : `Synced: ${s.pulled} in, ${s.pushed} out`, s.last_error ? 'err' : 'ok') } catch (e) { toast((e as Error).message, 'err') } finally { setSyncing(false) }
  }
  const syncState = sync?.status.last_error ? 'bad' : syncing || sync?.status.running ? 'busy' : 'ok'

  const nav: { to: string; view: string; label: string; count?: number; warn?: number }[] = [
    { to: '/inbox', view: 'inbox', label: 'Inbox', count: dash?.inbox },
    { to: '/today', view: 'today', label: 'Today', count: dash?.today, warn: dash?.overdue },
    { to: '/upcoming', view: 'upcoming', label: 'Upcoming', count: dash?.upcoming_week },
    { to: '/anytime', view: 'anytime', label: 'Anytime' },
    { to: '/someday', view: 'someday', label: 'Someday', count: dash?.someday },
    { to: '/logbook', view: 'logbook', label: 'Logbook' },
    { to: '/calendar', view: 'calendar', label: 'Calendar' },
  ]
  const addItems: MenuItem[] = [
    { label: 'New list…', icon: 'list', onSelect: () => actions.newList(null) },
    { label: 'New area…', icon: 'folder', onSelect: () => actions.newArea() },
  ]

  return (
    <aside className="sidebar" aria-label="Tasks">
      <div className="sb-brand">
        <img src="/favicon.svg" alt="" className="sb-logo" />
        <span className="sb-title">{title !== 'Pocket Tasks' ? title : <>Pocket <b>Tasks</b></>}</span>
        {status?.version && <span className="sb-version" title={`Version ${status.version}`}>{/^v?\d+\.\d+/.test(status.version) ? 'v' + status.version.replace(/^v/, '').replace(/[-+].*$/, '') : status.version}</span>}
      </div>
      <button type="button" className="btn primary create-btn" onClick={onNewTask} title="New task (⌘⇧N)">
        <Icon name="plus" size={17} strokeWidth={2.2} />New task<kbd className="kbd-on-primary">⌘⇧N</kbd>
      </button>

      <nav className="sb-nav" aria-label="Views">
        {nav.map(n => (
          <NavLink key={n.to} to={n.to} onClick={onNavigate} className={({ isActive }) => `sb-nav-item ${isActive ? 'active' : ''}`}>
            <ViewIcon view={n.view} size={16} />{n.label}
            {n.warn ? <span className="badge danger" title={`${n.warn} overdue`}>{n.count || n.warn}</span> : n.count ? <span className="sb-count">{n.count}</span> : null}
          </NavLink>
        ))}
      </nav>

      <section className="sb-sec">
        <h4 className="sb-h">
          <button type="button" className="sb-h-btn" aria-expanded={listsOpen} onClick={() => setListsOpen(o => !o)}><Icon name={listsOpen ? 'chevron-down' : 'chevron-right'} size={13} />Lists</button>
          <button type="button" className="btn ghost icon-only xs" aria-label="New list or area" title="New list or area" aria-haspopup="menu" aria-expanded={!!addMenu} onClick={e => setAddMenu(addMenu ? null : rectOf(e.currentTarget))}><Icon name="plus" size={14} /></button>
        </h4>
        {addMenu && <Menu anchor={addMenu} items={addItems} onClose={() => setAddMenu(null)} align="right" width={180} />}
        {listsOpen && <ul className="cal-list">
          {areas.map(a => <AreaGroup key={a.id} area={a} lists={live.filter(l => l.area_id === a.id && !l.is_inbox)} actions={actions} onNavigate={onNavigate} />)}
          {loose.map(l => <ListRow key={l.id} list={l} actions={actions} onNavigate={onNavigate} />)}
        </ul>}
        {listsOpen && !hasLists && <p className="sb-empty">No lists yet. <button type="button" className="link-btn" onClick={() => actions.newList(null)}>Make one</button></p>}
      </section>

      {tags.length > 0 && <section className="sb-sec">
        <h4 className="sb-h"><button type="button" className="sb-h-btn" aria-expanded={tagsOpen} onClick={() => setTagsOpen(o => !o)}><Icon name={tagsOpen ? 'chevron-down' : 'chevron-right'} size={13} />Tags</button></h4>
        {tagsOpen && <div className="tag-cloud">{tags.map(t => <NavLink key={t.tag} to={`/tag/${encodeURIComponent(t.tag)}`} onClick={onNavigate} className={({ isActive }) => `tag ${isActive ? 'active' : ''}`} style={{ ['--tag' as string]: tint(t.tag) }}>{t.tag}<small>{t.count}</small></NavLink>)}</div>}
      </section>}

      {filters.length > 0 && <section className="sb-sec">
        <h4 className="sb-h"><button type="button" className="sb-h-btn" aria-expanded={filtersOpen} onClick={() => setFiltersOpen(o => !o)}><Icon name={filtersOpen ? 'chevron-down' : 'chevron-right'} size={13} />Filters</button></h4>
        {filtersOpen && <ul className="cal-list">{filters.map(f => (
          <li key={f.id} className="cal-row sb-row">
            <NavLink to={`/filter/${f.id}`} onClick={onNavigate} className={({ isActive }) => `cal-check ${isActive ? 'is-active' : ''}`}><span className="sb-row-ico"><Icon name="filter" size={14} /></span><span className="cal-name ellipsis">{f.name}</span></NavLink>
          </li>
        ))}</ul>}
      </section>}

      <div className="sb-bottom">
        {sync && sync.mode !== 'off' && <button type="button" className={`sync-state ${syncState}`} onClick={syncNow} title={sync.status.last_error || 'Sync with Nextcloud now'} disabled={syncing}>
          <span className="sync-dot" />
          <span className="grow ellipsis">{sync.status.last_error ? 'Nextcloud sync failed' : `Nextcloud · ${sync.mode === 'import' ? 'import' : 'two-way'}`}</span>
          <small>{syncing || sync.status.running ? 'syncing…' : sync.status.last_run ? ago(sync.status.last_run) : 'never'}</small>
          <Icon name="sync" size={13} className={syncing || sync.status.running ? 'spin' : ''} />
        </button>}
        <div className="sb-foot">
          <Link to="/settings" className="sb-foot-link" onClick={onNavigate}><Icon name="settings" size={15} />Settings</Link>
          <button type="button" className="sb-foot-link" onClick={() => { onNavigate(); window.dispatchEvent(new CustomEvent('pt:shortcuts')) }}><Icon name="keyboard" size={15} />Shortcuts</button>
          <NavLink to="/trash" className={({ isActive }) => `sb-foot-link sb-trash ${isActive ? 'active' : ''}`} onClick={onNavigate} title="Trash" aria-label="Trash"><Icon name="trash" size={15} /></NavLink>
        </div>
      </div>
      {dialogs}
    </aside>
  )
}

function AreaGroup({ area, lists, actions, onNavigate }: { area: Area; lists: List[]; actions: Actions; onNavigate: () => void }) {
  const [open, setOpen] = useFold('pt-area:' + area.id)
  const [menu, setMenu] = useState<Anchor | null>(null)
  const total = lists.reduce((s, l) => s + (l.open_count || 0), 0)
  const items: MenuItem[] = [
    { label: 'New list here…', icon: 'plus', onSelect: () => actions.newList(area.id) },
    { label: 'Rename…', icon: 'edit', onSelect: () => actions.renameArea(area) },
    { label: 'Colour…', icon: 'palette', onSelect: () => actions.colorArea(area) },
    'sep',
    { label: 'Delete area…', icon: 'trash', danger: true, onSelect: () => actions.delArea(area) },
  ]
  return (
    <li className="sb-area">
      <div className="cal-row sb-row area" onContextMenu={e => { e.preventDefault(); setMenu({ x: e.clientX, y: e.clientY, w: 0, h: 0 }) }}>
        <button type="button" className="cal-check" aria-expanded={open} onClick={() => setOpen(o => !o)}>
          <span className="sb-row-ico" style={{ color: area.color || 'var(--muted)' }}><Icon name="folder" size={15} /></span>
          <span className="cal-name ellipsis">{area.name}</span>
          <Icon name={open ? 'chevron-down' : 'chevron-right'} size={12} className="sb-twist" />
          {!open && total > 0 && <span className="sb-count">{total}</span>}
        </button>
        <button type="button" className="btn ghost icon-only xs cal-more" aria-label={`Options for ${area.name}`} aria-haspopup="menu" aria-expanded={!!menu} onClick={e => setMenu(rectOf(e.currentTarget))}><Icon name="more" size={15} /></button>
        {menu && <Menu anchor={menu} items={items} onClose={() => setMenu(null)} width={200} />}
      </div>
      {open && <ul className="cal-list sb-nested">
        {lists.map(l => <ListRow key={l.id} list={l} actions={actions} onNavigate={onNavigate} />)}
        {!lists.length && <li className="sb-empty">No lists</li>}
      </ul>}
    </li>
  )
}

function ListRow({ list, actions, onNavigate }: { list: List; actions: Actions; onNavigate: () => void }) {
  const [menu, setMenu] = useState<Anchor | null>(null)
  return (
    <li className="cal-row sb-row" data-drop-list={list.id} onContextMenu={e => { e.preventDefault(); setMenu({ x: e.clientX, y: e.clientY, w: 0, h: 0 }) }}>
      <NavLink to={`/list/${list.id}`} onClick={onNavigate} className={({ isActive }) => `cal-check ${isActive ? 'is-active' : ''}`} title={list.description || list.name}>
        <span className="sb-row-ico"><ListRing color={list.color} /></span>
        <span className="cal-name ellipsis">{list.name}</span>
        {list.open_count > 0 && <span className="sb-count">{list.open_count}</span>}
      </NavLink>
      <button type="button" className="btn ghost icon-only xs cal-more" aria-label={`Options for ${list.name}`} aria-haspopup="menu" aria-expanded={!!menu} onClick={e => setMenu(rectOf(e.currentTarget))}><Icon name="more" size={15} /></button>
      {menu && <Menu anchor={menu} items={listMenuItems(list, actions)} onClose={() => setMenu(null)} width={210} />}
    </li>
  )
}
