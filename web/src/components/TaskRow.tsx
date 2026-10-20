import React from 'react'
import type { Task } from '../lib/types'
import { PRIORITY_MARK, priorityLevel, tint } from '../lib/format'
import { dayOf, fmtTime, label, today } from '../lib/dates'

export function DueChip({ task }: { task: Task }) {
  if (!task.due) return null
  const d = dayOf(task.due), t = today()
  const cls = task.status === 'done' || task.status === 'cancelled' ? '' : d < t ? 'overdue' : d === t ? 'today' : ''
  return <span className={`due ${cls}`} title={`Deadline ${task.due}`}>⚑ {label(task.due)}</span>
}

export function TaskRow({ task, selected, focused, showList, onToggle, onOpen, onSelect, onPointerDown, dragging, depth = 0, showDone }: {
  task: Task; selected?: boolean; focused?: boolean; showList?: boolean; depth?: number; dragging?: boolean; showDone?: boolean
  onToggle: (t: Task) => void; onOpen: (t: Task) => void; onSelect?: (t: Task, e: React.MouseEvent) => void; onPointerDown?: (t: Task, e: React.PointerEvent) => void
}) {
  const done = task.status === 'done' || task.status === 'cancelled'
  const pr = priorityLevel(task.priority)
  const startDiffers = task.start && (!task.due || dayOf(task.start) !== dayOf(task.due) || fmtTime(task.start) !== fmtTime(task.due))
  const startChip = task.start && (startDiffers || !task.due)
  return (
    <div
      className={`task-row ${done ? 'done' : ''} ${selected ? 'selected' : ''} ${focused ? 'focused' : ''} ${task.overdue ? 'overdue' : ''} ${dragging ? 'dragging' : ''}`}
      data-task-id={task.id}
      style={depth ? { paddingLeft: 12 + depth * 24 } : undefined}
      onClick={e => { if (onSelect && (e.shiftKey || e.metaKey || e.ctrlKey)) { e.preventDefault(); onSelect(task, e) } else onOpen(task) }}
      onPointerDown={e => onPointerDown?.(task, e)}
    >
      <button type="button" className={`check ${done ? 'on' : ''} ${pr}`} onClick={e => { e.stopPropagation(); onToggle(task) }} onPointerDown={e => e.stopPropagation()} aria-label={done ? 'Reopen' : 'Complete'}>{done ? '✓' : ''}</button>
      <div className="task-main">
        <span className="task-title">{pr !== 'none' && !done && <span className={`prio ${pr}`}>{PRIORITY_MARK[pr]}</span>}{task.title || <span className="muted">Untitled</span>}</span>
        <span className="task-meta">
          {showList && task.list && <span className="list-chip"><span className="dot" style={{ background: task.list.color }} />{task.list.name}</span>}
          {startChip && <span className="due start" title={`When ${task.start}`}>▸ {label(task.start)}</span>}
          <DueChip task={task} />
          {task.repeat && <span className="ico" title={task.repeat.rrule}>↻</span>}
          {task.someday && <span className="ico" title="Someday">☾</span>}
          {task.tags.map(t => <span key={t} className="tag mini" style={{ ['--tag' as string]: tint(t) }}>#{t}</span>)}
          {task.subtask_count > 0 && <span className="ico" title="Subtasks">☰ {task.subtask_done}/{task.subtask_count}</span>}
          {task.attachments?.length > 0 && <span className="ico" title={`${task.attachments.length} attached`}>📎</span>}
          {task.notes && <span className="ico" title="Has notes">≡</span>}
          {done && showDone && task.completed_at && <span className="muted small">{new Date(task.completed_at).toLocaleTimeString(undefined, { hour: 'numeric', minute: '2-digit' })}</span>}
        </span>
      </div>
    </div>
  )
}
