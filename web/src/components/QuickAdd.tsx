import React, { useEffect, useRef, useState } from 'react'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Alarm, Parsed, QuickAnswer, Repeat, Task } from '../lib/types'
import { label, rruleLabel, alarmLabel } from '../lib/dates'
import { PRIORITY_MARK, priorityLevel } from '../lib/format'
import { useToast } from './ui'
import { AlarmEditor, DateButton, ListSelect, PrioritySelect, RepeatEditor, SectionSelect, TagInput } from './pickers'

type Part = 'due' | 'start' | 'list' | 'tags' | 'priority' | 'repeat' | 'alarm'

export interface QuickDefaults { list_id?: number | null; section_id?: number | null; start?: string | null; due?: string | null; parent_id?: number | null; someday?: boolean }

/** One line of plain words; parsed by the server as you type, chips show what it understood. */
export function QuickAdd({ defaults = {}, placeholder = 'Add a task… e.g. "Call the bank tomorrow 9am !high #money /Home every month"', onAdded, autoFocus, compact, inputRef: outerRef, onCancel }: { defaults?: QuickDefaults; placeholder?: string; onAdded?: (t: Task) => void; autoFocus?: boolean; compact?: boolean; inputRef?: React.RefObject<HTMLInputElement | null>; onCancel?: () => void }) {
  const { settings, lists, bump } = useApp()
  const { toast } = useToast()
  const [text, setText] = useState('')
  const [parsed, setParsed] = useState<Parsed | null>(null)
  const [removed, setRemoved] = useState<Set<Part>>(new Set())
  const [expanded, setExpanded] = useState(false)
  const [busy, setBusy] = useState(false)
  const [x, setX] = useState({ notes: '', list_id: null as number | null, section_id: null as number | null, start: null as string | null, due: null as string | null, repeat: null as Repeat | null, priority: 0, tags: [] as string[], alarms: [] as Alarm[] })
  const innerRef = useRef<HTMLInputElement>(null)
  const ref = outerRef || innerRef
  const listId = defaults.list_id ?? null
  const seq = useRef(0)

  useEffect(() => {
    const t = text.trim()
    if (!t) { setParsed(null); return }
    const n = ++seq.current
    const h = setTimeout(() => {
      api.post<QuickAnswer>(`${V1}/tasks/quick`, { text: t, list_id: listId || undefined, commit: false })
        .then(r => { if (n === seq.current) setParsed(r.parsed) }).catch(() => {})
    }, 250)
    return () => clearTimeout(h)
  }, [text, listId])

  const chips: { part: Part; text: string }[] = []
  if (parsed) {
    if (parsed.start && !removed.has('start')) chips.push({ part: 'start', text: `▸ ${label(parsed.start)}` })
    if (parsed.due && !removed.has('due')) chips.push({ part: 'due', text: `⚑ ${label(parsed.due)}` })
    if (parsed.list_id && parsed.list_id !== listId && !removed.has('list')) { const l = lists.find(l => l.id === parsed.list_id); if (l) chips.push({ part: 'list', text: `▤ ${l.name}` }) }
    if (parsed.tags?.length && !removed.has('tags')) chips.push({ part: 'tags', text: parsed.tags.map(t => `#${t}`).join(' ') })
    if (parsed.priority && !removed.has('priority')) chips.push({ part: 'priority', text: `${PRIORITY_MARK[priorityLevel(parsed.priority)]} ${priorityLevel(parsed.priority)}` })
    if (parsed.repeat && !removed.has('repeat')) chips.push({ part: 'repeat', text: `↻ ${rruleLabel(parsed.repeat.rrule)}` })
    if (parsed.alarm && !removed.has('alarm')) chips.push({ part: 'alarm', text: `⏰ ${alarmLabel(parsed.alarm)}` })
  }

  const body = (): Record<string, unknown> => {
    const p = parsed
    const keep = (k: Part) => p && !removed.has(k)
    const b: Record<string, unknown> = {
      title: (expanded || removed.size) && p ? p.title : text.trim(),
      list_id: expanded && x.list_id ? x.list_id : keep('list') && p!.list_id ? p!.list_id : listId || settings?.default_list_id,
      section_id: expanded ? x.section_id : defaults.section_id ?? undefined,
      parent_id: defaults.parent_id ?? undefined,
      start: expanded ? x.start : keep('start') ? p!.start : defaults.start ?? undefined,
      due: expanded ? x.due : keep('due') ? p!.due : defaults.due ?? undefined,
      priority: expanded ? x.priority : keep('priority') ? p!.priority : undefined,
      tags: expanded ? x.tags : keep('tags') ? p!.tags : undefined,
      repeat: expanded ? x.repeat : keep('repeat') ? p!.repeat : undefined,
      alarms: expanded ? x.alarms : keep('alarm') && p!.alarm ? [{ trigger: p!.alarm }] : undefined,
      notes: expanded ? x.notes : undefined,
      someday: defaults.someday || undefined,
    }
    for (const k of Object.keys(b)) if (b[k] === undefined) delete b[k]
    return b
  }

  const submit = async () => {
    if (!text.trim() || busy) return
    setBusy(true)
    try {
      let t: Task
      const plain = !expanded && !removed.size && !defaults.section_id && !defaults.parent_id && !defaults.start && !defaults.due && !defaults.someday
      if (plain) t = (await api.post<QuickAnswer>(`${V1}/tasks/quick`, { text: text.trim(), list_id: listId || undefined, commit: true })).task
      else t = await api.post<Task>(`${V1}/tasks`, body())
      setText(''); setParsed(null); setRemoved(new Set()); setExpanded(false)
      setX(v => ({ ...v, notes: '', start: null, due: null, repeat: null, priority: 0, tags: [], alarms: [] }))
      onAdded?.(t); bump()
      ref.current?.focus()
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }

  const expand = () => {
    const p = parsed
    setX({ notes: x.notes, list_id: (p?.list_id && !removed.has('list') ? p.list_id : null) ?? listId ?? settings?.default_list_id ?? null, section_id: defaults.section_id ?? null, start: p?.start && !removed.has('start') ? p.start : defaults.start ?? null, due: p?.due && !removed.has('due') ? p.due : defaults.due ?? null, repeat: p?.repeat && !removed.has('repeat') ? p.repeat : null, priority: p?.priority && !removed.has('priority') ? p.priority : 0, tags: p?.tags && !removed.has('tags') ? p.tags : [], alarms: p?.alarm && !removed.has('alarm') ? [{ trigger: p.alarm }] : [] })
    setExpanded(true)
  }

  const key = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter') { e.preventDefault(); if ((e.metaKey || e.ctrlKey) && !expanded) expand(); else submit() }
    else if (e.key === 'Escape') { if (expanded) setExpanded(false); else if (text) { setText(''); setParsed(null) } else { ref.current?.blur(); onCancel?.() } }
  }

  return (
    <div className={`quick-add ${compact ? 'compact' : ''} ${expanded ? 'expanded' : ''}`} onKeyDown={e => e.stopPropagation()}>
      <div className="quick-row">
        <span className="plus">＋</span>
        <input ref={ref} value={text} onChange={e => setText(e.target.value)} onKeyDown={key} placeholder={placeholder} autoFocus={autoFocus} aria-label="Add a task" />
        {text && <button type="button" className="btn ghost xs" title={expanded ? 'Fewer fields' : 'More fields (⌘↵)'} onClick={() => expanded ? setExpanded(false) : expand()}>{expanded ? '⌃' : '⌄'}</button>}
        {text && <button type="button" className="btn primary sm" onClick={submit} disabled={busy}>Add</button>}
      </div>
      {!expanded && chips.length > 0 && <div className="quick-chips">
        {parsed && parsed.title !== text.trim() && <span className="muted small">“{parsed.title}”</span>}
        {chips.map(c => <button type="button" key={c.part} className="chip active" title="Click to drop this part" onClick={() => setRemoved(s => new Set([...s, c.part]))}>{c.text} ✕</button>)}
      </div>}
      {expanded && <div className="quick-more">
        <textarea rows={2} placeholder="Notes (Markdown)" value={x.notes} onChange={e => setX({ ...x, notes: e.target.value })} />
        <div className="row wrap">
          <ListSelect className="xs" value={x.list_id} onChange={id => setX({ ...x, list_id: id, section_id: null })} />
          <SectionSelect className="xs" listId={x.list_id} value={x.section_id} onChange={id => setX({ ...x, section_id: id })} />
          <DateButton kind="start" value={x.start} onChange={v => setX({ ...x, start: v })} placeholder="When" />
          <DateButton kind="due" value={x.due} onChange={v => setX({ ...x, due: v })} placeholder="Deadline" />
        </div>
        <div className="row wrap"><span className="muted small" style={{ width: 60 }}>Priority</span><PrioritySelect value={x.priority} onChange={p => setX({ ...x, priority: p })} /></div>
        <div className="row wrap" style={{ alignItems: 'flex-start' }}><span className="muted small" style={{ width: 60, paddingTop: 6 }}>Repeat</span><div className="grow"><RepeatEditor value={x.repeat} onChange={r => setX({ ...x, repeat: r })} /></div></div>
        <div className="row wrap" style={{ alignItems: 'flex-start' }}><span className="muted small" style={{ width: 60, paddingTop: 6 }}>Tags</span><div className="grow"><TagInput value={x.tags} onChange={t => setX({ ...x, tags: t })} /></div></div>
        <div className="row wrap" style={{ alignItems: 'flex-start' }}><span className="muted small" style={{ width: 60, paddingTop: 6 }}>Alarm</span><div className="grow"><AlarmEditor value={x.alarms} onChange={a => setX({ ...x, alarms: a })} timed={!!(x.start && x.start.length > 10) || !!(x.due && x.due.length > 10)} /></div></div>
        <div className="row" style={{ justifyContent: 'flex-end' }}><button type="button" className="btn sm" onClick={() => setExpanded(false)}>Cancel</button><button type="button" className="btn primary sm" onClick={submit} disabled={busy || !text.trim()}>Add task</button></div>
      </div>}
    </div>
  )
}
