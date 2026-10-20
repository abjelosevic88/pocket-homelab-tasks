import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, qs, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Task } from '../lib/types'
import { label } from '../lib/dates'
import { snippetParts } from '../lib/format'
import { useDebounce } from './ui'

// ⌘K: tasks by what they say, lists and views by name, a few commands.
export function Palette({ onClose, onNew }: { onClose: () => void; onNew: () => void }) {
  const { lists, areas, filters, tags } = useApp()
  const [q, setQ] = useState('')
  const dq = useDebounce(q, 120)
  const [hits, setHits] = useState<Task[]>([])
  const [sel, setSel] = useState(0)
  const navigate = useNavigate()

  useEffect(() => {
    let live = true
    const t = dq.trim()
    if (!t) { setHits([]); return }
    api.get<Task[]>(`${V1}/search${qs({ q: t, limit: 12 })}`).then(r => live && setHits(r || [])).catch(() => {})
    return () => { live = false }
  }, [dq])
  useEffect(() => setSel(0), [hits, q])

  type Row = { key: string; run: () => void; title: React.ReactNode; sub?: React.ReactNode; ico: string }
  const go = (to: string) => { onClose(); navigate(to) }
  const t = q.trim().toLowerCase()
  const match = (s: string) => !t || s.toLowerCase().includes(t)
  const views: Row[] = [
    { key: 'inbox', run: () => go('/inbox'), title: 'Inbox', ico: '▤' }, { key: 'today', run: () => go('/today'), title: 'Today', ico: '☀' }, { key: 'upcoming', run: () => go('/upcoming'), title: 'Upcoming', ico: '▦' },
    { key: 'anytime', run: () => go('/anytime'), title: 'Anytime', ico: '☰' }, { key: 'someday', run: () => go('/someday'), title: 'Someday', ico: '☾' }, { key: 'logbook', run: () => go('/logbook'), title: 'Logbook', ico: '☑' }, { key: 'calendar', run: () => go('/calendar'), title: 'Calendar', ico: '▦' },
  ].filter(v => match(v.title as string))
  const rows: Row[] = [
    ...(t ? [] : [{ key: 'cmd-new', run: onNew, title: 'New task', ico: '＋' }]),
    ...hits.map(h => ({ key: 'h' + h.id, run: () => { onClose(); navigate(`/list/${h.list_id}?task=${h.id}`) }, title: h.title, sub: <>{h.list?.name}{h.due ? ` · ⚑ ${label(h.due)}` : ''}{h.snippet ? <> · {snippetParts(h.snippet).map((p, i) => p.hit ? <mark key={i}>{p.text}</mark> : <span key={i}>{p.text}</span>)}</> : null}</>, ico: h.status === 'done' ? '☑' : '☐' })),
    ...(t ? views : views.slice(0, 3)),
    ...lists.filter(l => !l.archived && t && match(l.name)).slice(0, 6).map(l => ({ key: 'l' + l.id, run: () => go(`/list/${l.id}`), title: l.name, sub: areas.find(a => a.id === l.area_id)?.name, ico: '▤' })),
    ...filters.filter(f => t && match(f.name)).slice(0, 4).map(f => ({ key: 'f' + f.id, run: () => go(`/filter/${f.id}`), title: f.name, sub: 'Filter', ico: f.icon || '⌕' })),
    ...tags.filter(x => t && match(x.tag)).slice(0, 4).map(x => ({ key: 't' + x.tag, run: () => go(`/tag/${encodeURIComponent(x.tag)}`), title: `#${x.tag}`, sub: `${x.count} tasks`, ico: '#' })),
    ...(t ? [{ key: 'cmd-search', run: () => go(`/search?q=${encodeURIComponent(q.trim())}`), title: `Search everything for “${q.trim()}”`, ico: '⌕' }] : []),
  ]
  const key = (e: React.KeyboardEvent) => {
    if (e.key === 'ArrowDown') { e.preventDefault(); setSel(s => Math.min(rows.length - 1, s + 1)) }
    else if (e.key === 'ArrowUp') { e.preventDefault(); setSel(s => Math.max(0, s - 1)) }
    else if (e.key === 'Enter') { e.preventDefault(); rows[sel]?.run() }
    else if (e.key === 'Escape') onClose()
  }
  return (
    <div className="modal-backdrop top" onMouseDown={e => { if (e.target === e.currentTarget) onClose() }}>
      <div className="switcher" role="dialog" aria-label="Search">
        <input autoFocus value={q} onChange={e => setQ(e.target.value)} onKeyDown={key} placeholder="Find a task, a list, a tag…" />
        <div className="switcher-rows">
          {rows.map((r, i) => (
            <button key={r.key} className={`switcher-row ${i === sel ? 'sel' : ''}`} onMouseEnter={() => setSel(i)} onClick={r.run}>
              <span className="ico">{r.ico}</span>
              <span className="grow"><b>{r.title}</b>{r.sub && <small className="muted">{r.sub}</small>}</span>
            </button>
          ))}
        </div>
        <div className="switcher-foot muted small"><span>↑↓ choose · ↵ open · esc close</span><span>⌘K anywhere</span></div>
      </div>
    </div>
  )
}
