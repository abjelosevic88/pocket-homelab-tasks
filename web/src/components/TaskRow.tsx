import React from 'react'
import type { Task } from '../lib/types'
import { priorityLevel, tint } from '../lib/format'
import { dayOf, fmtTime, label, today } from '../lib/dates'
import { Icon, ListRing } from './icons'

export function DueChip({ task }: { task: Task }) {
  if (!task.due) return null
  const d = dayOf(task.due), t = today()
  const cls = task.status === 'done' || task.status === 'cancelled' ? '' : d < t ? 'overdue' : d === t ? 'today' : ''
  return <span className={`meta-chip due ${cls}`} title={`Deadline ${task.due}`}><Icon name="flag" size={12} />{label(task.due)}</span>
}

export function TaskRow({ task, selected, focused, showList, onToggle, onOpen, onSelect, onPointerDown, dragging, depth = 0, showDone }: {
  task: Task; selected?: boolean; focused?: boolean; showList?: boolean; depth?: number; dragging?: boolean; showDone?: boolean
  onToggle: (t: Task) => void; onOpen: (t: Task) => void; onSelect?: (t: Task, e: React.MouseEvent) => void; onPointerDown?: (t: Task, e: React.PointerEvent) => void
}) {
  const done = task.status === 'done' || task.status === 'cancelled'
  const pr = priorityLevel(task.priority)
  const startDiffers = task.start && (!task.due || dayOf(task.start) !== dayOf(task.due) || fmtTime(task.start) !== fmtTime(task.due))
  const startChip = task.start && (startDiffers || !task.due)
  const startToday = !!task.start && dayOf(task.start) === today()
  const meta: React.ReactNode[] = []
  const hints: React.ReactNode[] = []
  if (showList && task.list) meta.push(<span key="l" className="meta-chip list"><ListRing color={task.list.color} size={11} />{task.list.name}</span>)
  if (startChip) meta.push(<span key="s" className={`meta-chip start ${startToday && !done ? 'today' : ''}`} title={`When ${task.start}`}><Icon name={startToday ? 'star' : 'calendar'} size={12} fill={startToday} stroke={startToday ? 1.4 : 1.9} />{label(task.start!)}</span>)
  if (task.due) meta.push(<DueChip key="d" task={task} />)
  if (task.repeat) hints.push(<span key="r" className="meta-ico" title={`Repeats: ${task.repeat.rrule}`}><Icon name="repeat" size={12} /></span>)
  if (task.someday) hints.push(<span key="sd" className="meta-ico" title="Someday"><Icon name="archive" size={12} /></span>)
  if (task.alarms?.length) hints.push(<span key="a" className="meta-ico" title={`${task.alarms.length} reminder${task.alarms.length > 1 ? 's' : ''}`}><Icon name="bell" size={12} /></span>)
  if (task.subtask_count > 0) meta.push(<span key="st" className={`meta-chip ${task.subtask_done === task.subtask_count ? 'all-done' : ''}`} title="Subtasks"><Icon name="subtasks" size={12} />{task.subtask_done}/{task.subtask_count}</span>)
  if (task.attachments?.length > 0) hints.push(<span key="at" className="meta-ico" title={`${task.attachments.length} attached`}><Icon name="clip" size={12} /></span>)
  if (task.notes) hints.push(<span key="n" className="meta-ico" title="Has notes"><Icon name="notes" size={12} /></span>)
  for (const t of task.tags) meta.push(<span key={'t' + t} className="tag-pill" style={{ ['--tag' as string]: tint(t) }}>{t}</span>)
  if (done && showDone && task.completed_at) meta.push(<span key="c" className="meta-chip">{new Date(task.completed_at).toLocaleTimeString(undefined, { hour: 'numeric', minute: '2-digit' })}</span>)

  return (
    <div
      className={`task-row ${done ? 'done' : ''} ${selected ? 'selected' : ''} ${focused ? 'focused' : ''} ${task.overdue ? 'overdue' : ''} ${dragging ? 'dragging' : ''}`}
      data-task-id={task.id}
      style={depth ? { paddingLeft: 12 + depth * 30 } : undefined}
      onClick={e => { if (onSelect && (e.shiftKey || e.metaKey || e.ctrlKey)) { e.preventDefault(); onSelect(task, e) } else onOpen(task) }}
      onPointerDown={e => onPointerDown?.(task, e)}
    >
      <button type="button" className={`check ${done ? 'on' : ''} ${pr}`} onClick={e => { e.stopPropagation(); onToggle(task) }} onPointerDown={e => e.stopPropagation()} aria-label={done ? 'Reopen' : 'Complete'}>
        <Icon name="check" size={11} stroke={3.2} />
      </button>
      <div className="task-main">
        <span className="task-title">{task.title || <span className="muted">Untitled</span>}{hints.length > 0 && <span className="title-icons">{hints}</span>}</span>
        {meta.length > 0 && <span className="task-meta">{meta}</span>}
      </div>
      {pr !== 'none' && !done && <span className={`prio-flag ${pr}`} title={`${pr[0].toUpperCase() + pr.slice(1)} priority`}><Icon name="flag" size={13} fill /></span>}
    </div>
  )
}
