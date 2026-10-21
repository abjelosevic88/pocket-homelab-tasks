import React, { useEffect, useMemo, useState } from 'react'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Alarm, List, Repeat, Section } from '../lib/types'
import { PALETTE, PRIORITY_VALUE, priorityLevel, tint } from '../lib/format'
import { Icon } from './icons'
import { REPEAT_PRESETS, alarmLabel, dayOf, hasTime, join, label, nextWeek, thisWeekend, timeOf, today, tomorrow } from '../lib/dates'
import { type Anchor, Popover, Segmented, rectOf } from './ui'
import { type Option, Select } from './Select'

/** A button with a panel under it (a bottom sheet on phones); closes on a click outside or Escape. */
export function Dropdown({ label, className = 'btn sm', title, width = 290, children }: { label: React.ReactNode; className?: string; title?: string; width?: number; children: (close: () => void) => React.ReactNode }) {
  const [anchor, setAnchor] = useState<Anchor | null>(null)
  const close = () => setAnchor(null)
  return (
    <>
      <button type="button" className={className} title={title} aria-haspopup="dialog" aria-expanded={!!anchor} onClick={e => { e.stopPropagation(); setAnchor(anchor ? null : rectOf(e.currentTarget)) }}>{label}</button>
      {anchor && <Popover anchor={anchor} onClose={close} prefer="below" width={width} className="dd-pop" label={typeof title === 'string' ? title : undefined}><div className="dd-body" onClick={e => e.stopPropagation()}>{children(close)}</div></Popover>}
    </>
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
        <input type="date" aria-label="Day" value={day} onChange={e => onChange(e.target.value ? join(e.target.value, time) : null)} />
        {allowTime && <input type="time" value={time} disabled={!day} onChange={e => onChange(join(day || today(), e.target.value))} style={{ width: 116 }} aria-label="Time" />}
      </div>
      <div className="row" style={{ justifyContent: 'space-between' }}>
        <button type="button" className="btn ghost sm danger-text" onClick={() => pick(null)} disabled={!value}><Icon name="x" size={13} />Clear</button>
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

/** Lists grouped by area, for the list pickers. */
export function listOptions(lists: List[], areas: { id: number; name: string }[]) {
  const live = lists.filter(l => !l.archived)
  const byArea = (id: number | null) => live.filter(l => l.area_id === id)
  const groups: { name: string; lists: List[] }[] = [{ name: '', lists: byArea(null) }, ...areas.map(a => ({ name: a.name, lists: byArea(a.id) }))].filter(g => g.lists.length)
  return groups
}

/** Every live list as an option, grouped by area. */
export function listOptionsFlat(lists: List[], areas: { id: number; name: string }[]): Option<number>[] {
  return listOptions(lists, areas).flatMap(g => g.lists.map(l => ({ value: l.id, label: l.name, color: l.color || undefined, group: g.name || (areas.length ? 'No area' : undefined) })))
}

export function ListSelect({ value, onChange, className = '' }: { value: number | null; onChange: (id: number) => void; className?: string }) {
  const { lists, areas } = useApp()
  const sm = className.includes('xs')
  return <Select<number> value={value ?? 0} options={listOptionsFlat(lists, areas)} onChange={onChange} placeholder="Choose a list…" label="List" size={sm ? 'sm' : ''} className={sm ? 'inline' : ''} width={sm ? 240 : 'trigger'} />
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
  const sm = className.includes('xs')
  return <Select<number> value={value ?? 0} options={[{ value: 0, label: 'No section' }, ...sections.map(s => ({ value: s.id, label: s.name }))]} onChange={v => onChange(v || null)} label="Section" size={sm ? 'sm' : ''} className={sm ? 'inline' : ''} width={sm ? 220 : 'trigger'} />
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
    <div className="stack tight">
      <div className="chips">
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
        {value.map(t => <span key={t} className="tag" style={{ ['--tag' as string]: tint(t) }}>#{t}<button type="button" onClick={() => onChange(value.filter(x => x !== t))} aria-label={`Remove ${t}`}><Icon name="x" size={11} strokeWidth={2.4} /></button></span>)}
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
    <div className="stack tight">
      {value.map((a, i) => <div key={i} className="row small alarm-row"><Icon name="bell" size={13} className="muted" /><span className="grow">{alarmLabel(a.trigger)}{a.description ? ` · ${a.description}` : ''}</span><button type="button" className="btn ghost icon-only xs" aria-label="Remove this reminder" onClick={() => onChange(value.filter((_, j) => j !== i))}><Icon name="x" size={13} /></button></div>)}
      <div className="chips">
        {ALARM_PRESETS.filter(p => timed || p.trigger === '-P1D' || p.trigger === 'PT0S').map(p => <button key={p.trigger} type="button" className="chip" onClick={() => add(p.trigger)}>{p.label}</button>)}
        <Dropdown className="chip" label="Custom…" title="A custom reminder" width={300}>{close => <div className="stack tight">
          <input className="mono" placeholder="-PT30M, -P2D or 2026-10-21T08:00:00Z" value={custom} onChange={e => setCustom(e.target.value)} />
          <div className="form-actions"><button type="button" className="btn sm primary" onClick={() => { add(custom.trim()); setCustom(''); close() }}>Add</button></div>
        </div>}</Dropdown>
      </div>
      {!timed && value.length > 0 && <small className="muted">All-day: reminders use the day start from Settings.</small>}
    </div>
  )
}

export function ColorPicker({ value, onChange }: { value: string; onChange: (c: string) => void }) {
  const custom = !!value && !PALETTE.includes(value)
  return (
    <div className="palette">
      {PALETTE.map(c => <button type="button" key={c} className={`swatch ${value === c ? 'active' : ''}`} style={{ background: c }} onClick={() => onChange(c)} aria-label={c} aria-pressed={value === c}>{value === c && <Icon name="check" size={13} strokeWidth={2.6} style={{ color: '#fff' }} />}</button>)}
      <label className={`swatch custom ${custom ? 'active' : ''}`} style={custom ? { background: value, borderStyle: 'solid' } : undefined} title="Any colour"><Icon name="plus" size={13} /><input type="color" value={value || '#2563eb'} onChange={e => onChange(e.target.value)} aria-label="Any colour" /></label>
    </div>
  )
}

export function EstimateInput({ value, onChange }: { value: number | null; onChange: (v: number | null) => void }) {
  return (
    <div className="row">
      <input type="number" min={0} step={5} value={value ?? ''} placeholder="min" onChange={e => onChange(e.target.value ? Number(e.target.value) : null)} style={{ width: 84 }} className="num sm" aria-label="Estimate in minutes" />
      <div className="chips">{[15, 30, 60, 120].map(m => <button key={m} type="button" className={`chip ${value === m ? 'active' : ''}`} onClick={() => onChange(m)}>{m < 60 ? `${m} min` : `${m / 60} h`}</button>)}</div>
    </div>
  )
}

export const timedValue = (s: string | null) => hasTime(s)
