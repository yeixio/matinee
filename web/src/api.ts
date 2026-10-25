// Typed client for the Matinee server API. The session lives in an HttpOnly
// cookie, so requests just need to be same-origin.

export type User = { id: number; username: string; isAdmin: boolean }

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
}

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
  setup: () => request<{ needsSetup: boolean }>('GET', '/api/setup'),
  createAdmin: (username: string, password: string) =>
    request<{ user: User }>('POST', '/api/setup', { username, password }),
  login: (username: string, password: string) =>
    request<{ user: User }>('POST', '/api/auth/login', { username, password }),
  logout: () => request<void>('POST', '/api/auth/logout'),
  me: () => request<User>('GET', '/api/me'),

  libraries: () => request<Library[]>('GET', '/api/libraries'),
  createLibrary: (name: string, kind: string, paths: string[]) =>
    request<Library>('POST', '/api/libraries', { name, kind, paths }),
  deleteLibrary: (id: number) => request<void>('DELETE', `/api/libraries/${id}`),
  scanLibrary: (id: number) => request<void>('POST', `/api/libraries/${id}/scan`),
  libraryItems: (id: number) => request<Item[]>('GET', `/api/libraries/${id}/items`),
  browse: (path: string) =>
    request<{ path: string; parent: string; dirs: string[] }>('GET', `/api/fs?path=${encodeURIComponent(path)}`),

  users: () => request<User[]>('GET', '/api/users'),
  createUser: (username: string, password: string, isAdmin: boolean) =>
    request<User>('POST', '/api/users', { username, password, isAdmin }),

  home: () => request<{ continueWatching: Item[]; recentlyAdded: Item[] }>('GET', '/api/home'),
  search: (q: string) => request<Item[]>('GET', `/api/search?q=${encodeURIComponent(q)}`),
  item: (id: number) => request<ItemDetail>('GET', `/api/items/${id}`),
  children: (id: number) => request<Item[]>('GET', `/api/items/${id}/children`),
  setProgress: (id: number, positionMs: number, played = false) =>
    request<void>('PUT', `/api/items/${id}/progress`, { positionMs, played }),
  startHls: (id: number, startSec = 0, burnSubtitle?: string) =>
    request<{ url: string; startSec: number }>(
      'POST',
      `/api/items/${id}/hls?start=${startSec}${burnSubtitle ? `&subtitle=${encodeURIComponent(burnSubtitle)}` : ''}`,
    ),
  subtitles: async (id: number, track: string) => {
    const res = await fetch(`/api/items/${id}/subtitles/${encodeURIComponent(track)}.vtt`, { credentials: 'same-origin' })
    if (!res.ok) throw new ApiError(res.status, "Couldn't load these subtitles")
    return res.text()
  },
}

export const imageUrl = (id: number, type: 'poster' | 'backdrop') => `/api/items/${id}/image/${type}`
export const fileUrl = (id: number) => `/api/items/${id}/file`
