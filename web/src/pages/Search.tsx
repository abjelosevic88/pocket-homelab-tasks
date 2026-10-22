import { useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { api, qs, V1 } from '../lib/api'
import type { Task } from '../lib/types'
import { snippetParts } from '../lib/format'
import { Empty, Loading, PageHeader, useDebounce } from '../components/ui'
import { useTaskCompletion } from '../lib/use-task-completion'
import { TaskRow } from '../components/TaskRow'
import { useApp } from '../lib/app-context'
import { Icon } from '../components/icons'

export default function Search() {
  const [params, setParams] = useSearchParams()
  const q = params.get('q') || ''
  const [text, setText] = useState(q)
  const dq = useDebounce(text, 250)
  const [hits, setHits] = useState<Task[] | null>(null)
  const [busy, setBusy] = useState(false)
  const { version } = useApp()
  const { toggle, pending } = useTaskCompletion()
  useEffect(() => { if (dq !== q) setParams(p => { const n = new URLSearchParams(p); if (dq.trim()) n.set('q', dq.trim()); else n.delete('q'); return n }, { replace: true }) }, [dq]) // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => {
    if (!q.trim()) { setHits(null); return }
    let live = true
    setBusy(true)
    api.get<Task[]>(`${V1}/search${qs({ q, limit: 100 })}`).then(r => live && setHits(r || [])).catch(() => live && setHits([])).finally(() => live && setBusy(false))
    return () => { live = false }
  }, [q, version])
  const open = (t: Task) => setParams(p => { const n = new URLSearchParams(p); n.set('task', String(t.id)); return n })
  return (
    <div className="page search-page">
      <PageHeader title="Search" sub="Titles, notes and tags; word beginnings match, accents do not matter." />
      <div className="search-field lg mb"><Icon name="search" size={17} /><input autoFocus value={text} onChange={e => setText(e.target.value)} placeholder="Search tasks…" aria-label="Search tasks" /></div>
      {busy && !hits && <Loading />}
      {hits && !hits.length && <Empty icon="search" title={`Nothing for “${q}”`} />}
      {!q.trim() && !hits && <Empty icon="search" title="Search every task">Words in titles, notes and tags. ⌘K searches from anywhere.</Empty>}
      {hits && hits.length > 0 && <div className="task-list">
        {hits.map(h => <div key={h.id} className="task-slot">
          <TaskRow task={h} pending={pending.has(h.id)} showList onToggle={toggle} onOpen={open} />
          {h.snippet && <div className="snippet muted small">{snippetParts(h.snippet).map((p, i) => p.hit ? <mark key={i}>{p.text}</mark> : <span key={i}>{p.text}</span>)}</div>}
        </div>)}
      </div>}
    </div>
  )
}
