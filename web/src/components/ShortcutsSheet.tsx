import { Modal } from './ui'

const GROUPS: { title: string; keys: [string[], string][] }[] = [
  { title: 'Anywhere', keys: [[['⌘', 'K'], 'Search & commands'], [['⌘', '⇧', 'N'], 'New task'], [[','], 'Settings'], [['['], 'Show or hide the sidebar'], [['?'], 'This sheet'], [['esc'], 'Close a dialog, a menu or the task']] },
  { title: 'In a list', keys: [[['n'], 'Add a task (quick add)'], [['/'], 'Add a task (quick add)'], [['↑', '↓'], 'Move through tasks'], [['⇧', '↑↓'], 'Select while moving'], [['↵'], 'Open the task'], [['space'], 'Complete or reopen'], [['⇧', 'A'], 'Select all']] },
  { title: 'The focused or selected tasks', keys: [[['t'], 'Start today'], [['w'], 'When…'], [['d'], 'Deadline…'], [['s'], 'Someday on or off'], [['m'], 'Move to a list…'], [['1'], 'Low priority'], [['2'], 'Medium priority'], [['3'], 'High priority'], [['0'], 'No priority'], [['⌫'], 'Move to the trash']] },
  { title: 'Quick add', keys: [[['↵'], 'Add'], [['⌘', '↵'], 'All fields'], [['esc'], 'Clear, then leave']] },
]

export function ShortcutsSheet({ onClose }: { onClose: () => void }) {
  return (
    <Modal title="Keyboard shortcuts" onClose={onClose} size="lg">
      <div className="shortcuts">
        {GROUPS.map(g => (
          <section key={g.title}>
            <h4 className="sc-h">{g.title}</h4>
            {g.keys.map(([keys, label]) => (
              <div key={label + keys.join('')} className="sc-row"><span>{label}</span><span className="sc-keys">{keys.map(k => <kbd key={k}>{k}</kbd>)}</span></div>
            ))}
          </section>
        ))}
      </div>
      <p className="muted small mt">Quick add reads plain words: <code>tomorrow 9am</code>, <code>!high</code>, <code>#tag</code>, <code>/List</code>, <code>every week</code>, <code>*15m</code>. ⌘-click or ⇧-click rows to select several.</p>
    </Modal>
  )
}
