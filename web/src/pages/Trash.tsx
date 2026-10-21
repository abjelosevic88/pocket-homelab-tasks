import { useState } from 'react'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Task } from '../lib/types'
import { ago, stamp } from '../lib/format'
import { Confirm, Empty, Loading, PageHeader, useAsync, useToast } from '../components/ui'
import { Icon } from '../components/icons'

type Trashed = Task & { deleted_at?: string }

export default function Trash() {
  const { data, reload } = useAsync(() => api.get<Trashed[]>(`${V1}/trash`), [])
  const { settings, bump, lists } = useApp()
  const { toast } = useToast()
  const [empty, setEmpty] = useState(false)
  const run = async (fn: () => Promise<unknown>, msg: string) => { try { await fn(); toast(msg); reload(); bump() } catch (e) { toast((e as Error).message, 'err') } }
  return (
    <div className="page">
      <PageHeader title="Trash" sub={`Deleted tasks stay ${settings?.trash_days ?? 30} days, then they are gone.`}
        actions={data && data.length > 0 && <button type="button" className="btn danger-ghost" onClick={() => setEmpty(true)}><Icon name="trash" size={15} />Empty the trash</button>} />
      {!data ? <Loading /> : data.length === 0 ? <Empty icon="trash" title="The trash is empty">Deleted tasks wait here before they go for good.</Empty> : (
        <div className="card trash-card"><table className="table trash-table">
          <thead><tr><th>Task</th><th>List</th><th>Deleted</th><th /></tr></thead>
          <tbody>{data.map(t => (
            <tr key={t.id}>
              <td><b>{t.title}</b>{t.notes && <div className="muted small ellipsis" style={{ maxWidth: 420 }}>{t.notes.slice(0, 140)}</div>}</td>
              <td className="muted">{t.list?.name || lists.find(l => l.id === t.list_id)?.name || ''}</td>
              <td className="muted" title={t.deleted_at ? stamp(t.deleted_at) : ''}>{t.deleted_at ? ago(t.deleted_at) : ''}</td>
              <td className="right"><span className="row" style={{ justifyContent: 'flex-end' }}><button type="button" className="btn sm" onClick={() => run(() => api.post(`${V1}/trash/${t.id}/restore`), 'Restored')}><Icon name="undo" size={14} />Restore</button><button type="button" className="btn ghost sm danger-ghost" onClick={() => run(() => api.del(`${V1}/trash/${t.id}`), 'Deleted for good')}>Delete</button></span></td>
            </tr>
          ))}</tbody>
        </table></div>
      )}
      {empty && <Confirm title="Empty the trash?" message="Every deleted task goes for good." onCancel={() => setEmpty(false)} onConfirm={() => { setEmpty(false); run(() => api.del(`${V1}/trash`), 'Trash emptied') }} />}
    </div>
  )
}
