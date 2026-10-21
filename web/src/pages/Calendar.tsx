import { useEffect, useMemo, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { CalendarDays, Task } from '../lib/types'
import { addDays, addMonths, dayOf, fmtTime, monthGrid, monthTitle, sameMonth, today, wdDate, weekDays, weekdayNames, withTime, hasTime, timeOf, join } from '../lib/dates'
import { Modal, PageHeader, Segmented, useAsync, useToast } from '../components/ui'
import { QuickAdd } from '../components/QuickAdd'
import { Icon } from '../components/icons'
import { priorityLevel } from '../lib/format'

type Mode = 'month' | 'week'
type Item = { task: Task; kind: 'start' | 'due' | 'done' }

export default function Calendar() {
  const { settings, version, bump } = useApp()
  const { toast } = useToast()
  const [, setParams] = useSearchParams()
  const ws = settings?.week_start ?? 1
  const [mode, setMode] = useState<Mode>(() => (localStorage.getItem('pt-cal') as Mode) || 'month')
  const [anchor, setAnchor] = useState(today())
  const [addDay, setAddDay] = useState<string | null>(null)
  const [drag, setDrag] = useState<{ item: Item; x: number; y: number } | null>(null)
  const [overDay, setOverDay] = useState<string | null>(null)
  const press = useRef<{ item: Item; x: number; y: number } | null>(null)
  useEffect(() => { localStorage.setItem('pt-cal', mode) }, [mode])

  const rows = useMemo(() => mode === 'month' ? monthGrid(anchor, ws) : [weekDays(anchor, ws)], [mode, anchor, ws])
  const from = rows[0][0], to = rows[rows.length - 1][6]
  const { data, reload } = useAsync(() => api.get<CalendarDays>(`${V1}/calendar?from=${from}&to=${to}`), [from, to])
  useEffect(() => { if (version > 0) reload() }, [version]) // eslint-disable-line react-hooks/exhaustive-deps

  const items = (day: string): Item[] => {
    const d = data?.days?.[day]
    if (!d) return []
    const out: Item[] = []
    const seen = new Set<number>()
    for (const t of d.start || []) { out.push({ task: t, kind: 'start' }); seen.add(t.id) }
    for (const t of d.due || []) if (!seen.has(t.id)) { out.push({ task: t, kind: 'due' }); seen.add(t.id) } else out.find(i => i.task.id === t.id)!.kind = 'start'
    for (const t of d.done || []) if (!seen.has(t.id)) out.push({ task: t, kind: 'done' })
    return out.sort((a, b) => (a.kind === 'done' ? 1 : 0) - (b.kind === 'done' ? 1 : 0) || (timeOf(a.task.start) || '~').localeCompare(timeOf(b.task.start) || '~'))
  }
  const openTask = (t: Task) => setParams(p => { const n = new URLSearchParams(p); n.set('task', String(t.id)); return n })
  const step = (n: number) => setAnchor(a => mode === 'month' ? addMonths(a, n) : addDays(a, 7 * n))

  // Drag a task to another day: the field it had moves (both → start).
  useEffect(() => {
    const move = (e: PointerEvent) => {
      const pr = press.current
      if (!pr) return
      if (!drag) { if (Math.hypot(e.clientX - pr.x, e.clientY - pr.y) < 6) return; setDrag({ ...pr, x: e.clientX, y: e.clientY }); document.body.classList.add('dragging') }
      setDrag(d => d && { ...d, x: e.clientX, y: e.clientY })
      const el = document.elementFromPoint(e.clientX, e.clientY) as HTMLElement | null
      setOverDay(el?.closest<HTMLElement>('[data-day]')?.dataset.day || null)
    }
    const up = async () => {
      const pr = press.current, d = drag, day = overDay
      press.current = null; document.body.classList.remove('dragging'); setDrag(null); setOverDay(null)
      if (!pr || !d || !day) return
      const t = pr.item.task
      const field = t.start ? 'start' : 'due'
      const cur = t[field]
      if (!cur || dayOf(cur) === day) return
      try { await api.put(`${V1}/tasks/${t.id}`, { [field]: hasTime(cur) ? withTime(day, timeOf(cur)) : day }); bump(); toast(`Moved to ${wdDate(day)}`) } catch (e) { toast((e as Error).message, 'err') }
    }
    window.addEventListener('pointermove', move); window.addEventListener('pointerup', up)
    return () => { window.removeEventListener('pointermove', move); window.removeEventListener('pointerup', up) }
  }, [drag, overDay]) // eslint-disable-line react-hooks/exhaustive-deps

  const now = today()
  const names = weekdayNames(ws)
  return (
    <div className="page cal-page">
      <PageHeader title="Calendar" sub={mode === 'month' ? monthTitle(anchor) : `${wdDate(rows[0][0])} – ${wdDate(rows[0][6])}`} actions={<div className="row wrap">
        <Segmented size="sm" value={mode} onChange={setMode} options={[{ value: 'month', label: 'Month' }, { value: 'week', label: 'Week' }]} />
        <button type="button" className="btn sm" onClick={() => setAnchor(today())}>Today</button>
        <span className="row" style={{ gap: 2 }}>
          <button type="button" className="btn ghost icon-only sm" onClick={() => step(-1)} aria-label="Previous" title="Previous"><Icon name="chevron-left" size={17} /></button>
          <button type="button" className="btn ghost icon-only sm" onClick={() => step(1)} aria-label="Next" title="Next"><Icon name="chevron-right" size={17} /></button>
        </span>
      </div>} />
      <div className={`cal ${mode}`}>
        {names.map(n => <div key={n} className="cal-head">{n}</div>)}
        {rows.map(r => r.map(day => {
          const its = items(day)
          const dim = mode === 'month' && !sameMonth(day, anchor)
          return (
            <div key={day} data-day={day} className={`cal-cell ${day === now ? 'today' : ''} ${dim ? 'dim' : ''} ${overDay === day ? 'over' : ''}`} onClick={e => { if ((e.target as HTMLElement).closest('.cal-item')) return; setAddDay(day) }}>
              <div className="cal-n"><span className="n">{day.slice(8).replace(/^0/, '')}</span>{mode === 'week' && <small className="muted">{its.length ? its.length : ''}</small>}</div>
              <div className="cal-items">
                {its.slice(0, mode === 'week' ? 50 : 4).map(it => <CalItem key={it.task.id + it.kind} it={it} onOpen={openTask} onPress={(it, e) => { if (e.pointerType !== 'touch' && it.kind !== 'done') press.current = { item: it, x: e.clientX, y: e.clientY } }} dragging={drag?.item.task.id === it.task.id} />)}
                {mode === 'month' && its.length > 4 && <button type="button" className="cal-overflow" onClick={e => { e.stopPropagation(); setAnchor(day); setMode('week') }}>+{its.length - 4} more</button>}
              </div>
            </div>
          )
        }))}
      </div>
      {drag && <div className="drag-ghost" style={{ left: drag.x + 12, top: drag.y + 8 }}>{drag.item.task.title}</div>}
      {addDay && <Modal title={`New task · ${wdDate(addDay)}`} onClose={() => setAddDay(null)}>
        <QuickAdd autoFocus defaults={{ start: addDay }} onAdded={() => setAddDay(null)} onCancel={() => setAddDay(null)} placeholder="Add a task for this day…" />
        <p className="muted small mt-s">A time in the text (e.g. <code>9am</code>) makes it a timed task. {addDay && <button type="button" className="link-btn" onClick={() => setAddDay(join(addDay, settings?.day_start || '09:00'))}>Start at {settings?.day_start || '09:00'}</button>}</p>
      </Modal>}
    </div>
  )
}

function CalItem({ it, onOpen, onPress, dragging }: { it: Item; onOpen: (t: Task) => void; onPress: (it: Item, e: React.PointerEvent) => void; dragging: boolean }) {
  const t = it.task
  const time = it.kind === 'start' ? fmtTime(t.start) : it.kind === 'due' ? fmtTime(t.due) : ''
  const pr = priorityLevel(t.priority)
  return (
    <button type="button" className={`cal-item ${it.kind} ${t.overdue ? 'overdue' : ''} ${dragging ? 'dragging' : ''}`} style={{ ['--lc' as string]: t.list?.color || 'var(--accent)' }} onClick={e => { e.stopPropagation(); onOpen(t) }} onPointerDown={e => onPress(it, e)} title={`${t.title}${t.list ? ` · ${t.list.name}` : ''}`}>
      <span className="bar" />
      {it.kind === 'done' ? <span className="ico"><Icon name="check" size={11} strokeWidth={2.6} /></span> : it.kind === 'due' ? <span className="ico"><Icon name="flag" size={11} /></span> : null}
      {time && <span className="time">{time}</span>}
      <span className="grow ellipsis">{pr !== 'none' && it.kind !== 'done' && <span className={`prio ${pr}`}>!</span>}{t.title}</span>
    </button>
  )
}
