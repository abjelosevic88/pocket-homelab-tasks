import { useEffect, useState } from 'react'
import { api, upload, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { APIToken, Area, Feeds, List, NcSync, NcTest, Settings as SettingsT, SystemInfo, User } from '../lib/types'
import { Card, Confirm, Field, Loading, Modal, PageHeader, Segmented, Tabs, useAsync, useToast } from '../components/ui'
import { Avatar } from '../components/people'
import { ColorPicker, ListSelect } from '../components/pickers'
import { ago, bytes } from '../lib/format'

type Tab = 'general' | 'lists' | 'calendar' | 'nextcloud' | 'notify' | 'api' | 'data' | 'users' | 'profile' | 'about'

export default function Settings() {
  const [tab, setTab] = useState<Tab>(() => (new URLSearchParams(window.location.search).get('tab') as Tab) || 'general')
  return (
    <div className="page">
      <PageHeader title="Settings" />
      <Tabs tabs={[{ id: 'general', label: 'General' }, { id: 'lists', label: 'Lists & areas' }, { id: 'calendar', label: 'Calendar & apps' }, { id: 'nextcloud', label: 'Nextcloud sync' }, { id: 'notify', label: 'Notifications' }, { id: 'api', label: 'Apps & API' }, { id: 'data', label: 'Import & export' }, { id: 'users', label: 'Users' }, { id: 'profile', label: 'Profile' }, { id: 'about', label: 'About' }]} value={tab} onChange={setTab} />
      <div className="mt">
        {tab === 'general' && <General />}
        {tab === 'lists' && <ListsAreas />}
        {tab === 'calendar' && <CalendarApps />}
        {tab === 'nextcloud' && <Nextcloud />}
        {tab === 'notify' && <Notify />}
        {tab === 'api' && <Tokens />}
        {tab === 'data' && <Data />}
        {tab === 'users' && <Users />}
        {tab === 'profile' && <Profile />}
        {tab === 'about' && <About />}
      </div>
    </div>
  )
}

function useSettingsForm() {
  const { settings, setSettings } = useApp()
  const { toast } = useToast()
  const [f, setF] = useState<SettingsT | null>(settings)
  const [busy, setBusy] = useState(false)
  useEffect(() => { if (!f && settings) setF(settings) }, [settings]) // eslint-disable-line react-hooks/exhaustive-deps
  const save = async (extra: Record<string, unknown> = {}) => {
    if (!f) return
    setBusy(true)
    try { const s = await api.put<SettingsT>(`${V1}/settings`, { ...f, ...extra }); setSettings(s); setF(s); toast('Saved') } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return { f, setF, save, busy }
}

const OnOff = ({ value, onChange, yes = 'On', no = 'Off' }: { value: boolean; onChange: (v: boolean) => void; yes?: string; no?: string }) => <Segmented value={value ? 'on' : 'off'} onChange={v => onChange(v === 'on')} options={[{ value: 'on', label: yes }, { value: 'off', label: no }]} />

function General() {
  const { f, setF, save, busy } = useSettingsForm()
  const { user } = useApp()
  if (!f) return <Loading />
  const zones = (() => { try { return (Intl as unknown as { supportedValuesOf?: (k: string) => string[] }).supportedValuesOf?.('timeZone') || [] } catch { return [] } })()
  return (
    <Card title="General">
      <div className="grid" style={{ gap: 14, maxWidth: 620 }}>
        {user?.role === 'admin' && <Field label="App title" help="Shown in the sidebar and browser tab, for everyone."><input value={f.title} onChange={e => setF({ ...f, title: e.target.value })} maxLength={40} /></Field>}
        <Field group label="Week starts on"><Segmented value={f.week_start} onChange={v => setF({ ...f, week_start: v })} options={[{ value: 1, label: 'Monday' }, { value: 0, label: 'Sunday' }]} /></Field>
        <Field label="New tasks go to" help="The list quick add uses when the text names none."><ListSelect value={f.default_list_id} onChange={id => setF({ ...f, default_list_id: id })} /></Field>
        <div className="two-col tight">
          <Field label="Default estimate (minutes)" help="The length of a timed task on the calendar when it has none."><input type="number" min={5} max={1440} value={f.default_estimate_min} onChange={e => setF({ ...f, default_estimate_min: Number(e.target.value) })} /></Field>
          <Field label="Day starts at" help="When an all-day task's reminder fires, and where it sits on the calendar mirror."><input type="time" value={f.day_start} onChange={e => setF({ ...f, day_start: e.target.value })} /></Field>
        </div>
        <Field group label="Default alarm for timed tasks made here"><Segmented value={f.default_alarm} onChange={v => setF({ ...f, default_alarm: v })} options={[{ value: '', label: 'None' }, { value: 'PT0S', label: 'At the time' }, { value: '-PT15M', label: '15 min before' }, { value: '-PT1H', label: '1 h before' }, { value: '-P1D', label: '1 day before' }]} /></Field>
        <Field group label="Finished tasks in lists" help="Show a collapsed “Done” tail at the end of every list."><OnOff value={f.show_done_in_lists} onChange={v => setF({ ...f, show_done_in_lists: v })} yes="Show" no="Hide" /></Field>
        <Field group label="Logbook grouped by"><Segmented value={f.logbook_group} onChange={v => setF({ ...f, logbook_group: v })} options={[{ value: 'day', label: 'Day' }, { value: 'list', label: 'List' }]} /></Field>
        <Field label="Timezone" help="Dates are kept as floating local time; this says what “today” and reminder times mean on the server."><input list="pt-zones" value={f.timezone} onChange={e => setF({ ...f, timezone: e.target.value })} placeholder="Europe/Berlin" spellCheck={false} /><datalist id="pt-zones">{zones.map(z => <option key={z} value={z} />)}</datalist></Field>
        <Field label="Keep deleted tasks (days)"><input type="number" min={0} max={3650} value={f.trash_days} onChange={e => setF({ ...f, trash_days: Number(e.target.value) })} style={{ maxWidth: 120 }} /></Field>
        <div><button className="btn primary" onClick={() => save()} disabled={busy}>Save</button></div>
      </div>
    </Card>
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
  const ListRows = ({ areaId }: { areaId: number | null }) => {
    const ls = listsOf(areaId)
    return <>{ls.map((l, i) => {
      const gi = allIds.indexOf(l.id)
      return <div key={l.id} className={`manage-row ${l.archived ? 'muted' : ''}`}>
        <span className="dot" style={{ background: l.color }} />
        <span className="grow">{l.name}{l.is_inbox && <span className="badge" style={{ marginLeft: 6 }}>inbox</span>}{l.archived && <span className="badge" style={{ marginLeft: 6 }}>archived</span>}<small className="muted"> {l.open_count || ''}</small></span>
        <span className="row" style={{ gap: 2 }}>
          <button className="btn ghost xs" disabled={i === 0} onClick={() => { const a = allIds.indexOf(ls[i - 1].id); const n = [...allIds]; [n[gi], n[a]] = [n[a], n[gi]]; run(() => api.post(`${V1}/lists/reorder`, { ids: n })) }} aria-label="Up">↑</button>
          <button className="btn ghost xs" disabled={i === ls.length - 1} onClick={() => { const a = allIds.indexOf(ls[i + 1].id); const n = [...allIds]; [n[gi], n[a]] = [n[a], n[gi]]; run(() => api.post(`${V1}/lists/reorder`, { ids: n })) }} aria-label="Down">↓</button>
          <button className="btn ghost xs" onClick={() => setEdit({ kind: 'list', item: l })}>Edit</button>
          {!l.is_inbox && <button className="btn ghost xs" onClick={() => run(() => api.put(`${V1}/lists/${l.id}`, { archived: !l.archived }), l.archived ? 'Restored' : 'Archived')}>{l.archived ? 'Restore' : 'Archive'}</button>}
          {!l.is_inbox && <button className="btn ghost xs danger-text" onClick={() => { setMoveTo(null); setDel({ kind: 'list', item: l }) }}>Delete</button>}
        </span>
      </div>
    })}</>
  }
  return (
    <div className="grid" style={{ gap: 16 }}>
      <Card title="Areas and lists" actions={<label className="row small muted" style={{ gap: 4 }}><input type="checkbox" style={{ width: 'auto' }} checked={showArchived} onChange={e => setShowArchived(e.target.checked)} /> show archived</label>}>
        <p className="muted">Areas group lists (Work, Home…). Lists hold sections and tasks. The order here is the sidebar's.</p>
        {sortedAreas.map((a, i) => <div key={a.id} className="manage-area">
          <div className="manage-row head">
            <span className="dot" style={{ background: a.color || 'var(--muted)' }} /><b className="grow">{a.name}</b>
            <span className="row" style={{ gap: 2 }}>
              <button className="btn ghost xs" disabled={i === 0} onClick={() => reorder('areas', sortedAreas.map(x => x.id), i, -1)} aria-label="Up">↑</button>
              <button className="btn ghost xs" disabled={i === sortedAreas.length - 1} onClick={() => reorder('areas', sortedAreas.map(x => x.id), i, 1)} aria-label="Down">↓</button>
              <button className="btn ghost xs" onClick={() => setEdit({ kind: 'area', item: a })}>Edit</button>
              <button className="btn ghost xs danger-text" onClick={() => setDel({ kind: 'area', item: a })}>Delete</button>
            </span>
          </div>
          <ListRows areaId={a.id} />
        </div>)}
        <div className="manage-area"><div className="manage-row head"><span className="dot" style={{ background: 'transparent', border: '1px dashed var(--border)' }} /><b className="grow muted">No area</b></div><ListRows areaId={null} /></div>
        <div className="row mt"><button className="btn sm" onClick={() => setEdit({ kind: 'list', item: { id: 0, uid: '', name: '', color: '#2563eb', description: '', sort_order: 0, area_id: null, is_inbox: false, archived: false, open_count: 0, created_at: '' } })}>＋ New list</button><button className="btn sm" onClick={() => setEdit({ kind: 'area', item: { id: 0, name: '', color: '#2563eb', sort_order: 0 } })}>＋ New area</button></div>
      </Card>
      {edit && <EditModal e={edit} areas={areas} onClose={() => setEdit(null)} onSaved={() => { setEdit(null); bump() }} />}
      {del?.kind === 'area' && <Confirm title={`Delete the area “${del.item.name}”?`} message="Its lists keep existing without an area." onCancel={() => setDel(null)} onConfirm={() => { const d = del; setDel(null); run(() => api.del(`${V1}/areas/${d.item.id}`), 'Area deleted') }} />}
      {del?.kind === 'list' && <Modal title={`Delete “${del.item.name}”?`} onClose={() => setDel(null)}>
        <p>Where should its tasks go?</p>
        <select value={moveTo ?? ''} onChange={e => setMoveTo(e.target.value ? Number(e.target.value) : null)}><option value="">The trash</option>{lists.filter(l => l.id !== del.item.id && !l.archived).map(l => <option key={l.id} value={l.id}>Move to {l.name}</option>)}</select>
        <div className="form-actions"><button className="btn" onClick={() => setDel(null)}>Cancel</button><button className="btn danger" onClick={() => { const d = del; setDel(null); run(() => api.del(`${V1}/lists/${d.item.id}${moveTo ? `?move_to=${moveTo}` : ''}`), 'List deleted') }}>Delete list</button></div>
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
      <form onSubmit={save} className="grid" style={{ gap: 14 }}>
        <Field label="Name"><input autoFocus value={name} onChange={ev => setName(ev.target.value)} required maxLength={80} /></Field>
        <Field group label="Colour"><ColorPicker value={color} onChange={setColor} /></Field>
        {e.kind === 'list' && <>
          <Field label="Description"><input value={desc} onChange={ev => setDesc(ev.target.value)} placeholder="What this list is for" /></Field>
          <Field label="Area"><select value={areaId ?? ''} onChange={ev => setAreaId(ev.target.value ? Number(ev.target.value) : null)}><option value="">No area</option>{areas.map(a => <option key={a.id} value={a.id}>{a.name}</option>)}</select></Field>
        </>}
        <div className="form-actions"><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy}>{e.item.id ? 'Save' : 'Make'}</button></div>
      </form>
    </Modal>
  )
}

function CopyRow({ label, value, help }: { label: string; value: string; help?: string }) {
  const { toast } = useToast()
  return <Field label={label} help={help}><div className="row"><input readOnly value={value} className="mono" onFocus={e => e.target.select()} /><button type="button" className="btn sm" onClick={() => navigator.clipboard?.writeText(value).then(() => toast('Copied')).catch(() => {})}>Copy</button></div></Field>
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
  const { data, reload } = useAsync(() => api.get<Feeds>(`${V1}/feeds`), [])
  const [rotate, setRotate] = useState(false)
  if (!f) return <Loading />
  const apps = data?.apps?.length ? data.apps : APP_HOW
  return (
    <div className="grid" style={{ gap: 16 }}>
      <Card title="Tasks on your calendar">
        <div className="grid" style={{ gap: 14, maxWidth: 620 }}>
          <p className="muted">Every task with a date becomes an event on a “Tasks” calendar: through CalDAV (read–write: move the event and the task follows) or as a secret feed for apps that only subscribe.</p>
          <Field group label="Mirror tasks as events"><OnOff value={f.mirror_enabled} onChange={v => setF({ ...f, mirror_enabled: v })} /></Field>
          <Field group label="Tasks with only a deadline" help="Shown on the deadline day."><OnOff value={f.mirror_include_due} onChange={v => setF({ ...f, mirror_include_due: v })} yes="Include" no="Leave out" /></Field>
          <Field group label="Overdue open tasks" help="Roll forward to today with a ⚠︎ mark."><OnOff value={f.mirror_rollover} onChange={v => setF({ ...f, mirror_rollover: v })} yes="Show today" no="Stay put" /></Field>
          <Field group label="Finished tasks in the feed" help="On the day they were finished, with a ✓."><OnOff value={f.mirror_done} onChange={v => setF({ ...f, mirror_done: v })} yes="Show" no="Hide" /></Field>
          <Field group label="Finished tasks in the CalDAV calendar"><OnOff value={f.mirror_done_caldav} onChange={v => setF({ ...f, mirror_done_caldav: v })} yes="Show" no="Hide" /></Field>
          <div><button className="btn primary" onClick={() => save()} disabled={busy}>Save</button></div>
        </div>
      </Card>
      <Card title="Connect your apps">
        {!data ? <Loading /> : <div className="grid" style={{ gap: 14 }}>
          <p className="muted">An API token (Apps & API tab) is the app password for every CalDAV client. Each list is a task calendar; “Tasks” is the event calendar.</p>
          <div className="two-col tight">
            <CopyRow label="CalDAV address" value={data.caldav_url} />
            <CopyRow label="Nextcloud-shaped address" value={data.caldav_nextcloud_url} help="For apps that assume Nextcloud's path, like the Pocket Homelab app." />
            <CopyRow label="User name" value={data.username} help="Change it under Profile." />
          </div>
          <details className="apps-how"><summary>How to add it, per app</summary>
            <dl>{apps.map(a => <div key={a.name}><dt>{a.name}</dt><dd className="muted">{a.how}</dd></div>)}</dl>
          </details>
          <hr />
          <p className="muted"><b>Secret feed.</b> Read-only, no login: anyone with the link sees your dated tasks. Rotate it if it leaks.</p>
          <div className="two-col tight">
            <CopyRow label="webcal link" value={data.webcal_url} />
            <CopyRow label="ICS link (https)" value={data.ics_url} help="Add ?done=0 to leave out finished tasks, &days=N to limit how far back." />
          </div>
          <div><button className="btn sm" onClick={() => setRotate(true)}>Rotate the feed link</button></div>
        </div>}
      </Card>
      {rotate && <Confirm title="Rotate the feed link?" message="Every calendar subscribed to the old link stops updating until it is given the new one." onCancel={() => setRotate(false)} onConfirm={async () => { setRotate(false); try { await api.post(`${V1}/feeds/rotate`); toast('New link made'); reload() } catch (e) { toast((e as Error).message, 'err') } }} />}
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
  return (
    <div className="grid" style={{ gap: 16 }}>
      <Card title="Nextcloud Tasks" actions={data.mode !== 'off' && <button className="btn sm" onClick={runNow} disabled={busy || st.running}>{st.running ? 'Running…' : 'Run now'}</button>}>
        <div className="grid" style={{ gap: 14, maxWidth: 640 }}>
          <p className="muted">Lists ↔ calendars, tasks ↔ VTODOs by UID. <b>Import once</b> copies what is there and stops. <b>Two-way</b> keeps both sides the same every {data.interval_min || 2} minutes, so phones can keep talking to Nextcloud until you point them here.</p>
          <Field group label="Mode"><Segmented value={f.mode} onChange={v => setF({ ...f, mode: v })} options={[{ value: 'off', label: 'Off' }, { value: 'import', label: 'Import once' }, { value: 'two_way', label: 'Two-way' }]} /></Field>
          {f.mode !== 'off' && <>
            <Field group label="Account"><Segmented value={f.via} onChange={v => setF({ ...f, via: v })} options={[...(data.core_available ? [{ value: 'core' as const, label: 'Pocket Core' }] : []), { value: 'direct' as const, label: 'Direct' }]} /></Field>
            {f.via === 'core' ? <p className="muted">Pocket Core's Nextcloud service connects you with your own account there. Nothing to type.{data.core_error && <span className="bad"> {data.core_error}</span>}</p> : <>
              <Field label="Nextcloud address"><input value={f.url} onChange={e => setF({ ...f, url: e.target.value })} placeholder="https://cloud.example.com" spellCheck={false} /></Field>
              <div className="two-col tight">
                <Field label="Username"><input value={f.user} onChange={e => setF({ ...f, user: e.target.value })} autoComplete="off" /></Field>
                <Field label="App password" help="Nextcloud → Settings → Security → Devices & sessions."><input type="password" value={f.password} onChange={e => setF({ ...f, password: e.target.value })} placeholder={data.password_set ? 'saved — type to replace' : ''} autoComplete="new-password" /></Field>
              </div>
            </>}
            {f.mode === 'two_way' && <Field label="Every (minutes)"><input type="number" min={1} max={1440} value={f.interval_min} onChange={e => setF({ ...f, interval_min: Number(e.target.value) })} style={{ maxWidth: 120 }} /></Field>}
          </>}
          {test && <div className={`callout ${test.ok ? 'success' : 'danger'}`}>{test.ok ? <>It answers. Calendars found:<ul style={{ margin: '6px 0 0 18px' }}>{test.calendars.map(c => <li key={c.href}>{c.name} <span className="muted">· {c.tasks} tasks</span></li>)}</ul></> : 'It does not answer.'}</div>}
          <div className="row"><button className="btn primary" onClick={save} disabled={busy}>Save</button>{f.mode !== 'off' && <button className="btn" onClick={runTest} disabled={busy}>Test</button>}</div>
        </div>
      </Card>
      {data.mode !== 'off' && <Card title="Status">
        <table className="table compact">
          <tbody>
            <tr><th>Last run</th><td>{st.last_run ? ago(st.last_run) : 'never'}{st.running && <span className="badge" style={{ marginLeft: 6 }}>running</span>}</td></tr>
            <tr><th>Last error</th><td className={st.last_error ? 'bad' : 'muted'}>{st.last_error || 'none'}</td></tr>
            <tr><th>Moved</th><td>{st.pulled} pulled · {st.pushed} pushed</td></tr>
            <tr><th>Calendars</th><td>{st.lists?.length ? <ul style={{ margin: 0, paddingLeft: 18 }}>{st.lists.map(l => <li key={l.href}>{l.name} <span className="muted">· {l.tasks} tasks → {l.mapped_list_id ? lists.find(x => x.id === l.mapped_list_id)?.name || `list ${l.mapped_list_id}` : 'not mapped yet'}</span></li>)}</ul> : <span className="muted">none yet</span>}</td></tr>
          </tbody>
        </table>
      </Card>}
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
    <Card title="Reminders from the server">
      <div className="grid" style={{ gap: 14, maxWidth: 600 }}>
        <p className="muted">Alarms fire here, not only on the device, and a morning digest sums up the day.{f.notify_env && !f.notify_url && ' An address is set through NOTIFY_URL.'}</p>
        <Field group label="Send through"><Segmented value={f.notify_kind} onChange={v => setF({ ...f, notify_kind: v })} options={[{ value: 'apprise', label: 'Apprise' }, { value: 'ntfy', label: 'ntfy' }, { value: 'webhook', label: 'Webhook' }]} /></Field>
        <Field label="Address" help={hints[f.notify_kind]}><input value={f.notify_url} onChange={e => setF({ ...f, notify_url: e.target.value })} placeholder="http://…" spellCheck={false} /></Field>
        <Field label="Token (optional)" help="Sent as a bearer token."><input type="password" value={token} onChange={e => setToken(e.target.value)} placeholder={f.notify_token_set ? 'saved — type to replace' : ''} autoComplete="off" /></Field>
        <Field group label="Push alarms"><OnOff value={f.notify_alarms} onChange={v => setF({ ...f, notify_alarms: v })} /></Field>
        <Field label="Morning digest at" help="Empty: no digest."><input type="time" value={f.digest_time} onChange={e => setF({ ...f, digest_time: e.target.value })} style={{ maxWidth: 140 }} /></Field>
        <div className="row"><button className="btn primary" onClick={() => save(token ? { notify_token: token } : {}).then(() => setToken(''))} disabled={busy}>Save</button><button className="btn" onClick={test} disabled={testing || !(f.notify_url || f.notify_env)}>{testing ? 'Sending…' : 'Send a test'}</button></div>
      </div>
    </Card>
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
    <div className="grid" style={{ gap: 16 }}>
      <Card title="API tokens and CalDAV app passwords">
        <p className="muted">A token acts as you: <code>Authorization: Bearer &lt;token&gt;</code> for scripts, Raycast, Homepage — and as the <b>password of any CalDAV app</b> (Reminders, Thunderbird, Tasks.org) with <code>{user?.username || user?.email}</code> as the user name. Make one per device so it can be revoked alone.</p>
        <form onSubmit={create} className="row mb"><input value={name} onChange={e => setName(e.target.value)} placeholder="Token name (e.g. iPhone Reminders, Homepage)" /><button className="btn primary">Create</button></form>
        {fresh && <div className="callout success mb"><b>Copy it now — it is shown once.</b><div className="row mt-s"><pre className="mono grow" style={{ margin: 0 }}>{fresh}</pre><button className="btn sm" onClick={() => navigator.clipboard?.writeText(fresh).then(() => toast('Copied')).catch(() => {})}>Copy</button></div></div>}
        {data && data.length > 0 ? (
          <table className="table compact">
            <thead><tr><th>Name</th><th>Prefix</th><th>Last used</th><th>Created</th><th /></tr></thead>
            <tbody>{data.map(t => <tr key={t.id}><td>{t.name}</td><td className="mono">{t.prefix}…</td><td className="muted">{t.last_used_at ? ago(t.last_used_at) : 'never'}</td><td className="muted">{t.created_at.slice(0, 10)}</td><td className="right"><button className="btn ghost sm danger-text" onClick={() => setDel(t)}>Revoke</button></td></tr>)}</tbody>
          </table>
        ) : <p className="muted">No tokens yet.</p>}
        {del && <Confirm title={`Revoke “${del.name}”?`} message="Anything using this token stops working immediately." onCancel={() => setDel(null)} onConfirm={async () => { await api.del(`${V1}/tokens/${del.id}`); setDel(null); reload() }} />}
      </Card>
      <Card title="For scripts and other Pocket apps">
        <p className="muted small"><code>POST /api/v1/tasks/quick</code> adds from plain words, <code>GET /api/v1/tasks?view=today</code> lists, <code>GET /api/v1/dashboard</code> feeds a Homepage widget. Pocket Core apps ask for <code>tasks:read</code> / <code>tasks:write</code>. See docs/api.md.</p>
      </Card>
    </div>
  )
}

function Data() {
  const { toast } = useToast()
  const { user, bump, lists, settings } = useApp()
  const [busy, setBusy] = useState(false)
  const [into, setInto] = useState<number | null>(null)
  const [icsList, setIcsList] = useState<number | null>(null)
  const importFiles = async (files: File[]) => {
    setBusy(true)
    try { const r = await upload<{ tasks?: number; lists?: number; skipped?: number }>(`${V1}/import`, 'file', files, into ? { list_id: String(into) } : {}); toast(`Imported${r.tasks !== undefined ? `: ${r.tasks} tasks` : ''}${r.lists ? `, ${r.lists} lists` : ''}${r.skipped ? `, ${r.skipped} skipped` : ''}`); bump() } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <div className="two-col">
      <Card title="Export">
        <p className="muted">Everything of yours — lists, sections, tasks — as JSON, or a list as an iCalendar file of VTODOs for any other task app.</p>
        <div className="grid" style={{ gap: 10 }}>
          <a className="btn primary" href={`${V1}/export.json`} download>Download everything (.json)</a>
          <div className="row"><select value={icsList ?? ''} onChange={e => setIcsList(e.target.value ? Number(e.target.value) : null)}><option value="">All lists</option>{lists.filter(l => !l.archived).map(l => <option key={l.id} value={l.id}>{l.name}</option>)}</select><a className="btn" href={`${V1}/export.ics${icsList ? `?list_id=${icsList}` : ''}`} download>Download .ics</a></div>
        </div>
        {user?.role === 'admin' && <><hr className="mt mb" /><p className="muted">The database (every account, settings, attachments index) — admins only.</p><a className="btn" href={`${V1}/backup`}>Download database</a></>}
      </Card>
      <Card title="Import">
        <p className="muted">An <code>.ics</code> file of VTODOs (from Nextcloud Tasks, Reminders, Thunderbird…) or a Pocket Tasks <code>.json</code> export. VTODOs go into the list below; with none chosen a list named after the calendar is made.</p>
        <Field label="Into the list"><select value={into ?? ''} onChange={e => setInto(e.target.value ? Number(e.target.value) : null)}><option value="">A new list named after the file</option>{lists.filter(l => !l.archived).map(l => <option key={l.id} value={l.id}>{l.name}{l.id === settings?.default_list_id ? ' (default)' : ''}</option>)}</select></Field>
        <label className="btn mt">{busy ? 'Importing…' : 'Choose a file…'}<input type="file" accept=".ics,.json,text/calendar,application/json" hidden onChange={e => { const f = Array.from(e.target.files || []); e.target.value = ''; if (f.length) importFiles(f) }} /></label>
      </Card>
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
    <Card title="Users" actions={isAdmin && <button className="btn primary sm" onClick={() => setEdit({ role: 'member' })}>+ Add user</button>}>
      <p className="muted">Everyone signs in with their own account and has their own lists, tasks and settings — admins included; nobody sees anyone else's tasks. Admins manage the accounts and the app title.</p>
      {!users ? <Loading /> : <table className="table compact">
        <thead><tr><th /><th>Name</th><th>Email</th><th>CalDAV user</th><th>Role</th><th /></tr></thead>
        <tbody>
          {users.map(u => (
            <tr key={u.id}>
              <td style={{ width: 32 }}><Avatar user={u} /></td>
              <td>{u.name || <span className="muted">—</span>}{u.id === user?.id && <span className="badge" style={{ marginLeft: 6 }}>you</span>}</td>
              <td className="muted">{u.email}</td>
              <td className="mono muted">{u.username}</td>
              <td><span className={`badge ${u.role === 'admin' ? 'cat' : ''}`}>{u.role}</span></td>
              <td className="right">{isAdmin && <span className="row" style={{ justifyContent: 'flex-end' }}><button className="btn ghost sm" onClick={() => setEdit(u)}>Edit</button>{u.id !== user?.id && <button className="btn ghost sm danger-text" onClick={() => setDel(u)}>Remove</button>}</span>}</td>
            </tr>
          ))}
        </tbody>
      </table>}
      {status?.auth_mode !== 'local' && <p className="muted small mt">Auth mode is <code>{status?.auth_mode}</code>: users are created automatically on first visit.</p>}
      {edit && <UserModal user={edit} local={status?.auth_mode === 'local'} onClose={() => setEdit(null)} onSaved={() => { setEdit(null); reload() }} />}
      {del && <Confirm title={`Remove ${del.name || del.email}?`} message="Their account, lists and tasks are deleted." onCancel={() => setDel(null)} onConfirm={() => { const d = del; setDel(null); run(() => api.del(`${V1}/users/${d.id}`), 'User removed') }} />}
    </Card>
  )
}

function UserModal({ user, local, onClose, onSaved }: { user: Partial<User>; local: boolean; onClose: () => void; onSaved: () => void }) {
  const { toast } = useToast()
  const [f, setF] = useState({ name: user.name || '', email: user.email || '', username: user.username || '', password: '', role: user.role || 'member' })
  const [busy, setBusy] = useState(false)
  const save = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try { const body = { ...f, username: f.username || undefined, password: f.password || undefined }; if (user.id) await api.put(`${V1}/users/${user.id}`, body); else await api.post(`${V1}/users`, body); toast(user.id ? 'User saved' : 'User added'); onSaved() } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <Modal title={user.id ? 'Edit user' : 'Add user'} onClose={onClose}>
      <form onSubmit={save} className="grid" style={{ gap: 14 }}>
        <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} autoFocus required maxLength={60} /></Field>
        <Field label="Email" help="Used to sign in."><input type="email" value={f.email} onChange={e => setF({ ...f, email: e.target.value })} required /></Field>
        <Field label="CalDAV user name" help="Empty: the part of the email before @."><input value={f.username} onChange={e => setF({ ...f, username: e.target.value })} spellCheck={false} autoComplete="off" /></Field>
        {local && <Field label={user.id ? 'New password (leave empty to keep)' : 'Password'} help="At least 8 characters."><input type="password" value={f.password} onChange={e => setF({ ...f, password: e.target.value })} minLength={user.id ? 0 : 8} required={!user.id} autoComplete="new-password" /></Field>}
        <Field group label="Role"><Segmented value={f.role} onChange={v => setF({ ...f, role: v })} options={[{ value: 'member', label: 'Member' }, { value: 'admin', label: 'Admin' }]} /></Field>
        <div className="form-actions"><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy}>{user.id ? 'Save' : 'Add user'}</button></div>
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
    <Card title="Profile">
      <form onSubmit={save} className="grid" style={{ gap: 14, maxWidth: 480 }}>
        <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} /></Field>
        <Field label="Email"><input type="email" value={f.email} onChange={e => setF({ ...f, email: e.target.value })} /></Field>
        <Field label="User name" help="The CalDAV user name your apps sign in with. Changing it means re-entering it in every app."><input value={f.username} onChange={e => setF({ ...f, username: e.target.value })} spellCheck={false} autoComplete="off" /></Field>
        {status?.auth_mode === 'local' && <>
          <hr />
          <Field label="Current password"><input type="password" value={f.current_password} onChange={e => setF({ ...f, current_password: e.target.value })} autoComplete="current-password" /></Field>
          <Field label="New password" help="At least 8 characters. Leave empty to keep the current one."><input type="password" value={f.new_password} onChange={e => setF({ ...f, new_password: e.target.value })} autoComplete="new-password" /></Field>
        </>}
        <div><button className="btn primary" disabled={busy}>Save</button></div>
      </form>
    </Card>
  )
}

function About() {
  const { data } = useAsync(() => api.get<SystemInfo>(`${V1}/settings/system`), [])
  const counts = data?.counts || {}
  return (
    <Card title="About">
      <p><b>Pocket Tasks</b> is a Things-style task manager with a real CalDAV server underneath: Inbox, Today, Upcoming, Anytime, Someday and Logbook; areas, lists, sections, subtasks; when and deadline apart; repeats fixed or after completion; alarms pushed from the server; tasks mirrored as events on any calendar; Nextcloud Tasks import and two-way sync; quick add in plain words.</p>
      <p className="muted small">Open source (MIT) — <a href="https://github.com/abjelosevic88/pocket-homelab-tasks" target="_blank" rel="noreferrer">github.com/abjelosevic88/pocket-homelab-tasks</a>. One of the Pocket apps.</p>
      {data && (
        <table className="table compact mt">
          <tbody>
            <tr><th>Version</th><td className="mono">{data.version}</td></tr>
            <tr><th>Runtime</th><td className="mono">{data.go}</td></tr>
            <tr><th>Database</th><td className="mono">{data.data_dir} · {bytes(data.db_size)}</td></tr>
            <tr><th>Address</th><td className="mono">{data.base_url}</td></tr>
            <tr><th>CalDAV</th><td className="mono">{data.caldav?.url} · {data.caldav?.username}</td></tr>
            <tr><th>Pocket Core</th><td className="mono">{data.core?.configured ? (data.core.registered ? `registered at ${data.core.url}` : `not registered: ${data.core.error}`) : 'not configured'}</td></tr>
            <tr><th>Yours</th><td className="mono">{Object.entries(counts).map(([k, v]) => `${v} ${k}`).join(' · ')}</td></tr>
          </tbody>
        </table>
      )}
    </Card>
  )
}
