import React, { useEffect, useMemo, useRef, useState } from 'react'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Alarm, List, Repeat, Section } from '../lib/types'
import { PALETTE, PRIORITY_VALUE, priorityLevel, tint } from '../lib/format'
import { Icon } from './icons'
import { REPEAT_PRESETS, alarmLabel, dayOf, hasTime, join, label, nextWeek, thisWeekend, timeOf, today, tomorrow } from '../lib/dates'
import { Segmented } from './ui'

/** A button with a popover under it; closes on a click outside or Escape. */
export function Dropdown({ label, className = 'btn sm', title, align = 'left', children, open: forced, onOpenChange }: { label: React.ReactNode; className?: string; title?: string; align?: 'left' | 'right'; children: (close: () => void) => React.ReactNode; open?: boolean; onOpenChange?: (o: boolean) => void }) {
  const [open, setOpenRaw] = useState(false)
  const isOpen = forced ?? open
  const setOpen = (o: boolean) => { setOpenRaw(o); onOpenChange?.(o) }
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!isOpen) return
    const h = (e: MouseEvent) => { if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false) }
    const k = (e: KeyboardEvent) => { if (e.key === 'Escape') { e.stopPropagation(); setOpen(false) } }
    document.addEventListener('mousedown', h); document.addEventListener('keydown', k, true)
    return () => { document.removeEventListener('mousedown', h); document.removeEventListener('keydown', k, true) }
  }, [isOpen]) // eslint-disable-line react-hooks/exhaustive-deps
  return (
    <div className="menu-wrap" ref={ref}>
      <button type="button" className={className} title={title} onClick={e => { e.stopPropagation(); setOpen(!isOpen) }}>{label}</button>
      {isOpen && <div className={`menu pop ${align}`} onClick={e => e.stopPropagation()}>{children(() => setOpen(false))}</div>}
    </div>
  )
}

/** When / Deadline: quick days, a date, an optional time. */
export function DatePanel({ value, onChange, close, allowTime = true, quick = true }: { value: string | null; onChange: (v: string | null) => void; close?: () => void; allowTime?: boolean; quick?: boolean }) {
  const day = value ? dayOf(value) : ''
  const time = timeOf(value)
  const pick = (d: string | null) => { onChange(d === null ? null : join(d, time)); close?.() }
  return (
    <div className="date-panel">
      {quick && <div className="date-quick">
        <button type="button" onClick={() => pick(today())}>Today</button>
        <button type="button" onClick={() => pick(tomorrow())}>Tomorrow</button>
        <button type="button" onClick={() => pick(thisWeekend())}>This weekend</button>
        <button type="button" onClick={() => pick(nextWeek())}>Next week</button>
      </div>}
      <div className="row">
        <input type="date" value={day} onChange={e => onChange(e.target.value ? join(e.target.value, time) : null)} />
        {allowTime && <input type="time" value={time} disabled={!day} onChange={e => onChange(join(day || today(), e.target.value))} style={{ width: 110 }} />}
      </div>
      <div className="row" style={{ justifyContent: 'space-between' }}>
        <button type="button" className="btn ghost sm danger-text" onClick={() => pick(null)} disabled={!value}>Clear</button>
        {close && <button type="button" className="btn sm" onClick={close}>Done</button>}
      </div>
    </div>
  )
}

export function DateButton({ value, onChange, kind, placeholder }: { value: string | null; onChange: (v: string | null) => void; kind: 'start' | 'due'; placeholder: string }) {
  const overdue = kind === 'due' && value && dayOf(value) < today()
  return (
    <Dropdown className={`btn sm chipbtn ${value ? 'set' : ''} ${overdue ? 'overdue' : ''}`} label={<><Icon name={kind === 'due' ? 'flag' : 'calendar'} size={13} />{value ? label(value) : placeholder}</>}>
      {close => <DatePanel value={value} onChange={onChange} close={close} />}
    </Dropdown>
  )
}

/** Lists grouped by area, for <select>. */
export function listOptions(lists: List[], areas: { id: number; name: string }[]) {
  const live = lists.filter(l => !l.archived)
  const byArea = (id: number | null) => live.filter(l => l.area_id === id)
  const groups: { name: string; lists: List[] }[] = [{ name: '', lists: byArea(null) }, ...areas.map(a => ({ name: a.name, lists: byArea(a.id) }))].filter(g => g.lists.length)
  return groups
}

export function ListSelect({ value, onChange, className = '' }: { value: number | null; onChange: (id: number) => void; className?: string }) {
  const { lists, areas } = useApp()
  const groups = listOptions(lists, areas)
  return (
    <select className={className} value={value ?? ''} onChange={e => onChange(Number(e.target.value))}>
      {value === null && <option value="">Choose a list…</option>}
      {groups.map(g => g.name ? <optgroup key={g.name} label={g.name}>{g.lists.map(l => <option key={l.id} value={l.id}>{l.name}</option>)}</optgroup> : g.lists.map(l => <option key={l.id} value={l.id}>{l.name}</option>))}
    </select>
  )
}

export function useSections(listId: number | null) {
  const [sections, setSections] = useState<Section[]>([])
  const { version } = useApp()
  useEffect(() => {
    if (!listId) { setSections([]); return }
    let live = true
    api.get<Section[]>(`${V1}/lists/${listId}/sections`).then(s => live && setSections(s || [])).catch(() => {})
    return () => { live = false }
  }, [listId, version])
  return sections
}

export function SectionSelect({ listId, value, onChange, className = '' }: { listId: number | null; value: number | null; onChange: (id: number | null) => void; className?: string }) {
  const sections = useSections(listId)
  if (!sections.length) return null
  return (
    <select className={className} value={value ?? ''} onChange={e => onChange(e.target.value ? Number(e.target.value) : null)}>
      <option value="">No section</option>
      {sections.map(s => <option key={s.id} value={s.id}>{s.name}</option>)}
    </select>
  )
}

export function PrioritySelect({ value, onChange, size = 'sm' }: { value: number; onChange: (p: number) => void; size?: '' | 'sm' }) {
  return <Segmented size={size} value={priorityLevel(value)} onChange={v => onChange(PRIORITY_VALUE[v])} options={[{ value: 'none', label: 'None' }, { value: 'low', label: '! Low' }, { value: 'medium', label: '!! Medium' }, { value: 'high', label: '!!! High' }]} />
}

export function RepeatEditor({ value, onChange }: { value: Repeat | null; onChange: (r: Repeat | null) => void }) {
  const preset = REPEAT_PRESETS.find(p => p.rrule === value?.rrule)
  const [custom, setCustom] = useState(!!value && !preset)
  const [text, setText] = useState(value?.rrule || '')
  useEffect(() => { setText(value?.rrule || ''); if (value && !REPEAT_PRESETS.some(p => p.rrule === value.rrule)) setCustom(true) }, [value?.rrule]) // eslint-disable-line react-hooks/exhaustive-deps
  const from = value?.from || 'due'
  return (
    <div className="grid" style={{ gap: 8 }}>
      <div className="row wrap">
        <button type="button" className={`chip ${!value ? 'active' : ''}`} onClick={() => { onChange(null); setCustom(false) }}>Never</button>
        {REPEAT_PRESETS.map(p => <button key={p.rrule} type="button" className={`chip ${value?.rrule === p.rrule && !custom ? 'active' : ''}`} onClick={() => { onChange({ rrule: p.rrule, from }); setCustom(false) }}>{p.label}</button>)}
        <button type="button" className={`chip ${custom ? 'active' : ''}`} onClick={() => setCustom(true)}>Custom…</button>
      </div>
      {custom && <input className="mono" value={text} placeholder="FREQ=WEEKLY;INTERVAL=2;BYDAY=MO" onChange={e => setText(e.target.value)} onBlur={() => { const t = text.trim().replace(/^RRULE:/i, ''); if (t) onChange({ rrule: t, from }); else onChange(null) }} />}
      {value && <Segmented size="sm" value={from} onChange={v => onChange({ ...value, from: v })} options={[{ value: 'due', label: 'From the due date' }, { value: 'completion', label: 'After I finish it' }]} />}
    </div>
  )
}

export function TagInput({ value, onChange, autoFocus }: { value: string[]; onChange: (t: string[]) => void; autoFocus?: boolean }) {
  const { tags } = useApp()
  const [text, setText] = useState('')
  const [sel, setSel] = useState(0)
  const q = text.trim().toLowerCase().replace(/^#/, '')
  const hints = useMemo(() => tags.filter(t => !value.includes(t.tag) && (!q || t.tag.includes(q))).slice(0, 8), [tags, value, q])
  const add = (t: string) => { const n = t.trim().toLowerCase().replace(/^#/, ''); if (n && !value.includes(n)) onChange([...value, n]); setText(''); setSel(0) }
  const key = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if ((e.key === 'Enter' || e.key === ',' || e.key === 'Tab') && (text.trim() || hints.length)) { e.preventDefault(); add(q && hints[sel] && hints[sel].tag.startsWith(q) ? hints[sel].tag : text) }
    else if (e.key === 'Backspace' && !text && value.length) onChange(value.slice(0, -1))
    else if (e.key === 'ArrowDown') { e.preventDefault(); setSel(s => Math.min(hints.length - 1, s + 1)) }
    else if (e.key === 'ArrowUp') { e.preventDefault(); setSel(s => Math.max(0, s - 1)) }
  }
  return (
    <div className="tag-input">
      <div className="tag-input-row">
        {value.map(t => <span key={t} className="tag" style={{ ['--tag' as string]: tint(t) }}>#{t}<button type="button" onClick={() => onChange(value.filter(x => x !== t))} aria-label={`Remove ${t}`}>✕</button></span>)}
        <input value={text} onChange={e => { setText(e.target.value); setSel(0) }} onKeyDown={key} onBlur={() => { if (text.trim()) add(text) }} placeholder={value.length ? '' : 'Add a tag…'} autoFocus={autoFocus} />
      </div>
      {text && hints.length > 0 && <div className="tag-hints">{hints.map((h, i) => <button type="button" key={h.tag} className={i === sel ? 'sel' : ''} onMouseDown={e => { e.preventDefault(); add(h.tag) }}>#{h.tag} <small className="muted">{h.count}</small></button>)}</div>}
    </div>
  )
}

const ALARM_PRESETS: { label: string; trigger: string }[] = [
  { label: 'At the time', trigger: 'PT0S' },
  { label: '15 min before', trigger: '-PT15M' },
  { label: '1 hour before', trigger: '-PT1H' },
  { label: '1 day before, 09:00', trigger: '-P1D' },
]

export function AlarmEditor({ value, onChange, timed }: { value: Alarm[]; onChange: (a: Alarm[]) => void; timed: boolean }) {
  const [custom, setCustom] = useState('')
  const add = (trigger: string) => { if (!trigger || value.some(a => a.trigger === trigger)) return; onChange([...value, { trigger }]) }
  return (
    <div className="grid" style={{ gap: 6 }}>
      {value.map((a, i) => <div key={i} className="row small"><span className="grow">⏰ {alarmLabel(a.trigger)}{a.description ? ` · ${a.description}` : ''}</span><button type="button" className="btn ghost xs" onClick={() => onChange(value.filter((_, j) => j !== i))}>✕</button></div>)}
      <div className="row wrap">
        {ALARM_PRESETS.filter(p => timed || p.trigger === '-P1D' || p.trigger === 'PT0S').map(p => <button key={p.trigger} type="button" className="chip" onClick={() => add(p.trigger)}>{p.label}</button>)}
        <Dropdown className="chip" label="Custom…">{close => <div className="grid" style={{ gap: 8, minWidth: 240 }}>
          <input className="mono" placeholder="-PT30M, -P2D or 2026-10-21T08:00:00Z" value={custom} onChange={e => setCustom(e.target.value)} />
          <div className="row" style={{ justifyContent: 'flex-end' }}><button type="button" className="btn sm primary" onClick={() => { add(custom.trim()); setCustom(''); close() }}>Add</button></div>
        </div>}</Dropdown>
      </div>
      {!timed && value.length > 0 && <small className="muted">All-day: reminders use the day start from Settings.</small>}
    </div>
  )
}

export function ColorPicker({ value, onChange }: { value: string; onChange: (c: string) => void }) {
  return (
    <div className="row wrap">
      {PALETTE.map(c => <button type="button" key={c} className={`color-dot ${value === c ? 'active' : ''}`} style={{ background: c }} onClick={() => onChange(c)} aria-label={c} />)}
      <input type="color" value={value || '#2563eb'} onChange={e => onChange(e.target.value)} style={{ width: 34, height: 26, padding: 0 }} aria-label="Any colour" />
    </div>
  )
}

export function EstimateInput({ value, onChange }: { value: number | null; onChange: (v: number | null) => void }) {
  return (
    <div className="row">
      <input type="number" min={0} step={5} value={value ?? ''} placeholder="min" onChange={e => onChange(e.target.value ? Number(e.target.value) : null)} style={{ width: 90 }} />
      <div className="row wrap">{[15, 30, 60, 120].map(m => <button key={m} type="button" className={`chip ${value === m ? 'active' : ''}`} onClick={() => onChange(m)}>{m < 60 ? `${m} min` : `${m / 60} h`}</button>)}</div>
    </div>
  )
}

export const timedValue = (s: string | null) => hasTime(s)
