// ⌘K: tasks by what they say, views, lists, filters and tags by name, and commands.
import React, { useEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { useNavigate } from 'react-router-dom'
import { api, qs, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Task } from '../lib/types'
import { label } from '../lib/dates'
import { snippetParts } from '../lib/format'
import { Icon, ListRing, ViewIcon } from './icons'
import { useDebounce } from './ui'

export interface PaletteCommand { id: string; label: string; icon: string; hint?: string; keywords?: string; run: () => void }

const VIEWS: [string, string][] = [['inbox', 'Inbox'], ['today', 'Today'], ['upcoming', 'Upcoming'], ['anytime', 'Anytime'], ['someday', 'Someday'], ['logbook', 'Logbook'], ['calendar', 'Calendar']]

export function CommandPalette({ onClose, commands }: { onClose: () => void; commands: PaletteCommand[] }) {
  const { lists, areas, filters, tags } = useApp()
  const navigate = useNavigate()
  const [q, setQ] = useState('')
  const dq = useDebounce(q, 120)
  const [hits, setHits] = useState<Task[]>([])
  const [hi, setHi] = useState(0)
  const list = useRef<HTMLDivElement>(null)

  useEffect(() => {
    let live = true
    const t = dq.trim()
    if (!t) { setHits([]); return }
    api.get<Task[]>(`${V1}/search${qs({ q: t, limit: 12 })}`).then(r => live && setHits(r || [])).catch(() => {})
    return () => { live = false }
  }, [dq])

  type Row = { key: string; group: string; icon: React.ReactNode; label: React.ReactNode; sub?: React.ReactNode; hint?: string; run: () => void }
  const rows = useMemo<Row[]>(() => {
    const t = q.trim().toLowerCase()
    const words = t.split(/\s+/).filter(Boolean)
    const hit = (s: string) => words.every(w => s.toLowerCase().includes(w))
    const go = (to: string) => () => navigate(to)
    const out: Row[] = []
    for (const h of hits) out.push({
      key: 'h' + h.id, group: 'Tasks', icon: <Icon name={h.status === 'done' ? 'check-circle' : 'circle'} size={16} />, label: h.title,
      sub: <>{h.list?.name}{h.due ? ` · deadline ${label(h.due)}` : ''}{h.snippet ? <> · {snippetParts(h.snippet).map((p, i) => p.hit ? <mark key={i}>{p.text}</mark> : <span key={i}>{p.text}</span>)}</> : null}</>,
      run: go(`/list/${h.list_id}?task=${h.id}`),
    })
    for (const [v, name] of VIEWS) if (!t || hit(name)) out.push({ key: 'v' + v, group: 'Go to', icon: <ViewIcon view={v} size={16} />, label: name, run: go(`/${v}`) })
    if (t) {
      for (const l of lists.filter(l => !l.archived && hit(l.name)).slice(0, 6)) out.push({ key: 'l' + l.id, group: 'Lists', icon: <ListRing color={l.color} />, label: l.name, sub: areas.find(a => a.id === l.area_id)?.name, run: go(`/list/${l.id}`) })
      for (const f of filters.filter(f => hit(f.name)).slice(0, 4)) out.push({ key: 'f' + f.id, group: 'Filters', icon: <Icon name="filter" size={15} />, label: f.name, run: go(`/filter/${f.id}`) })
      for (const x of tags.filter(x => hit(x.tag)).slice(0, 4)) out.push({ key: 't' + x.tag, group: 'Tags', icon: <Icon name="hash" size={15} />, label: `#${x.tag}`, sub: `${x.count} tasks`, run: go(`/tag/${encodeURIComponent(x.tag)}`) })
    }
    for (const c of commands) if (!t || hit(`${c.label} ${c.keywords || ''}`)) out.push({ key: c.id, group: 'Commands', icon: <Icon name={c.icon} size={16} />, label: c.label, hint: c.hint, run: c.run })
    if (t) out.push({ key: 'search', group: 'Commands', icon: <Icon name="search" size={16} />, label: `Search everything for “${q.trim()}”`, run: go(`/search?q=${encodeURIComponent(q.trim())}`) })
    return out
  }, [q, hits, lists, areas, filters, tags, commands, navigate])
  useEffect(() => setHi(0), [q, hits])
  useEffect(() => { list.current?.querySelector(`[data-i="${hi}"]`)?.scrollIntoView({ block: 'nearest' }) }, [hi])

  const run = (r: Row | undefined) => { if (!r) return; onClose(); r.run() }
  let last = ''
  return createPortal(
    <div className="palette-backdrop" onMouseDown={e => { if (e.target === e.currentTarget) onClose() }}>
      <div className="palette-box" role="dialog" aria-label="Search and commands">
        <div className="pal-input">
          <Icon name="search" size={18} />
          <input autoFocus value={q} onChange={e => setQ(e.target.value)} placeholder="Find a task, a list, a tag, or run a command…" aria-label="Search"
            onKeyDown={e => {
              if (e.key === 'ArrowDown') { e.preventDefault(); setHi(h => Math.min(rows.length - 1, h + 1)) }
              else if (e.key === 'ArrowUp') { e.preventDefault(); setHi(h => Math.max(0, h - 1)) }
              else if (e.key === 'Enter') { e.preventDefault(); run(rows[hi]) }
              else if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); onClose() }
            }} />
          <kbd>esc</kbd>
        </div>
        <div className="pal-list" ref={list} role="listbox">
          {rows.length === 0 && <div className="pal-empty">Nothing matches.</div>}
          {rows.map((r, i) => {
            const head = r.group !== last ? <div className="pal-group" key={'g' + r.group}>{r.group}</div> : null
            last = r.group
            return (
              <React.Fragment key={r.key}>
                {head}
                <div data-i={i} role="option" aria-selected={i === hi} className={`pal-row ${i === hi ? 'hi' : ''}`} onMouseEnter={() => setHi(i)} onClick={() => run(r)}>
                  <span className="pal-ico">{r.icon}</span>
                  <span className="grow"><span className="pal-label ellipsis">{r.label}</span>{r.sub && <span className="pal-sub ellipsis">{r.sub}</span>}</span>
                  {r.hint && <kbd>{r.hint}</kbd>}
                  {i === hi && <Icon name="chevron-right" size={14} className="muted" />}
                </div>
              </React.Fragment>
            )
          })}
        </div>
      </div>
    </div>, document.body)
}
