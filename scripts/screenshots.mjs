// Capture the real demo UI for the README. Setup: docs/screenshots/README.md.
// Node 22+; Chrome's DevTools protocol, with no npm dependencies.
import fs from 'node:fs/promises'

const base = process.env.BASE || 'http://127.0.0.1:18197'
const chrome = process.env.CHROME || 'http://127.0.0.1:19227'
const out = new URL('../docs/screenshots/', import.meta.url)
const get = async url => {
  const response = await fetch(url)
  if (!response.ok) throw new Error(`${url}: HTTP ${response.status}`)
  return response.json()
}
const lists = await get(`${base}/api/v1/lists`)
const list = lists.find(l => l.name === 'Server rebuild')
const { tasks } = await get(`${base}/api/v1/tasks?view=today`)
const task = tasks.find(t => t.title === 'Move Immich to the NVMe')
if (!list || !task) throw new Error('Start a fresh DEMO_DATA=true AUTH_MODE=none instance first.')

const version = await get(`${chrome}/json/version`)
const ws = new WebSocket(version.webSocketDebuggerUrl)
await new Promise((resolve, reject) => { ws.onopen = resolve; ws.onerror = reject })
let id = 0
const pending = new Map()
ws.onmessage = event => {
  const message = JSON.parse(event.data)
  pending.get(message.id)?.(message)
}
const send = (method, params = {}, sessionId) => new Promise((resolve, reject) => {
  const requestId = ++id
  const timer = setTimeout(() => { pending.delete(requestId); reject(new Error(`Timeout: ${method}`)) }, 15000)
  pending.set(requestId, message => {
    clearTimeout(timer)
    pending.delete(requestId)
    if (message.error) reject(new Error(`${method}: ${JSON.stringify(message.error)}`))
    else resolve(message.result)
  })
  ws.send(JSON.stringify({ id: requestId, method, params, sessionId }))
})
let targetId
try {
  ;({ targetId } = await send('Target.createTarget', { url: 'about:blank' }))
  const { sessionId } = await send('Target.attachToTarget', { targetId, flatten: true })
  const s = (method, params) => send(method, params, sessionId)
  await s('Page.enable')
  await s('Runtime.enable')
  await s('Emulation.setTimezoneOverride', { timezoneId: 'Europe/Sarajevo' })
  const evaluate = async expression => {
    const result = await s('Runtime.evaluate', { expression, awaitPromise: true, returnByValue: true })
    if (result.exceptionDetails) throw new Error(JSON.stringify(result.exceptionDetails))
    return result.result.value
  }
  const waitFor = async expression => {
    const deadline = Date.now() + 15000
    while (Date.now() < deadline) {
      if (await evaluate(`Boolean(${expression})`)) return
      await new Promise(resolve => setTimeout(resolve, 100))
    }
    throw new Error(`UI did not become ready: ${expression}`)
  }
  await fs.mkdir(out, { recursive: true })
  let themeScript
  async function shot(name, path, { mobile = false, dark = false, ready, before } = {}) {
    await s('Emulation.setDeviceMetricsOverride', {
      width: mobile ? 390 : 1280, height: mobile ? 844 : 860,
      deviceScaleFactor: mobile ? 2 : 1, mobile,
    })
    if (themeScript) await s('Page.removeScriptToEvaluateOnNewDocument', { identifier: themeScript })
    ;({ identifier: themeScript } = await s('Page.addScriptToEvaluateOnNewDocument', {
      source: `localStorage.setItem('pt-theme', '${dark ? 'dark' : 'light'}')`,
    }))
    await s('Page.navigate', { url: base + path })
    await waitFor("(document.querySelector('.sidebar .sb-row') && document.querySelector('.page') || document.querySelector('.set-page .set-section')) && !document.querySelector('.loading')")
    if (ready) await waitFor(ready)
    if (before) await before({ s, evaluate, waitFor })
    await evaluate('document.fonts.ready.then(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))))')
    const { data } = await s('Page.captureScreenshot', { format: 'png' })
    await fs.writeFile(new URL(`${name}.png`, out), Buffer.from(data, 'base64'))
    console.log(`Captured ${name}.png`)
  }
  const todayReady = "document.querySelector('.task-row') && document.body.innerText.includes('Water the plants')"
  const detailReady = "document.querySelector('.detail-title')?.value === 'Move Immich to the NVMe' && [...document.querySelectorAll('.subtasks input')].some(input => input.value === 'rsync the library')"
  await shot('today', '/today', { ready: todayReady })
  await shot('list', `/list/${list.id}`, { ready: "document.body.innerText.includes('Write the runbook')" })
  await shot('task', `/list/${list.id}?task=${task.id}`, { ready: detailReady })
  await shot('calendar', '/calendar', { ready: "document.querySelector('.cal-item')" })
  await shot('quick-add', '/inbox', {
    ready: "document.body.innerText.includes('Book dentist')",
    before: async ({ s, evaluate, waitFor }) => {
      await evaluate("document.querySelector('.quick-add input').focus()")
      await s('Input.insertText', { text: 'Call the bank tomorrow 9am !high #money /House every month *30m' })
      await waitFor("document.querySelectorAll('.quick-chips .chip').length >= 5")
    },
  })
  await shot('calendar-apps', '/settings/calendar', {
    ready: "document.querySelector('input[value$=\"/dav/\"]')",
    before: async ({ evaluate }) => {
      await evaluate("document.querySelector('.set-howto details').open = true")
      await evaluate("document.querySelector('input[value$=\"/dav/\"]').closest('.set-section').scrollIntoView()")
    },
  })
  await shot('nextcloud', '/settings/nextcloud', {
    ready: "document.body.innerText.includes('Two-way')",
    before: async ({ evaluate, waitFor }) => {
      await evaluate("[...document.querySelectorAll('button')].find(button => button.textContent === 'Two-way').click()")
      await waitFor("document.querySelector('input[placeholder=\"https://cloud.example.com\"]')")
    },
  })
  await shot('dark', `/list/${list.id}`, { dark: true, ready: "document.body.innerText.includes('Write the runbook')" })
  await shot('mobile-today', '/today', { mobile: true, ready: todayReady })
  await shot('mobile-task', `/today?task=${task.id}`, { mobile: true, ready: detailReady })
} finally {
  if (targetId) await send('Target.closeTarget', { targetId })
  ws.close()
}
