import type { Task } from './types'

// Pages patch their rows in place when the detail panel edits a task; structural changes
// (moves, completion, deletion) go through bump() and a reload instead.
export function emitTask(task: Task) { window.dispatchEvent(new CustomEvent<Task>('pt:task', { detail: task })) }
export function onTask(fn: (t: Task) => void): () => void {
  const h = (e: Event) => fn((e as CustomEvent<Task>).detail)
  window.addEventListener('pt:task', h)
  return () => window.removeEventListener('pt:task', h)
}

/** Is the keyboard busy in a field? Then list shortcuts stay out of the way. */
export function inField(target: EventTarget | null): boolean {
  const el = target as HTMLElement | null
  if (!el || !el.tagName) return false
  return ['INPUT', 'TEXTAREA', 'SELECT'].includes(el.tagName) || el.isContentEditable
}
