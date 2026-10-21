import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Area, List } from '../lib/types'
import { Confirm, Field, type MenuItem, Modal, Prompt, useToast } from './ui'
import { Select } from './Select'
import { Icon } from './icons'
import { ColorPicker } from './pickers'

type Dialog =
  | { kind: 'newList'; areaId: number | null } | { kind: 'rename'; list: List } | { kind: 'color'; list: List } | { kind: 'area'; list: List } | { kind: 'section'; list: List } | { kind: 'delete'; list: List }
  | { kind: 'newArea' } | { kind: 'renameArea'; area: Area } | { kind: 'colorArea'; area: Area } | { kind: 'deleteArea'; area: Area }

/** The dialogs behind every list and area menu, shared by the sidebar and the list page. */
export function useListDialogs() {
  const { bump, lists, areas } = useApp()
  const { toast } = useToast()
  const navigate = useNavigate()
  const [d, setD] = useState<Dialog | null>(null)
  const run = async (fn: () => Promise<unknown>, msg?: string) => { try { await fn(); if (msg) toast(msg); bump() } catch (e) { toast((e as Error).message, 'err') } }
  const close = () => setD(null)

  const actions = {
    newList: (areaId: number | null = null) => setD({ kind: 'newList', areaId }),
    rename: (list: List) => setD({ kind: 'rename', list }),
    color: (list: List) => setD({ kind: 'color', list }),
    moveArea: (list: List) => setD({ kind: 'area', list }),
    addSection: (list: List) => setD({ kind: 'section', list }),
    archive: (list: List) => run(() => api.put(`${V1}/lists/${list.id}`, { archived: !list.archived }), list.archived ? 'List restored' : 'List archived'),
    del: (list: List) => setD({ kind: 'delete', list }),
    newArea: () => setD({ kind: 'newArea' }),
    renameArea: (area: Area) => setD({ kind: 'renameArea', area }),
    colorArea: (area: Area) => setD({ kind: 'colorArea', area }),
    delArea: (area: Area) => setD({ kind: 'deleteArea', area }),
  }

  let dialogs: React.ReactNode = null
  if (d?.kind === 'newList') dialogs = <Prompt title={d.areaId ? `New list in ${areas.find(a => a.id === d.areaId)?.name || 'the area'}` : 'New list'} placeholder="List name" button="Make" onClose={close} onDone={name => { close(); run(async () => { const l = await api.post<List>(`${V1}/lists`, { name, area_id: d.areaId ?? undefined }); navigate(`/list/${l.id}`) }) }} />
  else if (d?.kind === 'rename') dialogs = <Prompt title={`Rename “${d.list.name}”`} value={d.list.name} button="Rename" onClose={close} onDone={name => { close(); run(() => api.put(`${V1}/lists/${d.list.id}`, { name }), 'Renamed') }}><DescField list={d.list} /></Prompt>
  else if (d?.kind === 'color') dialogs = <ColorModal title={`Colour of “${d.list.name}”`} value={d.list.color} onClose={close} onDone={color => { close(); run(() => api.put(`${V1}/lists/${d.list.id}`, { color })) }} />
  else if (d?.kind === 'area') dialogs = <Modal title={`Move “${d.list.name}” to an area`} onClose={close}><div className="pick-list">
    <button type="button" className="pick-row" onClick={() => { close(); run(() => api.put(`${V1}/lists/${d.list.id}`, { area_id: null })) }}><span className="dot" style={{ background: 'transparent', boxShadow: 'inset 0 0 0 1.5px var(--border-strong)' }} /><span className="grow">No area</span>{!d.list.area_id && <Icon name="check" size={14} className="muted" />}</button>
    {areas.map(a => <button type="button" key={a.id} className="pick-row" onClick={() => { close(); run(() => api.put(`${V1}/lists/${d.list.id}`, { area_id: a.id })) }}><span className="dot" style={{ background: a.color || 'var(--muted)' }} /><span className="grow">{a.name}</span>{d.list.area_id === a.id && <Icon name="check" size={14} className="muted" />}</button>)}
  </div></Modal>
  else if (d?.kind === 'section') dialogs = <Prompt title={`New section in “${d.list.name}”`} placeholder="Section name" button="Add" onClose={close} onDone={name => { close(); run(() => api.post(`${V1}/lists/${d.list.id}/sections`, { name }), 'Section added') }} />
  else if (d?.kind === 'delete') dialogs = <DeleteListModal list={d.list} lists={lists} onClose={close} onDone={moveTo => { close(); run(async () => { await api.del(`${V1}/lists/${d.list.id}${moveTo ? `?move_to=${moveTo}` : ''}`); navigate('/today') }, 'List deleted') }} />
  else if (d?.kind === 'newArea') dialogs = <Prompt title="New area" placeholder="Area name (e.g. Work, Home)" button="Make" onClose={close} onDone={name => { close(); run(() => api.post(`${V1}/areas`, { name }), 'Area added') }} />
  else if (d?.kind === 'renameArea') dialogs = <Prompt title={`Rename “${d.area.name}”`} value={d.area.name} button="Rename" onClose={close} onDone={name => { close(); run(() => api.put(`${V1}/areas/${d.area.id}`, { name }), 'Renamed') }} />
  else if (d?.kind === 'colorArea') dialogs = <ColorModal title={`Colour of “${d.area.name}”`} value={d.area.color} onClose={close} onDone={color => { close(); run(() => api.put(`${V1}/areas/${d.area.id}`, { color })) }} />
  else if (d?.kind === 'deleteArea') dialogs = <Confirm title={`Delete the area “${d.area.name}”?`} message="Its lists keep existing without an area." onCancel={close} onConfirm={() => { close(); run(() => api.del(`${V1}/areas/${d.area.id}`), 'Area deleted') }} />

  return { actions, dialogs }
}

function DescField({ list }: { list: List }) {
  const { bump } = useApp()
  const [v, setV] = useState(list.description)
  return <Field label="Description"><input value={v} onChange={e => setV(e.target.value)} onBlur={() => { if (v !== list.description) api.put(`${V1}/lists/${list.id}`, { description: v }).then(bump).catch(() => {}) }} placeholder="What this list is for" /></Field>
}

function ColorModal({ title, value, onClose, onDone }: { title: string; value: string; onClose: () => void; onDone: (c: string) => void }) {
  const [c, setC] = useState(value || '#2563eb')
  return <Modal title={title} onClose={onClose} size="sm"><ColorPicker value={c} onChange={setC} /><div className="form-actions mt"><button type="button" className="btn" onClick={onClose}>Cancel</button><button type="button" className="btn primary" onClick={() => onDone(c)}>Save</button></div></Modal>
}

function DeleteListModal({ list, lists, onClose, onDone }: { list: List; lists: List[]; onClose: () => void; onDone: (moveTo: number | null) => void }) {
  const others = lists.filter(l => l.id !== list.id && !l.archived)
  const [to, setTo] = useState<number | null>(null)
  return (
    <Modal title={`Delete “${list.name}”?`} onClose={onClose}>
      <p>{list.open_count ? `It holds ${list.open_count} open task${list.open_count === 1 ? '' : 's'}. ` : ''}Where should its tasks go?</p>
      <Select<number> value={to ?? 0} onChange={v => setTo(v || null)} label="Where its tasks go" searchable={others.length > 8} options={[{ value: 0, label: 'The trash', icon: <Icon name="trash" size={14} className="muted" /> }, ...others.map(l => ({ value: l.id, label: `Move to ${l.name}`, color: l.color || undefined }))]} />
      <div className="form-actions mt"><button type="button" className="btn" onClick={onClose}>Cancel</button><button type="button" className="btn danger" onClick={() => onDone(to)}>Delete list</button></div>
    </Modal>
  )
}

/** The menu of a list: in the sidebar (⋯ or right-click) and on the list page. */
export function listMenuItems(list: List, actions: ReturnType<typeof useListDialogs>['actions']): MenuItem[] {
  return [
    { label: 'Rename…', icon: 'edit', onSelect: () => actions.rename(list) },
    { label: 'Colour…', icon: 'palette', onSelect: () => actions.color(list) },
    { label: 'Move to area…', icon: 'folder', onSelect: () => actions.moveArea(list) },
    { label: 'Add section…', icon: 'plus', onSelect: () => actions.addSection(list) },
    ...(!list.is_inbox ? ['sep' as const,
      { label: list.archived ? 'Restore from archive' : 'Archive', icon: 'archive', onSelect: () => actions.archive(list) },
      { label: 'Delete…', icon: 'trash', danger: true, onSelect: () => actions.del(list) }] : []),
  ]
}
