// Typed client for the Matinee server API (/api/v1, documented in
// internal/api/openapi.yaml). The session lives in an HttpOnly cookie, so
// requests just need to be same-origin.

// guest is the shared account of a device at home that hasn't signed in.
export type User = { id: number; username: string; isAdmin: boolean; guest?: boolean }
export type Settings = { requireSignInAtHome: boolean }

export type Device = {
  id: string
  deviceName: string
  platform: string
  createdAt: number
  lastSeenAt: number
  current: boolean
}

export type PendingDevice = { userCode: string; deviceName: string; platform: string; expiresAt: number }

export type Library = {
  id: number
  name: string
  kind: 'movies' | 'tv' | 'music' | string
  paths: string[]
  scanning: boolean
}

export type Item = {
  id: number
  libraryId: number
  parentId?: number
  kind: 'movie' | 'show' | 'season' | 'episode' | 'artist' | 'album' | 'track' | string
  title: string
  year?: number
  index?: number
  disc?: number
  durationMs?: number
  container?: string
  videoCodec?: string
  audioCodec?: string
  width?: number
  height?: number
  addedAt: number
  playable: boolean
  childCount?: number
  positionMs?: number
  played?: boolean
  overview?: string
  rating?: number
  hasPoster: boolean
  hasBackdrop: boolean
  showTitle?: string
  seasonIndex?: number
  // True until the server has looked the item up online.
  pending?: boolean
  onWatchList?: boolean
}

// Only movies and shows go on a watch list.
export const canWatchList = (item: Item) => item.kind === 'movie' || item.kind === 'show'

// How often lists refresh while some of their items are still being looked up.
export const pendingRefetch = (items?: Item[]) => (items?.some((i) => i.pending) ? 10_000 : false)

export type SubtitleTrack = {
  id: string
  label: string
  language?: string
  codec: string
  forced?: boolean
  default?: boolean
  sdh?: boolean
  external?: boolean
  // Picture-based subtitles the server draws into a transcode.
  burnIn?: boolean
}

export type ItemDetail = Item & { mimeType?: string; subtitles?: SubtitleTrack[]; parents: Item[] }

export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    credentials: 'same-origin',
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  if (!res.ok) {
    let message = res.statusText
    try {
      message = (await res.json()).error ?? message
    } catch {
      // not JSON
    }
    throw new ApiError(res.status, message)
  }
  if (res.status === 204 || res.status === 202) return undefined as T
  return res.json() as Promise<T>
}

export const api = {
  server: () => request<{ version: string; apiVersion: number; name?: string }>('GET', '/api/v1/server'),
  setup: () => request<{ needsSetup: boolean }>('GET', '/api/v1/setup'),
  createAdmin: (username: string, password: string) =>
    request<{ user: User }>('POST', '/api/v1/setup', { username, password }),
  login: (username: string, password: string) =>
    request<{ user: User }>('POST', '/api/v1/auth/login', { username, password }),
  logout: () => request<void>('POST', '/api/v1/auth/logout'),
  me: () => request<User>('GET', '/api/v1/me'),
  devices: () => request<Device[]>('GET', '/api/v1/devices'),
  signOutDevice: (id: string) => request<void>('DELETE', `/api/v1/devices/${encodeURIComponent(id)}`),
  pendingDevice: (code: string) => request<PendingDevice>('GET', `/api/v1/auth/device/${encodeURIComponent(code)}`),
  decideDevice: (code: string, approve: boolean) =>
    request<void>('POST', `/api/v1/auth/device/${encodeURIComponent(code)}/${approve ? 'approve' : 'deny'}`),

  libraries: () => request<Library[]>('GET', '/api/v1/libraries'),
  createLibrary: (name: string, kind: string, paths: string[]) =>
    request<Library>('POST', '/api/v1/libraries', { name, kind, paths }),
  deleteLibrary: (id: number) => request<void>('DELETE', `/api/v1/libraries/${id}`),
  scanLibrary: (id: number) => request<void>('POST', `/api/v1/libraries/${id}/scan`),
  libraryItems: (id: number) => request<Item[]>('GET', `/api/v1/libraries/${id}/items`),
  browse: (path: string) =>
    request<{ path: string; parent: string; dirs: string[] }>('GET', `/api/v1/fs?path=${encodeURIComponent(path)}`),

  settings: () => request<Settings>('GET', '/api/v1/settings'),
  saveSettings: (s: Settings) => request<Settings>('PUT', '/api/v1/settings', s),
  users: () => request<User[]>('GET', '/api/v1/users'),
  createUser: (username: string, password: string, isAdmin: boolean) =>
    request<User>('POST', '/api/v1/users', { username, password, isAdmin }),

  home: () => request<{ continueWatching: Item[]; watchList: Item[]; recentlyAdded: Item[] }>('GET', '/api/v1/home'),
  search: (q: string) => request<Item[]>('GET', `/api/v1/search?q=${encodeURIComponent(q)}`),
  item: (id: number) => request<ItemDetail>('GET', `/api/v1/items/${id}`),
  children: (id: number) => request<Item[]>('GET', `/api/v1/items/${id}/children`),
  watchList: () => request<Item[]>('GET', '/api/v1/watchlist'),
  addToWatchList: (id: number) => request<void>('PUT', `/api/v1/watchlist/${id}`),
  removeFromWatchList: (id: number) => request<void>('DELETE', `/api/v1/watchlist/${id}`),
  hideFromContinueWatching: (id: number) => request<void>('DELETE', `/api/v1/continue-watching/${id}`),
  setProgress: (id: number, positionMs: number, played = false) =>
    request<void>('PUT', `/api/v1/items/${id}/progress`, { positionMs, played }),
  startHls: (id: number, startSec = 0, burnSubtitle?: string) =>
    request<{ url: string; startSec: number }>(
      'POST',
      `/api/v1/items/${id}/hls?start=${startSec}${burnSubtitle ? `&subtitle=${encodeURIComponent(burnSubtitle)}` : ''}`,
    ),
  subtitles: async (id: number, track: string) => {
    const res = await fetch(`/api/v1/items/${id}/subtitles/${encodeURIComponent(track)}.vtt`, { credentials: 'same-origin' })
    if (!res.ok) throw new ApiError(res.status, "Couldn't load these subtitles")
    return res.text()
  },
}

export const imageUrl = (id: number, type: 'poster' | 'backdrop') => `/api/v1/items/${id}/image/${type}`
export const fileUrl = (id: number) => `/api/v1/items/${id}/file`
