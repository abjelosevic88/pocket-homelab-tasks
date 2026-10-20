import { useMemo } from 'react'
import { Navigate, useParams } from 'react-router-dom'
import { useApp } from '../lib/app-context'
import { TaskPage, type TaskPageProps } from '../components/TaskPage'
import { ListMenuItems, useListDialogs } from '../components/ListMenu'
import { useSections } from '../components/pickers'
import { longDate, today } from '../lib/dates'
import { Loading } from '../components/ui'
import { Icon } from '../components/icons'

type View = 'inbox' | 'today' | 'upcoming' | 'anytime' | 'someday' | 'logbook'

const VIEWS: Record<View, Omit<TaskPageProps, 'query'> & { query?: Record<string, string> }> = {
  inbox: { title: 'Inbox', grouping: 'none', emptyTitle: 'The inbox is empty', emptyText: 'Everything new lands here until it gets a list.', canReorder: true },
  today: { title: 'Today', grouping: 'today', showList: true, emptyTitle: 'Nothing for today', emptyText: 'Give a task a start date of today and it shows up here.', canReorder: true },
  upcoming: { title: 'Upcoming', grouping: 'server', showList: true, emptyTitle: 'Nothing coming up', emptyText: 'Tasks with a start or deadline after today, by day.' },
  anytime: { title: 'Anytime', grouping: 'server', showList: true, emptyTitle: 'Nothing to pick from', emptyText: 'Open tasks without a start date, by list.', canReorder: true },
  someday: { title: 'Someday', grouping: 'server', showList: true, emptyTitle: 'The shelf is empty', emptyText: 'Press s on a task to put it here for later.', canReorder: true },
  logbook: { title: 'Logbook', grouping: 'server', showList: true, quick: false, doneTime: true, emptyTitle: 'Nothing finished yet', emptyText: 'Completed tasks appear here, newest first.' },
}

export function ViewPage({ view }: { view: View }) {
  const { lists } = useApp()
  const cfg = VIEWS[view]
  const sub = view === 'today' ? longDate(today()) : undefined
  const quickDefaults = view === 'today' ? { start: today() } : view === 'someday' ? { someday: true } : view === 'inbox' ? { list_id: lists.find(l => l.is_inbox)?.id ?? null } : undefined
  return <TaskPage key={view} {...cfg} view={view} sub={sub} query={{ view }} quickDefaults={quickDefaults} filterQuery={{ view }} />
}

export function ListPage() {
  const { id } = useParams()
  const { lists, areas, settings } = useApp()
  const list = lists.find(l => l.id === Number(id))
  const sections = useSections(list?.id ?? null)
  const { actions, dialogs } = useListDialogs()
  const query = useMemo(() => ({ list_id: Number(id), status: settings?.show_done_in_lists ? 'all' : 'open', parent_id: 0 }), [id, settings?.show_done_in_lists])
  if (!lists.length) return <div className="page"><Loading /></div>
  if (!list) return <Navigate to="/today" replace />
  const area = areas.find(a => a.id === list.area_id)
  return <>
    <TaskPage key={list.id} title={list.name} sub={(area || list.description || list.is_inbox || list.archived) ? <>{[area?.name, list.description || (list.is_inbox ? 'Everything new lands here until it gets a list.' : '')].filter(Boolean).join(' · ')}{list.archived && <span className="badge" style={{ marginLeft: 6 }}>archived</span>}</> : undefined}
      query={query} grouping="sections" list={list} sections={sections} quickDefaults={{ list_id: list.id }} doneTail canReorder
      filterQuery={{ list_id: list.id }} emptyTitle="Nothing in this list" emptyText="Add a task above, or drag one here from another view."
      menu={close => <><hr /><ListMenuItems list={list} actions={actions} close={close} /></>} />
    {dialogs}
  </>
}

export function TagPage() {
  const { tag = '' } = useParams()
  return <TaskPage key={tag} icon={<Icon name="hash" size={24} style={{ color: 'var(--muted)' }} />} title={tag} query={{ tag, status: 'open' }} grouping="none" showList quickDefaults={{}} filterQuery={{ tag, status: 'open' }} emptyTitle={`Nothing tagged #${tag}`} />
}

export function FilterPage() {
  const { id } = useParams()
  const { filters } = useApp()
  const f = filters.find(x => x.id === Number(id))
  if (!filters.length) return <div className="page"><Loading /></div>
  if (!f) return <Navigate to="/today" replace />
  const q = Object.fromEntries(Object.entries(f.query).filter(([, v]) => v !== undefined && v !== null && !Array.isArray(v))) as Record<string, string | number | boolean>
  return <TaskPage key={f.id} icon={<Icon name="filter" size={22} style={{ color: 'var(--muted)' }} />} title={f.name} sub={<span className="mono small muted">{Object.entries(f.query).map(([k, v]) => `${k}=${Array.isArray(v) ? v.join(',') : v}`).join(' ')}</span>} query={{ ...q, filter_id: f.id }} grouping={f.query.view ? 'server' : 'none'} showList quick={false} emptyTitle="No task matches this filter" />
}

