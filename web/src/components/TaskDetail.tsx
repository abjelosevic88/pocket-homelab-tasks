import React, { useCallback, useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, upload, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Activity, Task } from '../lib/types'
import { emitTask } from '../lib/events'
import { hasTime, label, rruleLabel } from '../lib/dates'
import { ago, bytes, stamp } from '../lib/format'
import { Markdown, toggleCheckbox } from '../lib/markdown'
import { Confirm, Loading, useToast } from './ui'
import { AlarmEditor, DateButton, Dropdown, EstimateInput, ListSelect, PrioritySelect, RepeatEditor, SectionSelect, TagInput } from './pickers'

type Save = 'idle' | 'dirty' | 'saving' | 'saved' | 'error'

export function TaskDetail({ id, onClose }: { id: number; onClose: () => void }) {
  const { bump, lists } = useApp()
  const { toast } = useToast()
  const navigate = useNavigate()
  const [t, setT] = useState<Task | null>(null)
  const [err, setErr] = useState('')
  const [save, setSave] = useState<Save>('idle')
  const [notesEdit, setNotesEdit] = useState(false)
  const [notes, setNotes] = useState('')
  const [sub, setSub] = useState('')
  const [activityOpen, setActivityOpen] = useState(false)
  const [del, setDel] = useState(false)
  const [dropping, setDropping] = useState(false)
  const pending = useRef<Record<string, unknown>>({})
  const timer = useRef<number | null>(null)
  const fileRef = useRef<HTMLInputElement>(null)

  const load = useCallback(() => api.get<Task>(`${V1}/tasks/${id}`).then(x => { setT(x); setNotes(x.notes); setErr('') }).catch(e => setErr((e as Error).message)), [id])
  useEffect(() => { setT(null); setNotesEdit(false); setActivityOpen(false); pending.current = {}; load() }, [load])

  const flush = useCallback(async () => {
    const body = pending.current
    pending.current = {}
    if (!Object.keys(body).length) return
    setSave('saving')
    try { const x = await api.put<Task>(`${V1}/tasks/${id}`, body); setT(prev => prev ? { ...x, activity: prev.activity, subtasks: x.subtasks ?? prev.subtasks } : x); emitTask(x); setSave('saved'); window.setTimeout(() => setSave(s => s === 'saved' ? 'idle' : s), 1800) } catch (e) { setSave('error'); toast((e as Error).message, 'err') }
  }, [id, toast])
  // Text fields wait a moment; everything else saves at once.
  const patch = (fields: Partial<Task>, debounce = 0) => {
    setT(prev => prev ? { ...prev, ...fields } : prev)
    pending.current = { ...pending.current, ...fields }
    setSave('dirty')
    if (timer.current) window.clearTimeout(timer.current)
    if (debounce) timer.current = window.setTimeout(flush, debounce); else flush()
  }
  useEffect(() => () => { if (timer.current) { window.clearTimeout(timer.current); flush() } }, [flush])

  const structural = async (fn: () => Promise<unknown>, msg?: string) => { try { await fn(); if (msg) toast(msg); await load(); bump() } catch (e) { toast((e as Error).message, 'err') } }
  const complete = () => structural(async () => { const r = await api.post<{ task: Task; next?: Task }>(`${V1}/tasks/${id}/complete`, { done: !(t!.status === 'done' || t!.status === 'cancelled') }); if (r.next) toast(`Repeats: next on ${label(r.next.start || r.next.due)}`) })
  const addSub = async (e: React.FormEvent) => { e.preventDefault(); if (!sub.trim() || !t) return; const title = sub.trim(); setSub(''); structural(() => api.post(`${V1}/tasks`, { title, parent_id: t.id, list_id: t.list_id, section_id: t.section_id })) }
  const files = async (fs: File[]) => { if (!fs.length) return; try { const x = await upload<Task>(`${V1}/tasks/${id}/attachments`, 'files', fs); setT(prev => prev ? { ...prev, attachments: x.attachments } : x); emitTask(x); toast(fs.length === 1 ? 'Attached' : `${fs.length} files attached`) } catch (e) { toast((e as Error).message, 'err') } }

  if (err) return <aside className="task-detail"><div className="detail-head"><button className="btn ghost icon" onClick={onClose} aria-label="Close">✕</button></div><div className="callout danger m">{err}</div></aside>
  if (!t) return <aside className="task-detail"><div className="detail-head"><button className="btn ghost icon" onClick={onClose} aria-label="Close">✕</button></div><Loading /></aside>
  const done = t.status === 'done' || t.status === 'cancelled'
  const timed = hasTime(t.start) || hasTime(t.due)
  const list = lists.find(l => l.id === t.list_id)
  const subs = t.subtasks || []

  return (
    <aside className="task-detail" onKeyDown={e => { if (e.key === 'Escape' && !(e.target as HTMLElement).closest('.menu, .modal')) { (e.target as HTMLElement).blur?.(); onClose() } }}>
      <div className="detail-head">
        <button type="button" className={`check lg ${done ? 'on' : ''}`} onClick={complete} aria-label={done ? 'Reopen' : 'Complete'}>{done ? '✓' : ''}</button>
        <input className="detail-title" value={t.title} onChange={e => patch({ title: e.target.value }, 600)} onBlur={() => { if (timer.current) { window.clearTimeout(timer.current); flush() } }} placeholder="Task title" />
        <span className={`save-state ${save}`}>{save === 'saved' ? '✓ Saved' : save === 'saving' ? 'Saving…' : save === 'error' ? 'Not saved' : ''}</span>
        <button className="btn ghost icon" onClick={onClose} aria-label="Close">✕</button>
      </div>
      <div className="detail-body">
        <div className="detail-section">
          {notesEdit ? <textarea autoFocus className="notes-edit" rows={Math.max(4, notes.split('\n').length + 1)} value={notes} onChange={e => { setNotes(e.target.value); patch({ notes: e.target.value }, 900) }} onBlur={() => setNotesEdit(false)} placeholder="Notes (Markdown)" />
            : <div className={`notes-view ${t.notes ? '' : 'empty'}`} onClick={e => { if ((e.target as HTMLElement).tagName === 'A' || (e.target as HTMLElement).tagName === 'INPUT') return; setNotesEdit(true) }}>{t.notes ? <Markdown text={t.notes} onToggle={line => { const n = toggleCheckbox(t.notes, line); setNotes(n); patch({ notes: n }) }} /> : <span className="muted">Add notes…</span>}</div>}
        </div>

        <div className="props">
          <div className="prop"><span className="prop-label">List</span><span className="row wrap"><ListSelect className="xs" value={t.list_id} onChange={id => structural(() => api.post(`${V1}/tasks/${t.id}/move`, { list_id: id, section_id: null }))} /><SectionSelect className="xs" listId={t.list_id} value={t.section_id} onChange={sid => structural(() => api.post(`${V1}/tasks/${t.id}/move`, { list_id: t.list_id, section_id: sid }))} /></span></div>
          <div className="prop"><span className="prop-label">When</span><span className="row wrap"><DateButton kind="start" value={t.start} onChange={v => patch({ start: v })} placeholder="Someday / anytime" />{t.start && <span className="muted small">{label(t.start)}</span>}</span></div>
          <div className="prop"><span className="prop-label">Deadline</span><span className="row wrap"><DateButton kind="due" value={t.due} onChange={v => patch({ due: v })} placeholder="No deadline" />{t.overdue && <span className="badge danger">Overdue</span>}</span></div>
          <div className="prop"><span className="prop-label">Estimate</span><EstimateInput value={t.estimate_min} onChange={v => patch({ estimate_min: v })} /></div>
          <div className="prop"><span className="prop-label">Repeat</span><Dropdown className={`btn sm chipbtn ${t.repeat ? 'set' : ''}`} label={t.repeat ? `↻ ${rruleLabel(t.repeat.rrule)}${t.repeat.from === 'completion' ? ' · after done' : ''}` : 'Never'}>{() => <div style={{ minWidth: 300 }}><RepeatEditor value={t.repeat} onChange={r => patch({ repeat: r })} /></div>}</Dropdown></div>
          <div className="prop"><span className="prop-label">Priority</span><PrioritySelect value={t.priority} onChange={p => patch({ priority: p })} /></div>
          <div className="prop"><span className="prop-label">Tags</span><TagInput value={t.tags} onChange={tags => patch({ tags })} /></div>
          <div className="prop"><span className="prop-label">Alarms</span><AlarmEditor value={t.alarms || []} onChange={alarms => patch({ alarms })} timed={timed} /></div>
          <div className="prop"><span className="prop-label">Someday</span><label className="switch"><input type="checkbox" checked={t.someday} onChange={e => patch({ someday: e.target.checked })} /><span>{t.someday ? 'On the someday shelf' : 'No'}</span></label></div>
          <div className="prop"><span className="prop-label">Location</span><input className="xs" value={t.location} onChange={e => patch({ location: e.target.value }, 700)} placeholder="Where" /></div>
          <div className="prop"><span className="prop-label">URL</span><span className="row"><input className="xs" value={t.url} onChange={e => patch({ url: e.target.value }, 700)} placeholder="https://" spellCheck={false} />{t.url && <a className="btn sm" href={t.url} target="_blank" rel="noreferrer">Open</a>}</span></div>
        </div>

        <div className="detail-section">
          <h4>Subtasks {subs.length > 0 && <small className="muted">{subs.filter(s => s.status === 'done').length}/{subs.length}</small>}</h4>
          <div className="subtasks">
            {subs.map((s, i) => <SubtaskRow key={s.id} s={s} first={i === 0} last={i === subs.length - 1} prev={subs[i - 1]} next={subs[i + 1]} onChange={structural} />)}
            <form onSubmit={addSub} className="row"><span className="plus muted">＋</span><input value={sub} onChange={e => setSub(e.target.value)} placeholder="Add a subtask…" className="xs" /></form>
          </div>
        </div>

        <div className="detail-section">
          <h4>Attachments</h4>
          <div className={`dropzone ${dropping ? 'on' : ''}`} onDragOver={e => { e.preventDefault(); setDropping(true) }} onDragLeave={() => setDropping(false)} onDrop={e => { e.preventDefault(); setDropping(false); files(Array.from(e.dataTransfer.files)) }} onClick={() => fileRef.current?.click()}>
            Drop files here or <button type="button" className="link-btn" style={{ color: 'var(--accent-hover)' }}>choose…</button>
            <input ref={fileRef} type="file" multiple hidden onChange={e => { const f = Array.from(e.target.files || []); e.target.value = ''; files(f) }} />
          </div>
          {t.attachments?.length > 0 && <ul className="attachments">{t.attachments.map(a => <li key={a.id}><a href={a.url || `${V1}/tasks/${t.id}/attachments/${a.id}`} target="_blank" rel="noreferrer">📎 {a.name}</a><small className="muted">{bytes(a.size)}</small><button className="btn ghost xs" aria-label="Remove" onClick={() => structural(() => api.del(`${V1}/tasks/${t.id}/attachments/${a.id}`))}>✕</button></li>)}</ul>}
        </div>

        <div className="detail-section">
          <button className="side-head as-btn" style={{ padding: '4px 0' }} onClick={() => setActivityOpen(o => !o)}><span>Activity</span><span className="muted">{activityOpen ? '▾' : '▸'}</span></button>
          {activityOpen && <ul className="activity">{(t.activity || []).slice(0, 20).map((a: Activity) => <li key={a.id}><span className="muted small" title={stamp(a.at)}>{ago(a.at)}</span> <span>{a.kind}</span>{a.detail && typeof a.detail === 'object' && Object.keys(a.detail).length > 0 && <small className="muted"> · {Object.entries(a.detail).map(([k, v]) => `${k}: ${typeof v === 'object' ? JSON.stringify(v) : String(v)}`).join(', ')}</small>}{typeof a.detail === 'string' && a.detail && <small className="muted"> · {a.detail}</small>}</li>)}{!(t.activity || []).length && <li className="muted small">Nothing yet.</li>}</ul>}
        </div>
      </div>
      <div className="detail-foot">
        <span className="muted small">Created {ago(t.created_at)} · edited {ago(t.updated_at)}{list ? ` · ${list.name}` : ''}</span>
        <span className="spacer" />
        <a className="btn ghost sm" href={`${V1}/tasks/${t.id}/ics`} target="_blank" rel="noreferrer" title="The raw iCalendar">Open .ics</a>
        <button className="btn ghost sm" onClick={async () => { try { const x = await api.post<Task>(`${V1}/tasks/${t.id}/duplicate`); bump(); toast('Duplicated'); navigate({ search: `?task=${x.id}` }) } catch (e) { toast((e as Error).message, 'err') } }}>Duplicate</button>
        <button className="btn ghost sm danger-text" onClick={() => setDel(true)}>Trash</button>
      </div>
      {del && <Confirm title="Move this task to the trash?" message="Its subtasks go with it. It can be restored from the trash." onCancel={() => setDel(false)} onConfirm={async () => { setDel(false); try { await api.del(`${V1}/tasks/${t.id}`); bump(); toast('Moved to the trash'); onClose() } catch (e) { toast((e as Error).message, 'err') } }} />}
    </aside>
  )
}

function SubtaskRow({ s, first, last, prev, next, onChange }: { s: Task; first: boolean; last: boolean; prev?: Task; next?: Task; onChange: (fn: () => Promise<unknown>) => Promise<void> }) {
  const [title, setTitle] = useState(s.title)
  useEffect(() => setTitle(s.title), [s.title])
  const done = s.status === 'done' || s.status === 'cancelled'
  return (
    <div className={`subtask ${done ? 'done' : ''}`}>
      <button type="button" className={`check ${done ? 'on' : ''}`} onClick={() => onChange(() => api.post(`${V1}/tasks/${s.id}/complete`, { done: !done }))} aria-label={done ? 'Reopen' : 'Complete'}>{done ? '✓' : ''}</button>
      <input className="xs plain" value={title} onChange={e => setTitle(e.target.value)} onBlur={() => { if (title.trim() && title !== s.title) onChange(() => api.put(`${V1}/tasks/${s.id}`, { title: title.trim() })) }} onKeyDown={e => { if (e.key === 'Enter') (e.target as HTMLInputElement).blur() }} />
      <span className="subtask-tools">
        <button type="button" className="btn ghost xs" disabled={first} onClick={() => onChange(() => api.post(`${V1}/tasks/${s.id}/move`, { parent_id: s.parent_id, before_id: prev!.id }))} aria-label="Move up">↑</button>
        <button type="button" className="btn ghost xs" disabled={last} onClick={() => onChange(() => api.post(`${V1}/tasks/${s.id}/move`, { parent_id: s.parent_id, after_id: next!.id }))} aria-label="Move down">↓</button>
        <button type="button" className="btn ghost xs" onClick={() => onChange(() => api.del(`${V1}/tasks/${s.id}`))} aria-label="Delete">✕</button>
      </span>
    </div>
  )
}
