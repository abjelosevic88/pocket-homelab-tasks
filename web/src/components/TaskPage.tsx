import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { api, qs, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Filter, List, Section, Task, TaskGroup, TaskList, TaskQuery } from '../lib/types'
import { inField, onTask } from '../lib/events'
import { dayHeading, label, today } from '../lib/dates'
import { plural } from '../lib/format'
import { Confirm, Empty, Loading, type MenuItem, MenuButton, Modal, Prompt, modalOpen, useToast } from './ui'
import { TaskRow } from './TaskRow'
import { useTaskCompletion } from '../lib/use-task-completion'
import { QuickAdd, type QuickDefaults } from './QuickAdd'
import { DatePanel, TagInput } from './pickers'
import { Icon, ListRing, ViewIcon } from './icons'

export interface TaskPageProps {
  title: React.ReactNode
  sub?: React.ReactNode
  query: Record<string, string | number | boolean | undefined>
  grouping: 'none' | 'server' | 'today' | 'sections'
  showList?: boolean
  list?: List
  sections?: Section[]
  quick?: boolean
  quickDefaults?: QuickDefaults
  headerActions?: React.ReactNode
  /** Extra entries for the ⋯ menu in the header (a list's own menu). */
  menu?: MenuItem[]
  filterQuery?: TaskQuery
  emptyTitle?: string
  emptyText?: React.ReactNode
  doneTail?: boolean
  doneTime?: boolean
  canReorder?: boolean
  /** A smart view's key, for its icon in the header. */
  view?: string
  icon?: React.ReactNode
}

interface Group { key: string; title: React.ReactNode; tasks: Task[]; cls?: string; section?: Section | null; count?: number }

const isDay = (k: string) => /^\d{4}-\d{2}-\d{2}$/.test(k)

export function TaskPage(p: TaskPageProps) {
  const { version, bump, settings, lists, areas } = useApp()
  const { toast } = useToast()
  const { toggle, pending: completing } = useTaskCompletion()
  const navigate = useNavigate()
  const [params, setParams] = useSearchParams()
  const [data, setData] = useState<TaskList | null>(null)
  const [error, setError] = useState('')
  const [sel, setSel] = useState<Set<number>>(new Set())
  const [focus, setFocus] = useState<number | null>(null)
  const [doneOpen, setDoneOpen] = useState(false)
  const [sectionPrompt, setSectionPrompt] = useState<{ kind: 'rename' | 'new'; section?: Section } | null>(null)
  const [sectionDel, setSectionDel] = useState<Section | null>(null)
  const [saveFilter, setSaveFilter] = useState(false)
  const [moveOpen, setMoveOpen] = useState<number[] | null>(null)
  const [dateOpen, setDateOpen] = useState<{ ids: number[]; kind: 'start' | 'due' } | null>(null)
  const [tagOpen, setTagOpen] = useState<number[] | null>(null)
  const [confirmDel, setConfirmDel] = useState<number[] | null>(null)
  const [drag, setDrag] = useState<{ id: number; x: number; y: number; title: string } | null>(null)
  const [over, setOver] = useState<{ kind: 'row' | 'section' | 'list'; id: number; pos: 'before' | 'after' } | null>(null)
  const quickRef = useRef<HTMLInputElement>(null)
  const openId = Number(params.get('task')) || null
  const key = JSON.stringify(p.query)

  const load = useCallback(async () => {
    try { setData(await api.get<TaskList>(`${V1}/tasks${qs({ ...p.query, nest: 1 })}`)); setError('') } catch (e) { setError((e as Error).message) }
  }, [key]) // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => { setData(null); load() }, [load])
  useEffect(() => { if (version > 0) load() }, [version, load])
  useEffect(() => onTask(t => setData(d => d ? { ...d, tasks: d.tasks.map(x => x.id === t.id ? { ...x, ...t, subtasks: t.subtasks ?? x.subtasks } : x.subtasks ? { ...x, subtasks: x.subtasks.map(s => s.id === t.id ? { ...s, ...t } : s) } : x) } : d)), [])

  const byId = useMemo(() => new Map((data?.tasks || []).map(t => [t.id, t])), [data])
  const listOf = (id: number) => lists.find(l => l.id === id)
  const groups = useMemo<Group[]>(() => {
    const all = data?.tasks || []
    const nested = (t: Task) => !!t.parent_id && all.some(x => x.id === t.parent_id)
    const tasks = all.filter(t => !nested(t))
    const open = tasks.filter(t => t.status !== 'done' && t.status !== 'cancelled')
    const done = tasks.filter(t => t.status === 'done' || t.status === 'cancelled')
    if (p.grouping === 'today') {
      const od = open.filter(t => t.overdue), rest = open.filter(t => !t.overdue)
      const g: Group[] = []
      if (od.length) g.push({ key: 'overdue', title: 'Overdue', tasks: od, cls: 'overdue' })
      g.push({ key: 'today', title: od.length ? 'Today' : '', tasks: rest })
      return g
    }
    if (p.grouping === 'sections') {
      const g: Group[] = [{ key: 'none', title: '', tasks: open.filter(t => !t.section_id || !(p.sections || []).some(s => s.id === t.section_id)), section: null }]
      for (const s of p.sections || []) g.push({ key: 's' + s.id, title: s.name, tasks: open.filter(t => t.section_id === s.id), section: s })
      return g
    }
    if (p.grouping === 'server' && data?.groups?.length) {
      const seen = new Set<number>()
      const g: Group[] = data.groups.map((gr: TaskGroup) => {
        const ts = gr.ids.map(id => byId.get(id)).filter((t): t is Task => !!t && !nested(t))
        ts.forEach(t => seen.add(t.id))
        let title: React.ReactNode = gr.title
        let cls = ''
        if (isDay(gr.key)) { title = dayHeading(gr.key); if (gr.key < today()) cls = 'overdue' }
        else { const l = lists.find(l => String(l.id) === gr.key || l.name === gr.title); if (l) title = <><span className="dot" style={{ background: l.color }} />{l.name}</> }
        return { key: gr.key, title, tasks: ts, cls }
      })
      const rest = tasks.filter(t => !seen.has(t.id))
      if (rest.length) g.push({ key: 'rest', title: 'Other', tasks: rest })
      return g.filter(x => x.tasks.length)
    }
    if (p.grouping === 'server') {
      // No groups from the server: group here by day (upcoming, logbook) or by list (anytime, someday).
      const view = String(p.query.view || '')
      const keyOf = (t: Task) => view === 'logbook' ? (t.completed_at ? t.completed_at.slice(0, 10) : '') : view === 'upcoming' ? (t.start || t.due || '').slice(0, 10) : String(t.list_id)
      const m = new Map<string, Task[]>()
      for (const t of tasks) { const k = keyOf(t); m.set(k, [...(m.get(k) || []), t]) }
      const keys = [...m.keys()].sort((a, b) => view === 'logbook' ? b.localeCompare(a) : a.localeCompare(b))
      return keys.map(k => { const l = isDay(k) ? null : listOf(Number(k)); return { key: k, title: isDay(k) ? dayHeading(k) : l ? <><span className="dot" style={{ background: l.color }} />{l.name}</> : '', tasks: m.get(k)!, cls: isDay(k) && k < today() && view !== 'logbook' ? 'overdue' : '' } })
    }
    return [{ key: 'all', title: '', tasks: p.doneTail ? open : tasks }]
  }, [data, p.grouping, p.sections, p.doneTail, byId, lists]) // eslint-disable-line react-hooks/exhaustive-deps
  const doneTail = useMemo(() => p.doneTail ? (data?.tasks || []).filter(t => t.status === 'done' || t.status === 'cancelled') : [], [data, p.doneTail])
  const openCount = useMemo(() => (data?.tasks || []).filter(t => t.status !== 'done' && t.status !== 'cancelled' && !(t.parent_id && (data?.tasks || []).some(x => x.id === t.parent_id))).length, [data])

  // The order the keyboard walks: every visible row, subtasks under their parent.
  const flat = useMemo(() => {
    const out: Task[] = []
    for (const g of groups) for (const t of g.tasks) { out.push(t); if (t.subtasks) for (const s of t.subtasks) out.push(s) }
    if (doneOpen) out.push(...doneTail)
    return out
  }, [groups, doneTail, doneOpen])

  const open = (t: Task) => { setFocus(t.id); setParams(prev => { const n = new URLSearchParams(prev); n.set('task', String(t.id)); return n }) }
  const closeDetail = () => setParams(prev => { const n = new URLSearchParams(prev); n.delete('task'); return n })

  const select = (t: Task, e: React.MouseEvent) => {
    setSel(s => {
      const n = new Set(s)
      if (e.shiftKey && focus != null) {
        const a = flat.findIndex(x => x.id === focus), b = flat.findIndex(x => x.id === t.id)
        if (a >= 0 && b >= 0) for (let i = Math.min(a, b); i <= Math.max(a, b); i++) n.add(flat[i].id)
      } else if (n.has(t.id)) n.delete(t.id); else n.add(t.id)
      return n
    })
    setFocus(t.id)
  }
  const targets = () => (sel.size ? [...sel] : focus != null ? [focus] : [])

  const batch = async (body: Record<string, unknown>, msg?: string) => {
    const ids = body.ids as number[]
    if (!ids.length) return
    try { await api.post(`${V1}/tasks/batch`, body); setSel(new Set()); if (msg) toast(msg); bump() } catch (e) { toast((e as Error).message, 'err') }
  }
  const putOne = async (id: number, body: Record<string, unknown>) => {
    try { await api.put(`${V1}/tasks/${id}`, body); bump() } catch (e) { toast((e as Error).message, 'err') }
  }
  const setPriority = (pr: number) => { const ids = targets(); if (ids.length > 1) batch({ ids, set: { priority: pr } }); else if (ids[0]) putOne(ids[0], { priority: pr }) }
  const trash = (ids: number[]) => batch({ ids, action: 'delete' }, ids.length === 1 ? 'Moved to the trash' : `${ids.length} tasks moved to the trash`)

  // Keyboard: the list is driven without the mouse.
  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      if (inField(e.target) || modalOpen() || e.metaKey || e.ctrlKey || e.altKey) return
      if (document.querySelector('.menu, .popover, .select-pop, .sheet-backdrop')) return
      if ((e.target as HTMLElement)?.closest?.('.task-detail')) return
      const i = flat.findIndex(t => t.id === focus)
      const cur = i >= 0 ? flat[i] : null
      switch (e.key) {
        case 'ArrowDown': e.preventDefault(); if (flat.length) { const n = flat[Math.min(flat.length - 1, i + 1)]; setFocus(n.id); if (e.shiftKey) setSel(s => new Set([...s, n.id])); if (openId) open(n) } break
        case 'ArrowUp': e.preventDefault(); if (flat.length) { const n = flat[Math.max(0, i <= 0 ? 0 : i - 1)]; setFocus(n.id); if (e.shiftKey) setSel(s => new Set([...s, n.id])); if (openId) open(n) } break
        case 'Enter': if (cur) { e.preventDefault(); open(cur) } break
        case ' ': if (cur) { e.preventDefault(); toggle(cur) } break
        case 'n': case '/': e.preventDefault(); quickRef.current?.focus(); break
        case 't': { const ids = targets(); if (!ids.length) break; e.preventDefault(); if (ids.length > 1) batch({ ids, set: { start: today() } }); else putOne(ids[0], { start: today() }); break }
        case 'd': { const ids = targets(); if (ids.length) { e.preventDefault(); setDateOpen({ ids, kind: 'due' }) } break }
        case 'w': { const ids = targets(); if (ids.length) { e.preventDefault(); setDateOpen({ ids, kind: 'start' }) } break }
        case '1': e.preventDefault(); setPriority(1); break
        case '2': e.preventDefault(); setPriority(5); break
        case '3': e.preventDefault(); setPriority(9); break
        case '0': e.preventDefault(); setPriority(0); break
        case 's': { const ids = targets(); if (!ids.length) break; e.preventDefault(); if (ids.length > 1) batch({ ids, set: { someday: true } }); else if (cur) putOne(cur.id, { someday: !cur.someday }); break }
        case 'm': { const ids = targets(); if (ids.length) { e.preventDefault(); setMoveOpen(ids) } break }
        case 'Backspace': case 'Delete': { const ids = targets(); if (ids.length) { e.preventDefault(); if (ids.length > 1) setConfirmDel(ids); else trash(ids) } break }
        case 'a': if (e.shiftKey) { e.preventDefault(); setSel(new Set(flat.map(t => t.id))) } break
        case 'Escape': if (sel.size) setSel(new Set()); else if (openId) closeDetail(); else setFocus(null); break
      }
    }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }) // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => { if (focus != null) document.querySelector<HTMLElement>(`.task-row[data-task-id="${focus}"]`)?.scrollIntoView({ block: 'nearest' }) }, [focus])

  // Drag with the pointer: a row moves within its group, onto a section heading or a sidebar list.
  const pressRef = useRef<{ id: number; x: number; y: number; title: string } | null>(null)
  const onPointerDown = (t: Task, e: React.PointerEvent) => {
    if (e.button !== 0 || e.pointerType === 'touch' || !p.canReorder) return
    pressRef.current = { id: t.id, x: e.clientX, y: e.clientY, title: t.title }
  }
  useEffect(() => {
    const move = (e: PointerEvent) => {
      const pr = pressRef.current
      if (!pr) return
      if (!drag) { if (Math.hypot(e.clientX - pr.x, e.clientY - pr.y) < 6) return; setDrag({ ...pr, x: e.clientX, y: e.clientY }); document.body.classList.add('dragging') }
      setDrag(d => d && { ...d, x: e.clientX, y: e.clientY })
      const el = document.elementFromPoint(e.clientX, e.clientY) as HTMLElement | null
      const row = el?.closest<HTMLElement>('[data-task-id]')
      const sec = el?.closest<HTMLElement>('[data-drop-section]')
      const lst = el?.closest<HTMLElement>('[data-drop-list]')
      if (row && Number(row.dataset.taskId) !== pr.id) { const r = row.getBoundingClientRect(); setOver({ kind: 'row', id: Number(row.dataset.taskId), pos: e.clientY < r.top + r.height / 2 ? 'before' : 'after' }) }
      else if (sec) setOver({ kind: 'section', id: Number(sec.dataset.dropSection), pos: 'after' })
      else if (lst) setOver({ kind: 'list', id: Number(lst.dataset.dropList), pos: 'after' })
      else setOver(null)
    }
    const up = async () => {
      const pr = pressRef.current, d = drag, o = over
      pressRef.current = null
      document.body.classList.remove('dragging')
      setDrag(null); setOver(null)
      if (!pr || !d || !o) return
      try {
        if (o.kind === 'row') {
          const t = byId.get(o.id) || flat.find(x => x.id === o.id)
          if (!t) return
          const body: Record<string, unknown> = { [o.pos === 'before' ? 'before_id' : 'after_id']: o.id, list_id: t.list_id, section_id: t.section_id, parent_id: t.parent_id }
          await api.post(`${V1}/tasks/${pr.id}/move`, body)
        } else if (o.kind === 'section') await api.post(`${V1}/tasks/${pr.id}/move`, { list_id: p.list?.id, section_id: o.id || null })
        else if (o.kind === 'list') { await api.post(`${V1}/tasks/${pr.id}/move`, { list_id: o.id, section_id: null }); toast(`Moved to ${listOf(o.id)?.name || 'the list'}`) }
        bump()
      } catch (e) { toast((e as Error).message, 'err') }
    }
    window.addEventListener('pointermove', move)
    window.addEventListener('pointerup', up)
    return () => { window.removeEventListener('pointermove', move); window.removeEventListener('pointerup', up) }
  }, [drag, over, byId, flat, p.list?.id]) // eslint-disable-line react-hooks/exhaustive-deps

  const sectionAction = async (fn: () => Promise<unknown>, msg: string) => { try { await fn(); toast(msg); bump() } catch (e) { toast((e as Error).message, 'err') } }

  const row = (t: Task, depth = 0, showDone = false) => (
    <React.Fragment key={t.id}>
      <div className={`task-slot ${over?.kind === 'row' && over.id === t.id ? `over-${over.pos}` : ''}`}>
        <TaskRow task={t} pending={completing.has(t.id)} depth={depth} showList={p.showList} selected={sel.has(t.id)} focused={focus === t.id || openId === t.id} dragging={drag?.id === t.id} showDone={showDone || p.doneTime} onToggle={toggle} onOpen={open} onSelect={select} onPointerDown={onPointerDown} />
      </div>
      {t.subtasks?.filter(s => s.status !== 'done' || showDone || settings?.show_done_in_lists).map(s => row(s, depth + 1, showDone))}
    </React.Fragment>
  )

  const count = openCount
  return (
    <div className="page task-page">
      <header className="view-head">
        <div className="view-title">
          {(p.icon || p.view || p.list) && <span className="view-ico">{p.icon || (p.view ? <ViewIcon view={p.view} size={26} /> : <ListRing color={p.list!.color} size={24} />)}</span>}
          <h1>{p.title}</h1>
          {data && count > 0 && <span className="view-count">{count}</span>}
          <span className="spacer" />
          {p.headerActions}
          <MenuButton label="More" align="right" width={230} items={() => [
            ...(p.filterQuery ? [{ label: 'Save this view as a filter…', icon: 'filter', onSelect: () => setSaveFilter(true) }] : []),
            { label: 'Select all', icon: 'check', hint: '⇧A', onSelect: () => setSel(new Set(flat.map(t => t.id))) },
            ...(p.grouping === 'sections' && p.list ? [{ label: 'Add section…', icon: 'plus', onSelect: () => setSectionPrompt({ kind: 'new' }) }] : []),
            { label: 'Reload', icon: 'refresh', onSelect: () => load() },
            ...(p.menu?.length ? ['sep' as const, ...p.menu] : []),
          ]}><Icon name="more" size={18} /></MenuButton>
        </div>
        {p.sub && <div className="view-sub">{p.sub}</div>}
      </header>
      {p.quick !== false && <QuickAdd inputRef={quickRef} defaults={p.quickDefaults} />}
      {error && <div className="callout danger mt">{error}</div>}
      {!data && !error && <Loading />}
      {data && !flat.length && !doneTail.length && <Empty title={p.emptyTitle || 'Nothing here'} icon={p.view ? <ViewIcon view={p.view} size={40} /> : p.list ? <ListRing color={p.list.color} size={36} /> : undefined}>{p.emptyText}</Empty>}
      {data && groups.map(g => (
        (g.tasks.length > 0 || g.section !== undefined) && <section key={g.key} className={`task-group ${g.cls || ''}`}>
          {(g.title || g.section) && <div className={`group-head ${g.section ? 'section-head' : ''} ${over?.kind === 'section' && g.section && over.id === g.section.id ? 'over' : ''}`} data-drop-section={g.section ? g.section.id : undefined}>
            <h3>{g.title}</h3>{g.tasks.length > 0 && <span className="group-count">{g.tasks.length}</span>}
            <span className="spacer" />
            {g.section && <MenuButton label={`Options for ${g.section.name}`} className="btn ghost icon-only sm group-more" align="right" width={200} items={[
              { label: 'Rename…', icon: 'edit', onSelect: () => setSectionPrompt({ kind: 'rename', section: g.section! }) },
              { label: 'Delete section…', icon: 'trash', danger: true, onSelect: () => setSectionDel(g.section!) },
            ]}><Icon name="more" size={15} /></MenuButton>}
          </div>}
          <div className="task-list">{g.tasks.map(t => row(t))}</div>
          {g.section && p.list && <QuickAdd compact defaults={{ ...p.quickDefaults, list_id: p.list.id, section_id: g.section.id }} placeholder={`Add to ${g.section.name}…`} />}
        </section>
      ))}
      {data && doneTail.length > 0 && <section className="task-group done-tail">
        <button type="button" className="done-toggle" onClick={() => setDoneOpen(o => !o)}><Icon name={doneOpen ? 'chevronDown' : 'chevronRight'} size={13} />{doneOpen ? 'Hide' : 'Show'} {plural(doneTail.length, 'completed task')}</button>
        {doneOpen && <div className="task-list">{doneTail.map(t => row(t, 0, true))}</div>}
      </section>}

      {sel.size > 0 && <div className="batch-bar">
        <span className="grow"><b>{plural(sel.size, 'task')}</b> selected</span>
        <button type="button" className="btn sm" onClick={() => batch({ ids: [...sel], action: 'complete' }, 'Completed')}><Icon name="check" size={14} />Complete</button>
        <button type="button" className="btn sm" onClick={() => setMoveOpen([...sel])}><Icon name="move" size={14} />Move…</button>
        <button type="button" className="btn sm" onClick={() => setDateOpen({ ids: [...sel], kind: 'start' })}><Icon name="calendar" size={14} />When…</button>
        <button type="button" className="btn sm" onClick={() => setDateOpen({ ids: [...sel], kind: 'due' })}><Icon name="flag" size={14} />Deadline…</button>
        <button type="button" className="btn sm" onClick={() => setTagOpen([...sel])}><Icon name="hash" size={14} />Tags…</button>
        <button type="button" className="btn sm danger-ghost" onClick={() => setConfirmDel([...sel])}><Icon name="trash" size={14} />Delete</button>
        <button type="button" className="btn ghost icon-only sm" onClick={() => setSel(new Set())} aria-label="Clear selection" title="Clear selection (esc)"><Icon name="x" size={15} /></button>
      </div>}

      {drag && <div className="drag-ghost" style={{ left: drag.x + 12, top: drag.y + 8 }}>{drag.title}</div>}

      {moveOpen && <MoveModal ids={moveOpen} onClose={() => setMoveOpen(null)} onDone={async (listId) => { const ids = moveOpen; setMoveOpen(null); if (ids.length === 1) { try { await api.post(`${V1}/tasks/${ids[0]}/move`, { list_id: listId, section_id: null }); bump() } catch (e) { toast((e as Error).message, 'err') } } else batch({ ids, set: { list_id: listId } }, `Moved to ${listOf(listId)?.name}`) }} />}
      {dateOpen && <Modal title={dateOpen.kind === 'due' ? 'Deadline' : 'When'} onClose={() => setDateOpen(null)} size="sm">
        <DatePanel value={dateOpen.ids.length === 1 ? (byId.get(dateOpen.ids[0]) || flat.find(t => t.id === dateOpen.ids[0]))?.[dateOpen.kind] ?? null : null} onChange={v => { const d = dateOpen; setDateOpen(null); if (d.ids.length > 1) batch({ ids: d.ids, set: { [d.kind]: v } }); else putOne(d.ids[0], { [d.kind]: v }) }} close={() => setDateOpen(null)} />
      </Modal>}
      {tagOpen && <TagModal onClose={() => setTagOpen(null)} onDone={(add, remove) => { const ids = tagOpen; setTagOpen(null); batch({ ids, set: { add_tags: add, remove_tags: remove } }, 'Tags updated') }} />}
      {confirmDel && <Confirm title={`Move ${plural(confirmDel.length, 'task')} to the trash?`} message="They can be restored from the trash." onCancel={() => setConfirmDel(null)} onConfirm={() => { const ids = confirmDel; setConfirmDel(null); trash(ids) }} />}
      {sectionPrompt && p.list && <Prompt title={sectionPrompt.kind === 'new' ? 'New section' : `Rename “${sectionPrompt.section!.name}”`} value={sectionPrompt.section?.name || ''} placeholder="Section name" button={sectionPrompt.kind === 'new' ? 'Add' : 'Rename'} onClose={() => setSectionPrompt(null)} onDone={name => { const sp = sectionPrompt; setSectionPrompt(null); sectionAction(() => sp.kind === 'new' ? api.post(`${V1}/lists/${p.list!.id}/sections`, { name }) : api.put(`${V1}/sections/${sp.section!.id}`, { name }), sp.kind === 'new' ? 'Section added' : 'Renamed') }} />}
      {sectionDel && <Confirm title={`Delete the section “${sectionDel.name}”?`} message="Its tasks stay in the list without a section." onCancel={() => setSectionDel(null)} onConfirm={() => { const s = sectionDel; setSectionDel(null); sectionAction(() => api.del(`${V1}/sections/${s.id}`), 'Section deleted') }} />}
      {saveFilter && p.filterQuery && <Prompt title="Save this view as a filter" placeholder="Filter name" button="Save" onClose={() => setSaveFilter(false)} onDone={async name => { setSaveFilter(false); try { const f = await api.post<Filter>(`${V1}/filters`, { name, query: p.filterQuery }); bump(); navigate(`/filter/${f.id}`) } catch (e) { toast((e as Error).message, 'err') } }}><p className="muted small">It appears under Filters in the sidebar.</p></Prompt>}
    </div>
  )
}

function MoveModal({ ids, onClose, onDone }: { ids: number[]; onClose: () => void; onDone: (listId: number) => void }) {
  const { lists, areas } = useApp()
  const [q, setQ] = useState('')
  const live = lists.filter(l => !l.archived && (!q || l.name.toLowerCase().includes(q.toLowerCase())))
  return (
    <Modal title={`Move ${plural(ids.length, 'task')} to…`} onClose={onClose} size="sm">
      <div className="search-field mb"><Icon name="search" size={15} /><input autoFocus value={q} onChange={e => setQ(e.target.value)} placeholder="Find a list…" onKeyDown={e => { if (e.key === 'Enter' && live[0]) onDone(live[0].id) }} /></div>
      <div className="pick-list">
        {live.map(l => <button type="button" key={l.id} className="pick-row" onClick={() => onDone(l.id)}><ListRing color={l.color} /><span className="grow">{l.name}</span><small className="muted">{areas.find(a => a.id === l.area_id)?.name || ''}</small></button>)}
        {!live.length && <p className="muted">No list matches.</p>}
      </div>
    </Modal>
  )
}

function TagModal({ onClose, onDone }: { onClose: () => void; onDone: (add: string[], remove: string[]) => void }) {
  const [add, setAdd] = useState<string[]>([])
  const [remove, setRemove] = useState<string[]>([])
  return (
    <Modal title="Tags" onClose={onClose} size="sm">
      <div className="stack">
        <div className="field"><span className="label">Add</span><TagInput value={add} onChange={setAdd} autoFocus /></div>
        <div className="field"><span className="label">Remove</span><TagInput value={remove} onChange={setRemove} /></div>
        <div className="form-actions"><button type="button" className="btn" onClick={onClose}>Cancel</button><button type="button" className="btn primary" onClick={() => onDone(add, remove)} disabled={!add.length && !remove.length}>Apply</button></div>
      </div>
    </Modal>
  )
}

