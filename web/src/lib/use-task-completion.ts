import { useRef, useState } from 'react'
import { api, V1 } from './api'
import { useApp } from './app-context'
import { emitTask } from './events'
import { label } from './dates'
import type { Task } from './types'
import { useToast } from '../components/ui'

export function useTaskCompletion() {
  const { bump } = useApp()
  const { toast } = useToast()
  const locked = useRef(new Set<number>())
  const [pending, setPending] = useState(new Set<number>())

  const toggle = async (task: Task) => {
    if (locked.current.has(task.id)) return
    locked.current.add(task.id)
    setPending(new Set(locked.current))
    const done = task.status === 'done' || task.status === 'cancelled'
    try {
      // Keep the row mounted long enough to see the check and strike-through.
      if (!done && !window.matchMedia('(prefers-reduced-motion: reduce)').matches) {
        await new Promise(resolve => window.setTimeout(resolve, 650))
      }
      const result = await api.post<{ task: Task; next?: Task }>(`${V1}/tasks/${task.id}/complete`, { done: !done })
      emitTask(result.task)
      bump()
      if (!done) {
        let nextRemoved = false
        toast(`Completed “${task.title || 'Untitled'}”${result.next ? ` · Next: ${label(result.next.start || result.next.due)}` : ''}`, 'ok', {
          label: 'Undo', duration: 6000,
          run: async () => {
            try {
              // Reopening alone loses a recurring task's rule and leaves an extra occurrence.
              if (result.next && !nextRemoved) {
                await api.del(`${V1}/tasks/${result.next.id}`)
                nextRemoved = true
              }
              const restored = await api.put<Task>(`${V1}/tasks/${task.id}`, {
                status: task.status, percent: task.percent, repeat: task.repeat,
              })
              emitTask(restored)
              bump()
            } catch (error) {
              toast(`Could not undo: ${(error as Error).message}`, 'err')
              bump()
              throw error // Keep Undo available for a retry.
            }
          },
        })
      }
      return result.task
    } catch (error) {
      toast((error as Error).message, 'err')
      bump()
    } finally {
      locked.current.delete(task.id)
      setPending(new Set(locked.current))
    }
  }

  return { toggle, pending }
}
