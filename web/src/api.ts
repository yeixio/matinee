// Typed client for the Matinee server API (/api/v1, documented in
// internal/api/openapi.yaml). The session lives in an HttpOnly cookie, so
// requests just need to be same-origin.

// guest is the shared account of a device at home that hasn't signed in.
export type User = { id: number; username: string; isAdmin: boolean; guest?: boolean }
// requests: who besides admins may add titles through Radarr and Sonarr.
export type RequestMode = 'off' | 'approve' | 'auto'
export type Settings = {
  requireSignInAtHome: boolean
  requests: RequestMode
  // auto, off (the CPU), or an Accelerator's id.
  hardwareAcceleration: string
  // problem: why a GPU the server can see couldn't be used, when none works.
  hardware: { active: Accelerator | null; available: Accelerator[]; detecting: boolean; problem?: string }
  // Offer TMDB's YouTube trailers next to trailer files.
  remoteTrailers: boolean
}
// A GPU backend that passed its test encode on the server.
export type Accelerator = { id: string; name: string; device?: string; detail?: string; decode: boolean }

// A trailer file next to the media (local), which plays like the title
// itself, or a YouTube trailer from TMDB.
export type Trailer = {
  id: string
  name: string
  source: 'local' | 'youtube'
  mimeType?: string
  container?: string
  videoCodec?: string
  audioCodec?: string
  durationMs?: number
  key?: string
  url?: string
}

export type ArrApp = 'radarr' | 'sonarr'
// A Radarr or Sonarr connection. The API key stays on the server.
export type Integration = { connected: boolean; url: string; hasApiKey: boolean; qualityProfileId: number; rootFolder: string }
export type IntegrationConfig = { url: string; apiKey?: string; qualityProfileId?: number; rootFolder?: string }
export type QualityProfile = { id: number; name: string }
export type ConnectionCheck = {
  url: string // the address that works, with the app's URL base if it redirected to one
  appName: string
  version: string
  qualityProfiles: QualityProfile[]
  rootFolders: { path: string; freeSpace: number }[]
}

export type TitleKind = 'movie' | 'show'
export type RequestOptions = {
  movies: boolean
  shows: boolean
  canRequest: boolean
  autoApprove: boolean
  canReport: boolean
  qualityProfiles?: Partial<Record<TitleKind, QualityProfile[]>>
  defaultQualityProfile?: Partial<Record<TitleKind, number>>
  pendingRequests?: number
  openIssues?: number
}

// A movie or show found to ask for. status: in a library (available), in
// Radarr or Sonarr still to download (added), or waiting for an admin (requested).
export type Title = {
  kind: TitleKind
  tmdbId?: number
  tvdbId?: number
  title: string
  year?: number
  overview?: string
  poster?: string
  status?: 'available' | 'added' | 'requested'
  itemId?: number
  requestId?: string
}

export type TitleRequest = {
  id: string
  kind: TitleKind
  tmdbId?: number
  tvdbId?: number
  title: string
  year?: number
  poster?: string
  qualityProfileId?: number
  state: 'pending' | 'approved' | 'declined' | 'failed'
  error?: string
  userId: number
  username: string
  createdAt: number
  updatedAt: number
  itemId?: number
}

export type Upcoming = {
  kind: 'movie' | 'episode'
  title: string
  year?: number
  showTitle?: string
  season?: number
  episode?: number
  date: string
  release: 'cinemas' | 'digital' | 'physical' | 'airs'
  overview?: string
  poster?: string
  tmdbId?: number
}

export type IssueKind = 'video' | 'audio' | 'subtitles' | 'wrong' | 'other'
export type Issue = {
  id: string
  itemId: number
  item?: Item
  kind: IssueKind
  note?: string
  resolved: boolean
  resolution?: string
  replaceable: boolean
  userId: number
  username: string
  createdAt: number
  resolvedAt?: number
}

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
  /** Movies and shows: genres from the online match. */
  genres?: string[]
  // What the background analysis found, like 4k, hdr, dolby-vision, atmos,
  // 5.1 (see openapi.yaml). Shows and seasons get their episodes' tags.
  mediaTags?: string[]
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

/** One of the home screen's genre rows, a different few each day. */
export type HomeRow = { id: string; title: string; genre: string; items: Item[] }

export type HDR = 'dolby-vision' | 'hdr10+' | 'hdr10' | 'hlg'

export type VideoStream = {
  index: number
  codec: string
  profile?: string
  width: number
  height: number
  resolution: string
  frameRate?: number
  bitrate?: number
  bitDepth?: number
  pixFmt?: string
  colorPrimaries?: string
  colorTransfer?: string
  colorSpace?: string
  hdr?: HDR
  // Dolby Vision only: what players without it show instead.
  hdrBase?: Exclude<HDR, 'dolby-vision'>
  dvProfile?: number
  dvLevel?: number
  language?: string
  title?: string
  default?: boolean
}

export type AudioStream = {
  index: number
  codec: string
  profile?: string
  channels: number
  channelLayout?: string
  sampleRate?: number
  bitDepth?: number
  bitrate?: number
  atmos?: boolean
  dtsx?: boolean
  language?: string
  title?: string
  default?: boolean
  forced?: boolean
  commentary?: boolean
  descriptive?: boolean
}

export type EmbeddedSubtitle = {
  index: number
  codec: string
  language?: string
  title?: string
  default?: boolean
  forced?: boolean
  sdh?: boolean
}

// A file's streams in detail, from the server's background analysis.
export type MediaInfo = {
  container: string
  size: number
  durationMs: number
  bitrate?: number
  video: VideoStream[]
  audio: AudioStream[]
  subtitles: EmbeddedSubtitle[]
  chapters?: number
  error?: string
  analyzedAt: number
}

export type ItemDetail = Item & {
  mimeType?: string
  subtitles?: SubtitleTrack[]
  parents: Item[]
  // Left out until the file is analyzed; stale while it changed and waits to be analyzed again.
  mediaInfo?: MediaInfo
  mediaInfoStale?: boolean
  filePath?: string // admins only
  fileSize?: number
}

export type AnalysisMode = 'gentle' | 'normal' | 'off'
export type AnalysisStatus = {
  mode: AnalysisMode
  state: 'analyzing' | 'waiting' | 'slowDisk' | 'idle' | 'off'
  total: number
  done: number
  unreadable: number
  current?: string
}

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

  analysis: () => request<AnalysisStatus>('GET', '/api/v1/analysis'),
  setAnalysisMode: (mode: AnalysisMode) => request<AnalysisStatus>('PUT', '/api/v1/analysis', { mode }),
  reanalyze: (id: number) => request<void>('POST', `/api/v1/items/${id}/analyze`),
  settings: () => request<Settings>('GET', '/api/v1/settings'),
  saveSettings: (s: Partial<Omit<Settings, 'hardware'>>) => request<Settings>('PUT', '/api/v1/settings', s),
  users: () => request<User[]>('GET', '/api/v1/users'),
  createUser: (username: string, password: string, isAdmin: boolean) =>
    request<User>('POST', '/api/v1/users', { username, password, isAdmin }),

  integrations: () => request<Record<ArrApp, Integration>>('GET', '/api/v1/integrations'),
  testIntegration: (app: ArrApp, c: IntegrationConfig) => request<ConnectionCheck>('POST', `/api/v1/integrations/${app}/test`, c),
  saveIntegration: (app: ArrApp, c: IntegrationConfig) => request<Integration>('PUT', `/api/v1/integrations/${app}`, c),
  deleteIntegration: (app: ArrApp) => request<void>('DELETE', `/api/v1/integrations/${app}`),
  requestOptions: () => request<RequestOptions>('GET', '/api/v1/requests/options'),
  searchTitles: (kind: TitleKind, q: string) =>
    request<Title[]>('GET', `/api/v1/requests/search?kind=${kind}&q=${encodeURIComponent(q)}`),
  requests: (all = false) => request<TitleRequest[]>('GET', `/api/v1/requests${all ? '?all=1' : ''}`),
  createRequest: (t: Title, qualityProfileId?: number) =>
    request<TitleRequest>('POST', '/api/v1/requests', { kind: t.kind, tmdbId: t.tmdbId, tvdbId: t.tvdbId, qualityProfileId }),
  approveRequest: (id: string, qualityProfileId?: number) =>
    request<TitleRequest>('POST', `/api/v1/requests/${id}/approve`, { qualityProfileId }),
  declineRequest: (id: string) => request<TitleRequest>('POST', `/api/v1/requests/${id}/decline`),
  deleteRequest: (id: string) => request<void>('DELETE', `/api/v1/requests/${id}`),
  upcoming: (days = 30) => request<{ items: Upcoming[]; unavailable?: string[] }>('GET', `/api/v1/upcoming?days=${days}`),
  itemQuality: (id: number) =>
    request<{ app: ArrApp; qualityProfileId: number; qualityProfiles: QualityProfile[] }>('GET', `/api/v1/items/${id}/quality`),
  setItemQuality: (id: number, qualityProfileId: number) =>
    request<{ app: ArrApp; qualityProfileId: number; qualityProfiles: QualityProfile[] }>('PUT', `/api/v1/items/${id}/quality`, { qualityProfileId }),
  reportIssue: (id: number, kind: IssueKind, note: string) => request<Issue>('POST', `/api/v1/items/${id}/issues`, { kind, note }),
  issues: (all = false) => request<Issue[]>('GET', `/api/v1/issues${all ? '?all=1' : ''}`),
  resolveIssue: (id: string) => request<void>('POST', `/api/v1/issues/${id}/resolve`),
  replaceIssueFile: (id: string) => request<void>('POST', `/api/v1/issues/${id}/replace`),

  // newReleases and rows come from servers new enough to have them.
  home: () =>
    request<{ continueWatching: Item[]; watchList: Item[]; recentlyAdded: Item[]; newReleases?: Item[]; rows?: HomeRow[] }>('GET', '/api/v1/home'),
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
  trailers: (id: number) => request<Trailer[]>('GET', `/api/v1/items/${id}/trailers`),
  startTrailerHls: (id: number, trailer: string) =>
    request<{ url: string; startSec: number }>('POST', `/api/v1/items/${id}/trailers/${encodeURIComponent(trailer)}/hls`),
  subtitles: async (id: number, track: string) => {
    const res = await fetch(`/api/v1/items/${id}/subtitles/${encodeURIComponent(track)}.vtt`, { credentials: 'same-origin' })
    if (!res.ok) throw new ApiError(res.status, "Couldn't load these subtitles")
    return res.text()
  },
}

export const imageUrl = (id: number, type: 'poster' | 'backdrop') => `/api/v1/items/${id}/image/${type}`
export const fileUrl = (id: number) => `/api/v1/items/${id}/file`
export const trailerFileUrl = (id: number, trailer: string) => `/api/v1/items/${id}/trailers/${encodeURIComponent(trailer)}/file`
