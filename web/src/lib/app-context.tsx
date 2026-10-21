import React, { createContext, useCallback, useContext, useEffect, useRef, useState } from 'react'
import { api, V1 } from './api'
import type { Area, AuthStatus, Dashboard, Filter, List, NcSync, Settings, TagCount, User } from './types'

export type ThemeMode = 'system' | 'light' | 'dark'

interface AppState {
  status: AuthStatus | null
  user: User | null
  settings: Settings | null
  lists: List[]
  areas: Area[]
  tags: TagCount[]
  filters: Filter[]
  dash: Dashboard | null
  sync: NcSync | null
  /** Changes whenever tasks changed anywhere: pages reload on it. */
  version: number
  bump: () => void
  themeMode: ThemeMode
  theme: 'light' | 'dark'
  setThemeMode: (m: ThemeMode) => void
  toggleTheme: () => void
  refresh: () => Promise<void>
  reloadMeta: () => Promise<void>
  setUser: (u: User | null) => void
  setSettings: (s: Settings) => void
  setSync: (s: NcSync | null) => void
}

const Ctx = createContext<AppState>(null!)
export function useApp() { return useContext(Ctx) }

export function AppProvider({ children }: { children: React.ReactNode }) {
  const [status, setStatus] = useState<AuthStatus | null>(null)
  const [user, setUser] = useState<User | null>(null)
  const [settings, setSettings] = useState<Settings | null>(null)
  const [lists, setLists] = useState<List[]>([])
  const [areas, setAreas] = useState<Area[]>([])
  const [tags, setTags] = useState<TagCount[]>([])
  const [filters, setFilters] = useState<Filter[]>([])
  const [dash, setDash] = useState<Dashboard | null>(null)
  const [sync, setSync] = useState<NcSync | null>(null)
  const [version, setVersion] = useState(0)
  // The theme follows the system unless set to light or dark (stored per browser).
  const [themeMode, setThemeModeRaw] = useState<ThemeMode>(() => (localStorage.getItem('pt-theme') as ThemeMode) || 'system')
  const [systemDark, setSystemDark] = useState(() => window.matchMedia('(prefers-color-scheme: dark)').matches)
  useEffect(() => {
    const mq = window.matchMedia('(prefers-color-scheme: dark)')
    const h = () => setSystemDark(mq.matches)
    mq.addEventListener('change', h)
    return () => mq.removeEventListener('change', h)
  }, [])
  const theme: 'light' | 'dark' = themeMode === 'system' ? (systemDark ? 'dark' : 'light') : themeMode
  useEffect(() => {
    document.documentElement.dataset.theme = theme
    document.querySelectorAll('meta[name="theme-color"]').forEach(m => m.setAttribute('content', theme === 'dark' ? '#111113' : '#ffffff'))
  }, [theme])
  const setThemeMode = useCallback((m: ThemeMode) => { setThemeModeRaw(m); if (m === 'system') localStorage.removeItem('pt-theme'); else localStorage.setItem('pt-theme', m) }, [])
  useEffect(() => { if (settings?.title) document.title = settings.title }, [settings?.title])

  const reloadMeta = useCallback(async () => {
    const [l, a, t, f, d, s] = await Promise.all([
      api.get<List[]>(`${V1}/lists`).catch(() => [] as List[]),
      api.get<Area[]>(`${V1}/areas`).catch(() => [] as Area[]),
      api.get<TagCount[]>(`${V1}/tags`).catch(() => [] as TagCount[]),
      api.get<Filter[]>(`${V1}/filters`).catch(() => [] as Filter[]),
      api.get<Dashboard>(`${V1}/dashboard`).catch(() => null),
      api.get<NcSync>(`${V1}/sync/nextcloud`).catch(() => null),
    ])
    setLists(l || []); setAreas(a || []); setTags(t || []); setFilters(f || []); setDash(d); setSync(s)
  }, [])

  const refresh = useCallback(async () => {
    const st = await api.get<AuthStatus>(`${V1}/auth/status`)
    setStatus(st)
    setUser(st.user)
    if (st.authenticated) {
      setSettings(await api.get<Settings>(`${V1}/settings`))
      await reloadMeta()
    }
  }, [reloadMeta])

  useEffect(() => { refresh().catch(() => setStatus({ needs_setup: false, auth_mode: 'local', authenticated: false, user: null, version: '', title: 'Pocket Tasks' })) }, [refresh])
  useEffect(() => {
    const h = () => { setUser(null); setStatus(s => s ? { ...s, authenticated: false, user: null } : s) }
    window.addEventListener('pt:unauthorized', h)
    return () => window.removeEventListener('pt:unauthorized', h)
  }, [])
  useEffect(() => { if (user && version > 0) reloadMeta().catch(() => {}) }, [version, user, reloadMeta])

  const bump = useCallback(() => setVersion(v => v + 1), [])

  // Changes made elsewhere (the phone, a CalDAV client, Nextcloud) reach the open page within
  // seconds: a stamp is polled while the tab is visible; when it moves, pages and counts reload.
  const stamp = useRef<number | null>(null)
  useEffect(() => {
    if (!user) return
    const check = async () => {
      if (document.hidden) return
      try {
        const r = await api.get<{ stamp: number }>(`${V1}/changes`)
        if (stamp.current !== null && r.stamp !== stamp.current) bump()
        stamp.current = r.stamp
      } catch {}
    }
    check()
    const t = setInterval(check, 10000)
    const onShow = () => { if (!document.hidden) check() }
    document.addEventListener('visibilitychange', onShow)
    window.addEventListener('focus', onShow)
    return () => { clearInterval(t); document.removeEventListener('visibilitychange', onShow); window.removeEventListener('focus', onShow) }
  }, [user, bump])

  return <Ctx.Provider value={{ status, user, settings, lists, areas, tags, filters, dash, sync, version, bump, themeMode, theme, setThemeMode, toggleTheme: () => setThemeMode(theme === 'dark' ? 'light' : 'dark'), refresh, reloadMeta, setUser, setSettings, setSync }}>{children}</Ctx.Provider>
}
