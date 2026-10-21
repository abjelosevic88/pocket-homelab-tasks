import React, { useEffect, useMemo, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { api, upload, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { APIToken, Area, Feeds, List, NcSync, NcTest, Settings as SettingsT, SystemInfo, User } from '../lib/types'
import { Confirm, CopyField, Field, Loading, Modal, Segmented, SettingRow, Switch, Tabs, copyText, useAsync, useIsMobile, useToast } from '../components/ui'
import { Avatar } from '../components/people'
import { ColorPicker, ListSelect } from '../components/pickers'
import { Select } from '../components/Select'
import { Icon, ListRing } from '../components/icons'
import { ago, bytes } from '../lib/format'

type TabId = 'general' | 'lists' | 'calendar' | 'nextcloud' | 'notifications' | 'tokens' | 'data' | 'people' | 'profile' | 'about'

const TABS: { id: TabId; label: string; icon: string; sub: string }[] = [
  { id: 'general', label: 'General', icon: 'settings', sub: 'How your week, your lists and the logbook work — and the app title.' },
  { id: 'lists', label: 'Lists & areas', icon: 'folder', sub: 'Areas group lists; lists hold sections and tasks. The order here is the sidebar’s.' },
  { id: 'calendar', label: 'Calendar & apps', icon: 'calendar', sub: 'Tasks as events on a calendar, and the CalDAV addresses for Reminders, Thunderbird and friends.' },
  { id: 'nextcloud', label: 'Nextcloud sync', icon: 'sync', sub: 'Import Nextcloud Tasks once, or keep both sides the same.' },
  { id: 'notifications', label: 'Notifications', icon: 'bell', sub: 'Alarms fire on the server too, and a morning digest sums up the day.' },
  { id: 'tokens', label: 'Apps & API', icon: 'key', sub: 'Tokens for scripts and widgets — and the app password of every CalDAV client.' },
  { id: 'data', label: 'Import & export', icon: 'database', sub: 'Your tasks as JSON or iCalendar, and imports from other task apps.' },
  { id: 'profile', label: 'Profile', icon: 'user', sub: 'Your name, email, CalDAV user name and password.' },
  { id: 'people', label: 'People', icon: 'users', sub: 'Everyone who has an account on this box.' },
  { id: 'about', label: 'About', icon: 'info', sub: 'Version, numbers and the server.' },
]
// the old ?tab= names
const ALIAS: Record<string, TabId> = { notify: 'notifications', api: 'tokens', users: 'people' }

export default function Settings() {
  const { tab: raw } = useParams()
  const navigate = useNavigate()
  const mobile = useIsMobile()
  const legacy = new URLSearchParams(window.location.search).get('tab') || ''
  const want = raw || legacy
  const tab: TabId = TABS.find(t => t.id === (ALIAS[want] || want))?.id || 'general'
  const current = TABS.find(t => t.id === tab)!
  const go = (id: TabId) => navigate(`/settings/${id}`, { replace: true })

  // /settings?tab=api → /settings/tokens
  useEffect(() => { if (!raw && legacy) navigate(`/settings/${tab}`, { replace: true }) }, [raw, legacy, tab, navigate])
  useEffect(() => { document.querySelector('.set-content')?.scrollTo({ top: 0 }) }, [tab])
  useEffect(() => { document.querySelector('.set-tabs-mobile .tabs button.active')?.scrollIntoView({ inline: 'center', block: 'nearest' }) }, [tab, mobile])
  useEffect(() => {
    const h = (e: KeyboardEvent) => { if (e.key === 'Escape' && !document.querySelector('.modal-backdrop, .menu, .select-pop, .popover')) navigate('/') }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [navigate])

  return (
    <div className="set-page">
      <header className="set-header">
        <button type="button" className="btn ghost icon-only" aria-label="Back to your tasks" title="Back to your tasks (esc)" onClick={() => navigate('/')}><Icon name="chevron-left" size={18} /></button>
        <img src="/favicon.svg" alt="" width={24} height={24} className="set-logo" />
        <h1>Settings</h1>
      </header>
      {mobile && <div className="set-tabs-mobile"><Tabs tabs={TABS.map(t => ({ id: t.id, label: t.label, icon: t.icon }))} value={tab} onChange={go} /></div>}
      <div className="set-body">
        {!mobile && (
          <nav className="set-nav" aria-label="Settings sections">
            {TABS.map(t => (
              <button key={t.id} type="button" className={`set-nav-item ${t.id === tab ? 'active' : ''}`} aria-current={t.id === tab ? 'page' : undefined} onClick={() => go(t.id)}>
                <Icon name={t.icon} size={16} />{t.label}
              </button>
            ))}
          </nav>
        )}
        <main className="set-content">
          <div className="set-inner" key={tab}>
            <div className="set-title">
              <h2>{current.label}</h2>
              <p>{current.sub}</p>
            </div>
            {tab === 'general' && <General />}
            {tab === 'lists' && <ListsAreas />}
            {tab === 'calendar' && <CalendarApps />}
            {tab === 'nextcloud' && <Nextcloud />}
            {tab === 'notifications' && <Notify />}
            {tab === 'tokens' && <Tokens />}
            {tab === 'data' && <Data />}
            {tab === 'people' && <Users />}
            {tab === 'profile' && <Profile />}
            {tab === 'about' && <About />}
          </div>
        </main>
      </div>
    </div>
  )
}

/** A settings card: title, explanation, actions on the right — Pocket Calendar's section. */
function Section({ title, sub, actions, children }: { title: React.ReactNode; sub?: React.ReactNode; actions?: React.ReactNode; children?: React.ReactNode }) {
  return (
    <section className="set-section">
      <div className="set-section-head">
        <div className="grow"><h3>{title}</h3>{sub && <p>{sub}</p>}</div>
        {actions && <div className="row">{actions}</div>}
      </div>
      {children && <div className="set-section-body">{children}</div>}
    </section>
  )
}

function useSettingsForm() {
  const { settings, setSettings, user } = useApp()
  const { toast } = useToast()
  const [f, setF] = useState<SettingsT | null>(settings)
  const [busy, setBusy] = useState(false)
  useEffect(() => { if (!f && settings) setF(settings) }, [settings]) // eslint-disable-line react-hooks/exhaustive-deps
  const save = async (extra: Record<string, unknown> = {}) => {
    if (!f) return
    setBusy(true)
    try { const s = await api.put<SettingsT>(`${V1}/settings`, { ...f, ...extra }); setSettings(s); setF(s); toast('Saved') } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return { f, setF, save, busy, isAdmin: user?.role === 'admin' }
}

const ALARMS = [{ value: '', label: 'None' }, { value: 'PT0S', label: 'At the time' }, { value: '-PT15M', label: '15 minutes before' }, { value: '-PT1H', label: '1 hour before' }, { value: '-P1D', label: '1 day before' }]

function General() {
  const { f, setF, save, busy, isAdmin } = useSettingsForm()
  const zones = useMemo(() => { try { return (Intl as unknown as { supportedValuesOf?: (k: string) => string[] }).supportedValuesOf?.('timeZone') || [] } catch { return [] } }, [])
  if (!f) return <Loading />
  const zoneOpts = (f.timezone && !zones.includes(f.timezone) ? [f.timezone, ...zones] : zones).map(z => ({ value: z, label: z.replace(/_/g, ' '), keywords: z }))
  return (
    <div className="stack loose">
      <Section title="Planning" sub="Yours alone — everyone on the box has their own.">
        <SettingRow title="Week starts on"><Segmented size="sm" value={f.week_start} onChange={v => setF({ ...f, week_start: v })} options={[{ value: 1, label: 'Monday' }, { value: 0, label: 'Sunday' }]} /></SettingRow>
        <SettingRow title="New tasks go to" help="The list quick add uses when the text names none."><ListSelect value={f.default_list_id} onChange={id => setF({ ...f, default_list_id: id })} /></SettingRow>
        <SettingRow title="Default estimate" help="The length of a timed task on the calendar when it has none (minutes)."><input className="set-num" type="number" min={5} max={1440} value={f.default_estimate_min} onChange={e => setF({ ...f, default_estimate_min: Number(e.target.value) })} aria-label="Default estimate in minutes" /></SettingRow>
        <SettingRow title="Day starts at" help="When an all-day task’s reminder fires, and where it sits on the calendar mirror."><input className="set-time" type="time" value={f.day_start} onChange={e => setF({ ...f, day_start: e.target.value })} aria-label="Day starts at" /></SettingRow>
        <SettingRow title="Default alarm" help="For timed tasks made here."><Select value={f.default_alarm} options={ALARMS} onChange={v => setF({ ...f, default_alarm: v })} label="Default alarm" /></SettingRow>
      </Section>
      <Section title="Lists & logbook">
        <SettingRow title="Finished tasks in lists" help="Show a collapsed “Done” tail at the end of every list."><Switch checked={f.show_done_in_lists} onChange={v => setF({ ...f, show_done_in_lists: v })} label="Finished tasks in lists" /></SettingRow>
        <SettingRow title="Logbook grouped by"><Segmented size="sm" value={f.logbook_group} onChange={v => setF({ ...f, logbook_group: v })} options={[{ value: 'day', label: 'Day' }, { value: 'list', label: 'List' }]} /></SettingRow>
        <SettingRow title="Keep deleted tasks" help="Days in the trash before they go for good."><input className="set-num" type="number" min={0} max={3650} value={f.trash_days} onChange={e => setF({ ...f, trash_days: Number(e.target.value) })} aria-label="Days to keep deleted tasks" /></SettingRow>
      </Section>
      <Section title="Time zone" sub="Dates are kept as floating local time; this says what “today” and reminder times mean on the server.">
        <SettingRow title="Time zone">{zoneOpts.length ? <Select value={f.timezone} options={zoneOpts} onChange={v => setF({ ...f, timezone: v })} placeholder="Europe/Berlin" searchable label="Time zone" width={280} /> : <input value={f.timezone} onChange={e => setF({ ...f, timezone: e.target.value })} placeholder="Europe/Berlin" spellCheck={false} style={{ width: 220 }} />}</SettingRow>
      </Section>
      <Section title="This box" sub={isAdmin ? 'This applies to everyone on this box.' : 'Set by an admin for everyone.'}>
        <SettingRow title="App title" help="Shown in the sidebar and the browser tab."><input value={f.title} onChange={e => setF({ ...f, title: e.target.value })} maxLength={40} disabled={!isAdmin} style={{ width: 220 }} aria-label="App title" /></SettingRow>
      </Section>
      <div className="row"><button type="button" className="btn primary" onClick={() => save()} disabled={busy}>Save changes</button></div>
    </div>
  )
}

function ListsAreas() {
  const { lists, areas, bump } = useApp()
  const { toast } = useToast()
  const [edit, setEdit] = useState<{ kind: 'list'; item: List } | { kind: 'area'; item: Area } | null>(null)
  const [del, setDel] = useState<{ kind: 'list'; item: List } | { kind: 'area'; item: Area } | null>(null)
  const [moveTo, setMoveTo] = useState<number | null>(null)
  const [showArchived, setShowArchived] = useState(false)
  const run = async (fn: () => Promise<unknown>, msg?: string) => { try { await fn(); if (msg) toast(msg); bump() } catch (e) { toast((e as Error).message, 'err') } }
  const reorder = (kind: 'lists' | 'areas', ids: number[], i: number, dir: -1 | 1) => { const j = i + dir; if (j < 0 || j >= ids.length) return; const n = [...ids]; [n[i], n[j]] = [n[j], n[i]]; run(() => api.post(`${V1}/${kind}/reorder`, { ids: n })) }
  const sortedAreas = [...areas].sort((a, b) => a.sort_order - b.sort_order)
  const listsOf = (areaId: number | null) => lists.filter(l => (areaId === null ? !l.area_id || !areas.some(a => a.id === l.area_id) : l.area_id === areaId) && (showArchived || !l.archived)).sort((a, b) => a.sort_order - b.sort_order)
  const allIds = [...lists].sort((a, b) => a.sort_order - b.sort_order).map(l => l.id)
  const swap = (gi: number, other: number) => { const a = allIds.indexOf(other); const n = [...allIds]; [n[gi], n[a]] = [n[a], n[gi]]; run(() => api.post(`${V1}/lists/reorder`, { ids: n })) }
  const ListRows = ({ areaId }: { areaId: number | null }) => {
    const ls = listsOf(areaId)
    if (!ls.length) return <div className="manage-row empty muted small">No lists</div>
    return <>{ls.map((l, i) => {
      const gi = allIds.indexOf(l.id)
      return <div key={l.id} className={`manage-row ${l.archived ? 'muted' : ''}`}>
        <ListRing color={l.color} />
        <span className="grow manage-name"><span className="ellipsis">{l.name}</span>{l.is_inbox && <span className="badge">inbox</span>}{l.archived && <span className="badge">archived</span>}{l.open_count > 0 && <span className="faint small tnum">{l.open_count}</span>}</span>
        <span className="manage-tools">
          <button type="button" className="btn ghost icon-only sm" disabled={i === 0} onClick={() => swap(gi, ls[i - 1].id)} aria-label="Move up" title="Move up"><Icon name="chevron-up" size={15} /></button>
          <button type="button" className="btn ghost icon-only sm" disabled={i === ls.length - 1} onClick={() => swap(gi, ls[i + 1].id)} aria-label="Move down" title="Move down"><Icon name="chevron-down" size={15} /></button>
          <button type="button" className="btn ghost icon-only sm" onClick={() => setEdit({ kind: 'list', item: l })} aria-label={`Edit ${l.name}`} title="Edit"><Icon name="edit" size={15} /></button>
          {!l.is_inbox && <button type="button" className="btn ghost icon-only sm" onClick={() => run(() => api.put(`${V1}/lists/${l.id}`, { archived: !l.archived }), l.archived ? 'Restored' : 'Archived')} aria-label={l.archived ? 'Restore' : 'Archive'} title={l.archived ? 'Restore' : 'Archive'}><Icon name={l.archived ? 'undo' : 'archive'} size={15} /></button>}
          {!l.is_inbox && <button type="button" className="btn ghost icon-only sm danger-ghost" onClick={() => { setMoveTo(null); setDel({ kind: 'list', item: l }) }} aria-label={`Delete ${l.name}`} title="Delete"><Icon name="trash" size={15} /></button>}
        </span>
      </div>
    })}</>
  }
  const blankList: List = { id: 0, uid: '', name: '', color: '#2563eb', description: '', sort_order: 0, area_id: null, is_inbox: false, archived: false, open_count: 0, created_at: '' }
  return (
    <div className="stack loose">
      <Section title="Areas and lists" sub="Areas group lists (Work, Home…). Lists hold sections and tasks."
        actions={<label className="row small muted" style={{ gap: 8 }}><Switch size="sm" checked={showArchived} onChange={setShowArchived} label="Show archived" />Show archived</label>}>
        <div className="stack tight">
          {sortedAreas.map((a, i) => <div key={a.id} className="manage-area">
            <div className="manage-row head">
              <span className="manage-ico" style={{ color: a.color || 'var(--muted)' }}><Icon name="folder" size={15} /></span><b className="grow ellipsis">{a.name}</b>
              <span className="manage-tools">
                <button type="button" className="btn ghost icon-only sm" disabled={i === 0} onClick={() => reorder('areas', sortedAreas.map(x => x.id), i, -1)} aria-label="Move up" title="Move up"><Icon name="chevron-up" size={15} /></button>
                <button type="button" className="btn ghost icon-only sm" disabled={i === sortedAreas.length - 1} onClick={() => reorder('areas', sortedAreas.map(x => x.id), i, 1)} aria-label="Move down" title="Move down"><Icon name="chevron-down" size={15} /></button>
                <button type="button" className="btn ghost icon-only sm" onClick={() => setEdit({ kind: 'area', item: a })} aria-label={`Edit ${a.name}`} title="Edit"><Icon name="edit" size={15} /></button>
                <button type="button" className="btn ghost icon-only sm danger-ghost" onClick={() => setDel({ kind: 'area', item: a })} aria-label={`Delete ${a.name}`} title="Delete"><Icon name="trash" size={15} /></button>
              </span>
            </div>
            <ListRows areaId={a.id} />
          </div>)}
          <div className="manage-area"><div className="manage-row head"><span className="manage-ico faint"><Icon name="list" size={15} /></span><b className="grow muted">No area</b></div><ListRows areaId={null} /></div>
        </div>
        <div className="row mt"><button type="button" className="btn sm" onClick={() => setEdit({ kind: 'list', item: blankList })}><Icon name="plus" size={14} />New list</button><button type="button" className="btn sm" onClick={() => setEdit({ kind: 'area', item: { id: 0, name: '', color: '#2563eb', sort_order: 0 } })}><Icon name="folder" size={14} />New area</button></div>
      </Section>
      {edit && <EditModal e={edit} areas={areas} onClose={() => setEdit(null)} onSaved={() => { setEdit(null); bump() }} />}
      {del?.kind === 'area' && <Confirm title={`Delete the area “${del.item.name}”?`} message="Its lists keep existing without an area." onCancel={() => setDel(null)} onConfirm={() => { const d = del; setDel(null); run(() => api.del(`${V1}/areas/${d.item.id}`), 'Area deleted') }} />}
      {del?.kind === 'list' && <Modal title={`Delete “${del.item.name}”?`} onClose={() => setDel(null)} size="sm" footer={<>
        <button type="button" className="btn" onClick={() => setDel(null)}>Cancel</button>
        <button type="button" className="btn danger" onClick={() => { const d = del; setDel(null); run(() => api.del(`${V1}/lists/${d.item.id}${moveTo ? `?move_to=${moveTo}` : ''}`), 'List deleted') }}>Delete list</button>
      </>}>
        <Field group label="Where should its tasks go?">
          <Select<number> value={moveTo ?? 0} onChange={v => setMoveTo(v || null)} label="Where its tasks go" options={[{ value: 0, label: 'The trash' }, ...lists.filter(l => l.id !== del.item.id && !l.archived).map(l => ({ value: l.id, label: `Move to ${l.name}`, color: l.color || undefined }))]} />
        </Field>
      </Modal>}
    </div>
  )
}

function EditModal({ e, areas, onClose, onSaved }: { e: { kind: 'list'; item: List } | { kind: 'area'; item: Area }; areas: Area[]; onClose: () => void; onSaved: () => void }) {
  const { toast } = useToast()
  const [name, setName] = useState(e.item.name)
  const [color, setColor] = useState(e.item.color || '#2563eb')
  const [desc, setDesc] = useState(e.kind === 'list' ? e.item.description : '')
  const [areaId, setAreaId] = useState<number | null>(e.kind === 'list' ? e.item.area_id : null)
  const [busy, setBusy] = useState(false)
  const save = async (ev: React.FormEvent) => {
    ev.preventDefault(); setBusy(true)
    try {
      if (e.kind === 'list') { const body = { name, color, description: desc, area_id: areaId }; if (e.item.id) await api.put(`${V1}/lists/${e.item.id}`, body); else await api.post(`${V1}/lists`, body) }
      else { const body = { name, color }; if (e.item.id) await api.put(`${V1}/areas/${e.item.id}`, body); else await api.post(`${V1}/areas`, body) }
      toast('Saved'); onSaved()
    } catch (err) { toast((err as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <Modal title={e.item.id ? `Edit “${e.item.name}”` : e.kind === 'list' ? 'New list' : 'New area'} onClose={onClose}>
      <form onSubmit={save} className="stack">
        <Field label="Name"><input autoFocus value={name} onChange={ev => setName(ev.target.value)} required maxLength={80} /></Field>
        <Field group label="Colour"><ColorPicker value={color} onChange={setColor} /></Field>
        {e.kind === 'list' && <>
          <Field label="Description"><input value={desc} onChange={ev => setDesc(ev.target.value)} placeholder="What this list is for" /></Field>
          <Field group label="Area"><Select<number> value={areaId ?? 0} onChange={v => setAreaId(v || null)} label="Area" options={[{ value: 0, label: 'No area' }, ...areas.map(a => ({ value: a.id, label: a.name, color: a.color || undefined }))]} /></Field>
        </>}
        <div className="form-actions"><button type="button" className="btn" onClick={onClose}>Cancel</button><button type="submit" className="btn primary" disabled={busy}>{e.item.id ? 'Save' : 'Make'}</button></div>
      </form>
    </Modal>
  )
}

const APP_HOW: { name: string; how: string }[] = [
  { name: 'iPhone / iPad Reminders', how: 'Settings → Apps → Calendar → Accounts → Add Account → Other → Add CalDAV Account. Server: the CalDAV address, user name and an API token as the password. Reminders then shows every list; the Tasks calendar appears in Calendar.' },
  { name: 'macOS Reminders', how: 'System Settings → Internet Accounts → Add Other Account → CalDAV Account. Account type Manual, the CalDAV address, user name, an API token.' },
  { name: 'BusyCal', how: 'BusyCal → Settings → Accounts → + → CalDAV, the server address, user name, an API token. Tasks and the Tasks calendar both show.' },
  { name: 'Thunderbird', how: 'Calendar → New calendar → On the network → the CalDAV address. Lists come in as task calendars (the Tasks tab), the Tasks calendar as events.' },
  { name: 'Tasks.org (Android)', how: 'Settings → Synchronization → Add account → CalDAV. Address, user name, an API token.' },
  { name: 'Nextcloud Calendar', how: 'Calendar → + New calendar → New subscription from link, paste the webcal link. Read-only; the event-shaped mirror of your tasks.' },
  { name: 'Google Calendar', how: 'Other calendars → + → From URL, paste the ICS link (https). Google refreshes subscriptions every few hours.' },
]

function CalendarApps() {
  const { f, setF, save, busy } = useSettingsForm()
  const { toast } = useToast()
  const navigate = useNavigate()
  const { data, reload } = useAsync(() => api.get<Feeds>(`${V1}/feeds`), [])
  const [rotate, setRotate] = useState(false)
  if (!f) return <Loading />
  const apps = data?.apps?.length ? data.apps : APP_HOW
  return (
    <div className="stack loose">
      <Section title="Tasks on your calendar" sub="Every task with a date becomes an event on a “Tasks” calendar: through CalDAV (read–write — move the event and the task follows) or as a secret feed for apps that only subscribe.">
        <SettingRow title="Mirror tasks as events"><Switch checked={f.mirror_enabled} onChange={v => setF({ ...f, mirror_enabled: v })} label="Mirror tasks as events" /></SettingRow>
        <SettingRow title="Tasks with only a deadline" help="Shown on the deadline day."><Switch checked={f.mirror_include_due} onChange={v => setF({ ...f, mirror_include_due: v })} label="Tasks with only a deadline" /></SettingRow>
        <SettingRow title="Overdue open tasks move to today" help="They roll forward with a ⚠︎ mark instead of staying put."><Switch checked={f.mirror_rollover} onChange={v => setF({ ...f, mirror_rollover: v })} label="Overdue open tasks move to today" /></SettingRow>
        <SettingRow title="Finished tasks in the feed" help="On the day they were finished, with a ✓."><Switch checked={f.mirror_done} onChange={v => setF({ ...f, mirror_done: v })} label="Finished tasks in the feed" /></SettingRow>
        <SettingRow title="Finished tasks in the CalDAV calendar"><Switch checked={f.mirror_done_caldav} onChange={v => setF({ ...f, mirror_done_caldav: v })} label="Finished tasks in the CalDAV calendar" /></SettingRow>
        <div className="row mt-s"><button type="button" className="btn primary" onClick={() => save()} disabled={busy}>Save changes</button></div>
      </Section>
      <Section title="Connect your apps" sub={<>An API token is the app password for every CalDAV client — <button type="button" className="link-btn" onClick={() => navigate('/settings/tokens')}>make one under Apps & API</button>. Each list is a task calendar; “Tasks” is the event calendar.</>}>
        {!data ? <Loading /> : <div className="stack">
          <Field group label="CalDAV address"><CopyField value={data.caldav_url} label="CalDAV address" /></Field>
          <Field group label="Nextcloud-shaped address" help="For apps that assume Nextcloud’s path, like the Pocket Homelab app."><CopyField value={data.caldav_nextcloud_url} label="Nextcloud-shaped address" /></Field>
          <Field group label="User name" help="Change it under Profile."><CopyField value={data.username} label="User name" /></Field>
          <div className="set-howto">
            {apps.map(a => <details key={a.name}><summary><Icon name="smartphone" size={15} />{a.name}</summary><ol><li>{a.how}</li></ol></details>)}
          </div>
        </div>}
      </Section>
      <Section title="Secret feed" sub="Read-only, no login: anyone with the link sees your dated tasks. Rotate it if it leaks." actions={data && <button type="button" className="btn sm" onClick={() => setRotate(true)}><Icon name="refresh" size={14} />Rotate</button>}>
        {!data ? <Loading /> : <div className="stack">
          <Field group label="webcal link"><CopyField value={data.webcal_url} label="webcal link" secret /></Field>
          <Field group label="ICS link (https)" help="Add ?done=0 to leave out finished tasks, &days=N to limit how far back."><CopyField value={data.ics_url} label="ICS link" secret /></Field>
        </div>}
      </Section>
      {rotate && <Confirm title="Rotate the feed link?" confirmLabel="Rotate" message="Every calendar subscribed to the old link stops updating until it is given the new one." onCancel={() => setRotate(false)} onConfirm={async () => { setRotate(false); try { await api.post(`${V1}/feeds/rotate`); toast('New link made'); reload() } catch (e) { toast((e as Error).message, 'err') } }} />}
    </div>
  )
}

function Nextcloud() {
  const { data, reload } = useAsync(() => api.get<NcSync>(`${V1}/sync/nextcloud`), [])
  const { setSync, bump, lists } = useApp()
  const { toast } = useToast()
  const [f, setF] = useState({ mode: 'off' as NcSync['mode'], via: 'direct' as NcSync['via'], url: '', user: '', password: '', interval_min: 2 })
  const [test, setTest] = useState<NcTest | null>(null)
  const [busy, setBusy] = useState(false)
  useEffect(() => { if (data) setF({ mode: data.mode, via: data.core_available ? data.via : 'direct', url: data.url, user: data.user, password: '', interval_min: data.interval_min || 2 }) }, [data])
  if (!data) return <Loading />
  const st = data.status
  const save = async () => {
    setBusy(true)
    try { const r = await api.put<NcSync>(`${V1}/sync/nextcloud`, { ...f, password: f.password || undefined }); setSync(r); toast('Saved'); reload() } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  const runTest = async () => {
    setBusy(true); setTest(null)
    try { await api.put(`${V1}/sync/nextcloud`, { ...f, password: f.password || undefined }); setTest(await api.post<NcTest>(`${V1}/sync/nextcloud/test`)); reload() } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  const runNow = async () => {
    setBusy(true)
    try { const s = await api.post<NcSync['status']>(`${V1}/sync/nextcloud/run`); toast(s.last_error ? `Failed: ${s.last_error}` : `Synced: ${s.pulled} pulled, ${s.pushed} pushed`, s.last_error ? 'err' : 'ok'); bump(); reload() } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  const state = st.last_error ? 'error' : st.last_run ? 'ok' : ''
  return (
    <div className="stack loose">
      <Section title="Nextcloud Tasks" sub={<>Lists ↔ calendars, tasks ↔ VTODOs by UID. <b>Import once</b> copies what is there and stops. <b>Two-way</b> keeps both sides the same every {data.interval_min || 2} minutes, so phones can keep talking to Nextcloud until you point them here.</>}
        actions={data.mode !== 'off' && <button type="button" className="btn sm" onClick={runNow} disabled={busy || st.running}><Icon name="sync" size={14} className={st.running ? 'spin' : ''} />{st.running ? 'Running…' : 'Run now'}</button>}>
        <SettingRow title="Mode"><Segmented size="sm" value={f.mode} onChange={v => setF({ ...f, mode: v })} options={[{ value: 'off', label: 'Off' }, { value: 'import', label: 'Import once' }, { value: 'two_way', label: 'Two-way' }]} /></SettingRow>
        {f.mode !== 'off' && <>
          <SettingRow title="Account" help={f.via === 'core' ? <>Pocket Core’s Nextcloud service connects you with your own account there. Nothing to type.{data.core_error && <span className="danger-text"> {data.core_error}</span>}</> : undefined}>
            <Segmented size="sm" value={f.via} onChange={v => setF({ ...f, via: v })} options={[...(data.core_available ? [{ value: 'core' as const, label: 'Pocket Core' }] : []), { value: 'direct' as const, label: 'Direct' }]} />
          </SettingRow>
          {f.via === 'direct' && <SettingRow title="Nextcloud account" stack>
            <div className="stack tight">
              <Field label="Nextcloud address"><input value={f.url} onChange={e => setF({ ...f, url: e.target.value })} placeholder="https://cloud.example.com" spellCheck={false} /></Field>
              <div className="two-col">
                <Field label="Username"><input value={f.user} onChange={e => setF({ ...f, user: e.target.value })} autoComplete="off" /></Field>
                <Field label="App password" help="Nextcloud → Settings → Security → Devices & sessions."><input type="password" value={f.password} onChange={e => setF({ ...f, password: e.target.value })} placeholder={data.password_set ? 'saved — type to replace' : ''} autoComplete="new-password" /></Field>
              </div>
            </div>
          </SettingRow>}
          {f.mode === 'two_way' && <SettingRow title="Every" help="Minutes between runs."><input className="set-num" type="number" min={1} max={1440} value={f.interval_min} onChange={e => setF({ ...f, interval_min: Number(e.target.value) })} aria-label="Minutes between runs" /></SettingRow>}
        </>}
        {test && <div className={`callout ${test.ok ? 'success' : 'danger'} mt-s`}><Icon name={test.ok ? 'check-circle' : 'alert'} size={15} /><div>{test.ok ? <>It answers. Calendars found:<ul className="tight-list">{test.calendars.map(c => <li key={c.href}>{c.name} <span className="muted">· {c.tasks} tasks</span></li>)}</ul></> : 'It does not answer.'}</div></div>}
        <div className="row mt"><button type="button" className="btn primary" onClick={save} disabled={busy}>Save changes</button>{f.mode !== 'off' && <button type="button" className="btn" onClick={runTest} disabled={busy}>Test</button>}</div>
      </Section>
      {data.mode !== 'off' && <Section title={<span className="row"><span className={`set-status ${state}`} />Status</span>}>
        <dl className="set-dl">
          <div><dt>Last run</dt><dd>{st.last_run ? ago(st.last_run) : 'never'}{st.running && <span className="badge accent" style={{ marginLeft: 6 }}>running</span>}</dd></div>
          <div><dt>Last error</dt><dd className={st.last_error ? 'danger-text' : 'muted'}>{st.last_error || 'none'}</dd></div>
          <div><dt>Moved</dt><dd>{st.pulled} pulled · {st.pushed} pushed</dd></div>
          <div><dt>Calendars</dt><dd>{st.lists?.length ? <ul className="tight-list">{st.lists.map(l => <li key={l.href}>{l.name} <span className="muted">· {l.tasks} tasks → {l.mapped_list_id ? lists.find(x => x.id === l.mapped_list_id)?.name || `list ${l.mapped_list_id}` : 'not mapped yet'}</span></li>)}</ul> : <span className="muted">none yet</span>}</dd></div>
        </dl>
      </Section>}
    </div>
  )
}

function Notify() {
  const { f, setF, save, busy } = useSettingsForm()
  const { toast } = useToast()
  const [token, setToken] = useState('')
  const [testing, setTesting] = useState(false)
  if (!f) return <Loading />
  const hints: Record<string, string> = { apprise: 'Apprise API route, e.g. http://apprise:8000/notify/tasks', ntfy: 'ntfy topic URL, e.g. https://ntfy.example.com/tasks', webhook: 'Any URL that accepts a JSON POST {title, body, url}' }
  const test = async () => {
    setTesting(true)
    try { await save(token ? { notify_token: token } : {}); await api.post(`${V1}/notify/test`); toast('Sent — check your phone') } catch (e) { toast((e as Error).message, 'err') } finally { setTesting(false) }
  }
  return (
    <div className="stack loose">
      <Section title="Reminders from the server" sub={<>Alarms fire here, not only on the device.{f.notify_env && !f.notify_url && ' An address is set through NOTIFY_URL.'}</>}>
        <SettingRow title="Send through"><Segmented size="sm" value={f.notify_kind} onChange={v => setF({ ...f, notify_kind: v })} options={[{ value: 'apprise', label: 'Apprise' }, { value: 'ntfy', label: 'ntfy' }, { value: 'webhook', label: 'Webhook' }]} /></SettingRow>
        <SettingRow title="Address" help={hints[f.notify_kind]} stack><input value={f.notify_url} onChange={e => setF({ ...f, notify_url: e.target.value })} placeholder="http://…" spellCheck={false} aria-label="Address" /></SettingRow>
        <SettingRow title="Token" help="Optional; sent as a bearer token."><input type="password" value={token} onChange={e => setToken(e.target.value)} placeholder={f.notify_token_set ? 'saved — type to replace' : ''} autoComplete="off" style={{ width: 220 }} aria-label="Token" /></SettingRow>
        <SettingRow title="Push alarms" help="Each task’s alarms as a push."><Switch checked={f.notify_alarms} onChange={v => setF({ ...f, notify_alarms: v })} label="Push alarms" /></SettingRow>
        <SettingRow title="Morning digest" help="A summary of the day at this time. Empty: no digest."><input className="set-time" type="time" value={f.digest_time} onChange={e => setF({ ...f, digest_time: e.target.value })} aria-label="Morning digest at" /></SettingRow>
        <div className="row mt-s"><button type="button" className="btn primary" onClick={() => save(token ? { notify_token: token } : {}).then(() => setToken(''))} disabled={busy}>Save changes</button><button type="button" className="btn" onClick={test} disabled={testing || !(f.notify_url || f.notify_env)}><Icon name="bell" size={14} />{testing ? 'Sending…' : 'Send a test'}</button></div>
      </Section>
    </div>
  )
}

function Tokens() {
  const { toast } = useToast()
  const { user } = useApp()
  const { data, reload } = useAsync(() => api.get<APIToken[]>(`${V1}/tokens`), [])
  const [name, setName] = useState('')
  const [fresh, setFresh] = useState('')
  const [del, setDel] = useState<APIToken | null>(null)
  const create = async (e: React.FormEvent) => {
    e.preventDefault()
    try { const r = await api.post<{ token: string }>(`${V1}/tokens`, { name: name || 'token' }); setFresh(r.token); setName(''); reload() } catch (e) { toast((e as Error).message, 'err') }
  }
  return (
    <div className="stack loose">
      <Section title="API tokens and CalDAV app passwords" sub={<>A token acts as you: <code>Authorization: Bearer &lt;token&gt;</code> for scripts, Raycast, Homepage — and as the <b>password of any CalDAV app</b> (Reminders, Thunderbird, Tasks.org) with <code>{user?.username || user?.email}</code> as the user name. Make one per device so it can be revoked alone.</>}>
        <form onSubmit={create} className="row"><input value={name} onChange={e => setName(e.target.value)} placeholder="Token name (e.g. iPhone Reminders, Homepage)" aria-label="Token name" /><button type="submit" className="btn primary"><Icon name="plus" size={15} />Create</button></form>
        {fresh && <div className="set-fresh"><div className="row"><Icon name="check-circle" size={16} /><b>Copy it now — it is shown once.</b></div><CopyField value={fresh} label="New token" /></div>}
        <div className="set-people mt">
          {data && data.length > 0 ? data.map(t => (
            <div key={t.id} className="set-person">
              <span className="set-token-ico"><Icon name="key" size={15} /></span>
              <div className="grow"><div className="set-person-name">{t.name}<span className="mono faint small">{t.prefix}…</span></div><div className="muted small">Last used {t.last_used_at ? ago(t.last_used_at) : 'never'} · made {t.created_at.slice(0, 10)}</div></div>
              <button type="button" className="btn ghost sm danger-ghost" onClick={() => setDel(t)}>Revoke</button>
            </div>
          )) : data ? <p className="muted small">No tokens yet.</p> : <Loading />}
        </div>
        {del && <Confirm title={`Revoke “${del.name}”?`} confirmLabel="Revoke" message="Anything using this token stops working immediately." onCancel={() => setDel(null)} onConfirm={async () => { await api.del(`${V1}/tokens/${del.id}`); setDel(null); reload() }} />}
      </Section>
      <Section title="For scripts and other Pocket apps" sub={<><code>POST /api/v1/tasks/quick</code> adds from plain words, <code>GET /api/v1/tasks?view=today</code> lists, <code>GET /api/v1/dashboard</code> feeds a Homepage widget. Pocket Core apps ask for <code>tasks:read</code> / <code>tasks:write</code>. See docs/api.md.</>} />
    </div>
  )
}

function Data() {
  const { toast } = useToast()
  const { user, bump, lists, settings } = useApp()
  const [busy, setBusy] = useState(false)
  const [into, setInto] = useState<number | null>(null)
  const [icsList, setIcsList] = useState<number | null>(null)
  const live = lists.filter(l => !l.archived)
  const importFiles = async (files: File[]) => {
    setBusy(true)
    try { const r = await upload<{ tasks?: number; lists?: number; skipped?: number }>(`${V1}/import`, 'file', files, into ? { list_id: String(into) } : {}); toast(`Imported${r.tasks !== undefined ? `: ${r.tasks} tasks` : ''}${r.lists ? `, ${r.lists} lists` : ''}${r.skipped ? `, ${r.skipped} skipped` : ''}`); bump() } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <div className="stack loose">
      <Section title="Export" sub="Everything of yours — lists, sections, tasks — as JSON, or a list as an iCalendar file of VTODOs for any other task app.">
        <SettingRow title="Everything" help="Lists, sections and tasks as JSON."><a className="btn" href={`${V1}/export.json`} download><Icon name="download" size={15} />Download .json</a></SettingRow>
        <SettingRow title="As iCalendar" help="VTODOs of one list, or of all of them.">
          <Select<number> value={icsList ?? 0} onChange={v => setIcsList(v || null)} label="Which list" options={[{ value: 0, label: 'All lists' }, ...live.map(l => ({ value: l.id, label: l.name, color: l.color || undefined }))]} />
          <a className="btn" href={`${V1}/export.ics${icsList ? `?list_id=${icsList}` : ''}`} download><Icon name="download" size={15} />.ics</a>
        </SettingRow>
        {user?.role === 'admin' && <SettingRow title="The database" help="Every account, settings, the attachments index — admins only."><a className="btn" href={`${V1}/backup`}><Icon name="database" size={15} />Download database</a></SettingRow>}
      </Section>
      <Section title="Import" sub={<>An <code>.ics</code> file of VTODOs (from Nextcloud Tasks, Reminders, Thunderbird…) or a Pocket Tasks <code>.json</code> export. VTODOs go into the list below; with none chosen a list named after the calendar is made.</>}>
        <SettingRow title="Into the list"><Select<number> value={into ?? 0} onChange={v => setInto(v || null)} label="Into the list" options={[{ value: 0, label: 'A new list named after the file' }, ...live.map(l => ({ value: l.id, label: l.name + (l.id === settings?.default_list_id ? ' (default)' : ''), color: l.color || undefined }))]} width={280} /></SettingRow>
        <div className="row mt-s"><label className="btn primary"><Icon name="upload" size={15} />{busy ? 'Importing…' : 'Choose a file…'}<input type="file" accept=".ics,.json,text/calendar,application/json" hidden onChange={e => { const f = Array.from(e.target.files || []); e.target.value = ''; if (f.length) importFiles(f) }} /></label></div>
      </Section>
    </div>
  )
}

function Users() {
  const { user, status } = useApp()
  const { toast } = useToast()
  const { data: users, reload } = useAsync(() => api.get<User[]>(`${V1}/users`), [])
  const [edit, setEdit] = useState<Partial<User> | null>(null)
  const [del, setDel] = useState<User | null>(null)
  const isAdmin = user?.role === 'admin'
  const run = async (fn: () => Promise<unknown>, msg: string) => { try { await fn(); reload(); toast(msg) } catch (e) { toast((e as Error).message, 'err') } }
  return (
    <div className="stack loose">
      <Section title="Accounts" sub="Everyone signs in with their own account and has their own lists, tasks and settings — admins included; nobody sees anyone else’s tasks. Admins manage the accounts and the app title."
        actions={isAdmin && <button type="button" className="btn primary sm" onClick={() => setEdit({ role: 'member' })}><Icon name="plus" size={14} />Add person</button>}>
        {!users ? <Loading /> : <div className="set-people">
          {users.map(u => (
            <div key={u.id} className="set-person">
              <Avatar user={u} size={34} />
              <div className="grow"><div className="set-person-name">{u.name || <span className="muted">—</span>}{u.id === user?.id && <span className="badge">you</span>}<span className={`badge ${u.role === 'admin' ? 'accent' : ''}`}>{u.role}</span></div><div className="muted small ellipsis">{u.email} · CalDAV <span className="mono">{u.username}</span></div></div>
              {isAdmin && <span className="row" style={{ gap: 2 }}><button type="button" className="btn ghost sm" onClick={() => setEdit(u)}>Edit</button>{u.id !== user?.id && <button type="button" className="btn ghost sm danger-ghost" onClick={() => setDel(u)}>Remove</button>}</span>}
            </div>
          ))}
        </div>}
        {status?.auth_mode !== 'local' && <div className="callout info mt"><Icon name="info" size={15} /><span>Auth mode is <code>{status?.auth_mode}</code>: people are created automatically on their first visit.</span></div>}
      </Section>
      {edit && <UserModal user={edit} local={status?.auth_mode === 'local'} onClose={() => setEdit(null)} onSaved={() => { setEdit(null); reload() }} />}
      {del && <Confirm title={`Remove ${del.name || del.email}?`} confirmLabel="Remove" message="Their account, lists and tasks are deleted." onCancel={() => setDel(null)} onConfirm={() => { const d = del; setDel(null); run(() => api.del(`${V1}/users/${d.id}`), 'Removed') }} />}
    </div>
  )
}

function UserModal({ user, local, onClose, onSaved }: { user: Partial<User>; local: boolean; onClose: () => void; onSaved: () => void }) {
  const { toast } = useToast()
  const [f, setF] = useState({ name: user.name || '', email: user.email || '', username: user.username || '', password: '', role: user.role || 'member' })
  const [busy, setBusy] = useState(false)
  const save = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try { const body = { ...f, username: f.username || undefined, password: f.password || undefined }; if (user.id) await api.put(`${V1}/users/${user.id}`, body); else await api.post(`${V1}/users`, body); toast(user.id ? 'Saved' : 'Added'); onSaved() } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <Modal title={user.id ? 'Edit person' : 'Add a person'} onClose={onClose}>
      <form onSubmit={save} className="stack">
        <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} autoFocus required maxLength={60} /></Field>
        <Field label="Email" help="Used to sign in."><input type="email" value={f.email} onChange={e => setF({ ...f, email: e.target.value })} required /></Field>
        <Field label="CalDAV user name" help="Empty: the part of the email before @."><input value={f.username} onChange={e => setF({ ...f, username: e.target.value })} spellCheck={false} autoComplete="off" /></Field>
        {local && <Field label={user.id ? 'New password (leave empty to keep)' : 'Password'} help="At least 8 characters."><input type="password" value={f.password} onChange={e => setF({ ...f, password: e.target.value })} minLength={user.id ? 0 : 8} required={!user.id} autoComplete="new-password" /></Field>}
        <Field group label="Role"><Segmented value={f.role} onChange={v => setF({ ...f, role: v })} options={[{ value: 'member', label: 'Member' }, { value: 'admin', label: 'Admin' }]} /></Field>
        <div className="form-actions"><button type="button" className="btn" onClick={onClose}>Cancel</button><button type="submit" className="btn primary" disabled={busy}>{user.id ? 'Save' : 'Add'}</button></div>
      </form>
    </Modal>
  )
}

function Profile() {
  const { user, setUser, status } = useApp()
  const { toast } = useToast()
  const [f, setF] = useState({ name: user?.name || '', email: user?.email || '', username: user?.username || '', current_password: '', new_password: '' })
  const [busy, setBusy] = useState(false)
  const save = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try { setUser(await api.put(`${V1}/auth/me`, { ...f, current_password: f.current_password || undefined, new_password: f.new_password || undefined })); setF(x => ({ ...x, current_password: '', new_password: '' })); toast('Profile saved') } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <form onSubmit={save} className="stack loose">
      <Section title="You">
        <div className="set-profile">
          {user && <Avatar user={user} size={56} />}
          <div className="stack grow">
            <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} /></Field>
            <Field label="Email"><input type="email" value={f.email} onChange={e => setF({ ...f, email: e.target.value })} /></Field>
            <Field label="User name" help="The CalDAV user name your apps sign in with. Changing it means re-entering it in every app."><input value={f.username} onChange={e => setF({ ...f, username: e.target.value })} spellCheck={false} autoComplete="off" /></Field>
          </div>
        </div>
      </Section>
      {status?.auth_mode === 'local' && <Section title="Password" sub="At least 8 characters. Leave empty to keep the current one.">
        <div className="two-col">
          <Field label="Current password"><input type="password" value={f.current_password} onChange={e => setF({ ...f, current_password: e.target.value })} autoComplete="current-password" /></Field>
          <Field label="New password"><input type="password" value={f.new_password} onChange={e => setF({ ...f, new_password: e.target.value })} autoComplete="new-password" /></Field>
        </div>
      </Section>}
      <div className="row"><button type="submit" className="btn primary" disabled={busy}>Save changes</button></div>
    </form>
  )
}

function About() {
  const { data } = useAsync(() => api.get<SystemInfo>(`${V1}/settings/system`), [])
  const { toast } = useToast()
  const counts = data?.counts || {}
  return (
    <div className="stack loose">
      <Section title="Pocket Tasks" sub={data ? `Version ${data.version}` : undefined}>
        <p className="muted">A Things-style task manager with a real CalDAV server underneath: Inbox, Today, Upcoming, Anytime, Someday and Logbook; areas, lists, sections, subtasks; when and deadline apart; repeats fixed or after completion; alarms pushed from the server; tasks mirrored as events on any calendar; Nextcloud Tasks import and two-way sync; quick add in plain words.</p>
        <p className="muted small" style={{ margin: 0 }}>Open source (MIT) — <a href="https://github.com/abjelosevic88/pocket-homelab-tasks" target="_blank" rel="noreferrer">github.com/abjelosevic88/pocket-homelab-tasks</a>. One of the Pocket apps.</p>
      </Section>
      {data && Object.keys(counts).length > 0 && <div className="set-stats">
        {['open', 'done', 'lists', 'tags'].filter(k => k in counts).map(k => [k, counts[k]] as const).map(([k, v]) => <div key={k} className="set-stat"><span className="set-stat-n tnum">{v}</span><span className="set-stat-l">{k}</span></div>)}
      </div>}
      {data && <Section title="The server">
        <dl className="set-dl">
          <div><dt>Version</dt><dd className="mono">{data.version}</dd></div>
          <div><dt>Runtime</dt><dd className="mono">{data.go}</dd></div>
          <div><dt>Database</dt><dd className="mono">{data.data_dir} · {bytes(data.db_size)}</dd></div>
          <div><dt>Address</dt><dd className="mono">{data.base_url}</dd></div>
          <div><dt>CalDAV</dt><dd className="mono"><button type="button" className="link-btn mono" onClick={async () => toast(await copyText(data.caldav?.url || '') ? 'Copied' : 'Could not copy')} title="Copy">{data.caldav?.url}</button> · {data.caldav?.username}</dd></div>
          <div><dt>Pocket Core</dt><dd className="mono">{data.core?.configured ? (data.core.registered ? `registered at ${data.core.url}` : `not registered: ${data.core.error}`) : 'not configured'}</dd></div>
          {Object.keys(counts).length > 4 && <div><dt>Yours</dt><dd className="mono">{Object.entries(counts).map(([k, v]) => `${v} ${k}`).join(' · ')}</dd></div>}
        </dl>
      </Section>}
    </div>
  )
}
