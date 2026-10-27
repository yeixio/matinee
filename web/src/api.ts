// Typed client for the Matinee server API (/api/v1, documented in
// internal/api/openapi.yaml). The session lives in an HttpOnly cookie, so
// requests just need to be same-origin.

import { type ItemFilter, toSearchParams } from './filter'
import i18n from './i18n'

// guest is the shared account of a device at home that hasn't signed in, or
// one of its profiles. Profiles have accountId; kids profiles have kidsLevel
// (see profiles.ts).
export type User = {
  id: number
  username: string
  isAdmin: boolean
  guest?: boolean
  accountId?: number
  kidsLevel?: number
  avatar?: string
  hasPin?: boolean
}
// One entry in "Who's watching?". The account itself comes first.
export type ProfileChoice = { id: number; name: string; avatar?: string; kidsLevel?: number; hasPin: boolean; account?: boolean }
// A profile as admins manage it. libraries: library id → all or rated (kids only).
export type LibraryMode = 'all' | 'rated'
export type Profile = User & { libraries: Record<string, LibraryMode> }
export type ProfileChanges = {
  accountId?: number
  name?: string
  avatar?: string
  kidsLevel?: number
  pin?: string
  libraries?: Record<string, LibraryMode>
}
export type ManagedProfiles = {
  accounts: { account: User; profiles: Profile[] }[]
  avatars: string[]
  levels: { level: number; name: string }[]
}
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
  // A row of titles for a holiday on Home in the weeks before it.
  holidayRows: boolean
  // The theme people see until they pick their own.
  theme: string
  // Theme files on the server that couldn't be loaded, and why.
  themeProblems: string[]
  // Open a port on the router so the apps reach the server away from home.
  remoteAccess: boolean
  // The router port (443 or 80) an admin forwarded by hand; 0 asks the router.
  remoteForwardedPort: number
  // How remote access is doing; missing when the server doesn't offer it.
  remote?: RemoteStatus
  // The address set with MATINEE_PUBLIC_URL, which the apps use away from home.
  publicURL?: string
  // publicURL's name doesn't resolve to a public address: it only works at home.
  publicURLHomeOnly?: boolean
  // How library folders are watched for new files.
  libraryWatch: WatchMode
  // Set on the server's command line: how often polled folders are checked,
  // and how often every library is rescanned in full (0 for never).
  watchPollSeconds: number
  scanIntervalSeconds: number
  // Where the TMDB key in use comes from, and its last four characters.
  // The key itself is never sent; an admin changes it with tmdbKey.
  tmdb: { source: 'settings' | 'environment' | 'built-in' | 'none'; hint?: string }
}

// sharedAddress: the router has no public address of its own, so the internet
// can't reach the server until the provider gives it one.
// findable: the apps can look the server up if its address changes.
// listenPort: the server's remote access port, where a hand-made forward goes.
export type RemoteStatus = {
  enabled: boolean
  state: 'off' | 'working' | 'ready' | 'failed'
  url?: string
  router?: string
  publicIP?: string
  problem?: string
  sharedAddress?: boolean
  forwardedPort?: number
  listenPort?: number
  findable?: boolean
  // The Matinee Connect subscription that pays for being findable; missing
  // when the server uses a lookup service that needs none.
  connect?: ConnectStatus
  certificateExpires?: string
  checkedAt?: string
}

// checkout: a checkout is open, waiting for payment.
export type ConnectStatus = { state: 'none' | 'checkout' | 'active' | 'ended'; until?: string; checkout?: string; problem?: string }
export type ConnectPlan = { id: 'monthly' | 'yearly'; price: string; interval: 'month' | 'year' }

// auto: change notifications on local disks, a quick check on network shares;
// poll: the quick check everywhere; off: only the full rescan.
export type WatchMode = 'auto' | 'poll' | 'off'

// How one library folder is watched. reason says why a folder is polled.
export type FolderWatch = { path: string; method: 'events' | 'polling' | 'off'; reason?: string }

// A theme as the server sends it, every colour filled in. See docs/themes.md.
export type Theme = {
  id: string
  name: string
  description?: string
  author?: string
  scheme: 'dark' | 'light'
  builtIn: boolean
  fonts: { display: string; body: string }
  colors: Record<ThemeColor, string>
}
export type ThemeColor =
  | 'ink' | 'surface' | 'raised' | 'line' | 'text' | 'muted' | 'faint' | 'accent' | 'accentStrong' | 'onAccent'
  | 'danger' | 'glow' | 'glass' | 'glassFocused' | 'scrim' | 'track' | 'shade' | 'veil'
export type ThemeList = { default: string; themes: Theme[] }
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
  // Overseerr is connected, so there's a Discover tab. Older servers leave it out.
  discover?: boolean
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

// A movie or show from Overseerr, for the Discover tab. status: in a library
// (available, see itemId), on its way (added), or waiting for approval in
// Overseerr (requested).
export type DiscoverTitle = {
  kind: TitleKind
  tmdbId: number
  title: string
  year?: number
  overview?: string
  poster?: string
  backdrop?: string
  rating?: number
  status?: 'available' | 'added' | 'requested'
  itemId?: number
}

// more: the row has more titles than it shows (see api.discoverRow).
export type DiscoverRow = { id: string; title: string; because?: string; items: DiscoverTitle[]; more?: boolean }

export type OverseerrConnection = { connected: boolean; url: string; hasApiKey: boolean; requestAs: number }
export type OverseerrConfig = { url: string; apiKey?: string; requestAs?: number }
export type OverseerrUser = { id: number; name: string }
export type OverseerrCheck = { url: string; version: string; owner: OverseerrUser; users: OverseerrUser[] }

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

export type FilterValue = { value: string; label?: string; count: number }
export type FilterPerson = { id: number; name: string; hasPhoto: boolean; count: number }

// What a library can be filtered by: the values its titles have, most common first.
export type FilterOptions = {
  genres: FilterValue[]
  tags: FilterValue[]
  studios: FilterValue[]
  countries: FilterValue[]
  languages: FilterValue[]
  certifications: FilterValue[]
  collections: FilterValue[]
  directors: FilterPerson[]
  cast: FilterPerson[]
  yearMin?: number
  yearMax?: number
  hasRatings: boolean
  hasRuntimes: boolean
  hasReleased: boolean
}

export type Library = {
  id: number
  name: string
  kind: 'movies' | 'tv' | 'music' | string
  paths: string[]
  scanning: boolean
  // Empty until the server has set up watching, just after it starts.
  watch: FolderWatch[]
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
  // label is in English; subtitleLabel (media.ts) makes one in the page's language.
  label: string
  note?: string
  language?: string
  codec: string
  forced?: boolean
  default?: boolean
  sdh?: boolean
  external?: boolean
  // Picture-based subtitles the server draws into a transcode.
  burnIn?: boolean
}

/** One of the home screen's rows: a holiday coming up, a pinned collection, or a genre (a different few each day). */
// A row of titles. more: it has more than it shows, and api.row(id) has
// them all. relatedTo is the title a related:{id} row is like.
export type HomeRow = {
  id: string
  title: string
  genre: string
  collectionId?: string
  starter?: string
  holiday?: string
  relatedTo?: string
  items: Item[]
  more?: boolean
}

/** The orders a collection can have; "" is its own (the filter's, or the order titles were added in). */
export type CollectionSort = '' | 'title' | 'added' | 'released' | 'year' | 'rating' | 'runtime' | 'lastPlayed' | 'shuffle'

/**
 * A collection: smart ones are a saved library filter (the same fields as the
 * library page's query), manual ones are kept by hand. Shared ones have no owner.
 */
export type Collection = {
  id: string
  name: string
  kind: 'smart' | 'manual'
  ownerId?: number
  shared: boolean
  filter?: ItemFilter
  sort?: CollectionSort
  order?: 'asc' | 'desc'
  pinHome: boolean
  pinLibrary?: number
  starter?: string
  createdAt: number
  updatedAt: number
  /** How many titles it has in all; items may be only the first few. */
  count: number
  items: Item[]
  canEdit: boolean
  contains?: boolean
}

export type CollectionInput = {
  name: string
  kind?: 'smart' | 'manual'
  filter?: ItemFilter
  sort?: CollectionSort
  order?: 'asc' | 'desc' | ''
  pinHome?: boolean
  pinLibrary?: number | null
  shared?: boolean
}

/** One of TMDB's collections, such as a trilogy, with two or more titles in the library. */
export type BoxSet = { id: number; name: string; count: number; itemIds: number[] }

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
  creditsMs?: number // where the closing credits start, when a chapter says so
  error?: string
  analyzedAt: number
}
// The longer details of a movie, show, season or episode, once looked up.
// Seasons and episodes carry their show's genres, certification and networks.
export type Details = {
  tagline?: string
  originalTitle?: string
  originalLanguage?: string
  releaseDate?: string // YYYY-MM-DD; first air date for shows
  endDate?: string
  status?: string
  certification?: string
  genres?: string[]
  studios?: string[]
  networks?: string[]
  countries?: string[]
  languages?: string[]
  budget?: number // US dollars
  revenue?: number
  collection?: { id: number; name: string }
  seasons?: number
  episodes?: number
  imdbId?: string
  awards?: Award[]
  awardSummary?: string // e.g. "Won 4 Academy Awards · 31 wins and 62 nominations"
}
export type Award = { name: string; year?: number; won: boolean; names?: string[] }

// role is the character, or a job such as Director.
export type Credit = { personId: number; name: string; role?: string; department?: string; episodes?: number; hasPhoto: boolean }
// An episode's cast is its season's regulars; guests are its own guest stars.
export type Credits = { cast: Credit[]; guests?: Credit[]; crew: Credit[] }

export type Person = {
  id: number
  name: string
  hasPhoto: boolean
  biography?: string
  birthday?: string
  deathday?: string
  birthplace?: string
  knownFor?: string
  // Titles in the library they're in, newest first.
  titles: { item: Item; roles: string[] }[]
}

// collection: the franchise a movie belongs to, with its titles in the library.
export type Related = { collection?: { id: number; name: string; items: Item[] }; items: Item[]; more?: boolean }

// What to offer when an episode ends: the next episode, or after a show's
// last one a similar title to open.
export type NextUp = { next?: Item; similar?: Item }

// Each person's auto-play choices, on every device.
export type PlaybackPrefs = {
  autoPlay: boolean // offer the next episode with a countdown
  binge: boolean // skip the countdown
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
  details?: Details
  credits?: Credits
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

// message is in the page's language where the catalog has the server's
// error code (errors.json), else the server's English.
export class ApiError extends Error {
  status: number
  code?: string
  constructor(status: number, message: string, code?: string, params?: Record<string, string>) {
    super(code && i18n.exists(`errors:${code}`) ? i18n.t(`errors:${code}`, params) : message)
    this.status = status
    this.code = code
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
    let code: string | undefined
    let params: Record<string, string> | undefined
    try {
      const body = await res.json()
      message = body.error ?? message
      code = body.code
      params = body.params
    } catch {
      // not JSON
    }
    throw new ApiError(res.status, message, code, params)
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
  libraryItems: (id: number, filter: ItemFilter = {}) => {
    const q = toSearchParams({ ...filter, library: undefined }).toString()
    return request<Item[]>('GET', `/api/v1/libraries/${id}/items${q ? `?${q}` : ''}`)
  },
  filterItems: (filter: ItemFilter) => request<Item[]>('GET', `/api/v1/items?${toSearchParams(filter)}`),
  libraryFilters: (id: number) => request<FilterOptions>('GET', `/api/v1/libraries/${id}/filters`),
  libraryPeople: (id: number, q: string, role?: 'cast' | 'director') =>
    request<FilterPerson[]>('GET', `/api/v1/libraries/${id}/people?${new URLSearchParams({ q, ...(role ? { role } : {}) })}`),
  browse: (path: string) =>
    request<{ path: string; parent: string; dirs: string[] }>('GET', `/api/v1/fs?path=${encodeURIComponent(path)}`),

  analysis: () => request<AnalysisStatus>('GET', '/api/v1/analysis'),
  setAnalysisMode: (mode: AnalysisMode) => request<AnalysisStatus>('PUT', '/api/v1/analysis', { mode }),
  reanalyze: (id: number) => request<void>('POST', `/api/v1/items/${id}/analyze`),
  settings: () => request<Settings>('GET', '/api/v1/settings'),
  // tmdbKey replaces the server's TMDB key; "" goes back to the one it started with.
  saveSettings: (s: Partial<Omit<Settings, 'hardware' | 'themeProblems' | 'watchPollSeconds' | 'scanIntervalSeconds' | 'tmdb'>> & { tmdbKey?: string }) =>
    request<Settings>('PUT', '/api/v1/settings', s),
  connectPlans: () => request<{ plans: ConnectPlan[] }>('GET', '/api/v1/settings/connect/plans').then((r) => r.plans),
  connectSubscribe: (plan: ConnectPlan['id']) => request<{ url: string }>('POST', '/api/v1/settings/connect/subscribe', { plan }),
  connectManage: () => request<{ url: string }>('POST', '/api/v1/settings/connect/manage'),
  themes: () => request<ThemeList>('GET', '/api/v1/themes'),
  // theme: "" follows the server's default.
  myTheme: () => request<{ theme: string }>('GET', '/api/v1/me/theme'),
  setMyTheme: (theme: string) => request<{ theme: string }>('PUT', '/api/v1/me/theme', { theme }),
  // language: "" follows the browser.
  myLanguage: () => request<{ language: string }>('GET', '/api/v1/me/language'),
  setMyLanguage: (language: string) => request<{ language: string }>('PUT', '/api/v1/me/language', { language }),
  playback: () => request<PlaybackPrefs>('GET', '/api/v1/me/playback'),
  setPlayback: (p: PlaybackPrefs) => request<PlaybackPrefs>('PUT', '/api/v1/me/playback', p),
  users: () => request<User[]>('GET', '/api/v1/users'),
  profiles: () => request<{ current: number; profiles: ProfileChoice[] }>('GET', '/api/v1/profiles'),
  selectProfile: (id: number, pin?: string) => request<{ user: User }>('POST', '/api/v1/profiles/select', { id, pin }),
  manageProfiles: () => request<ManagedProfiles>('GET', '/api/v1/profiles/manage'),
  createProfile: (p: ProfileChanges) => request<Profile>('POST', '/api/v1/profiles', p),
  updateProfile: (id: number, p: ProfileChanges) => request<Profile>('PUT', `/api/v1/profiles/${id}`, p),
  deleteProfile: (id: number) => request<void>('DELETE', `/api/v1/profiles/${id}`),
  createUser: (username: string, password: string, isAdmin: boolean) =>
    request<User>('POST', '/api/v1/users', { username, password, isAdmin }),
  setAdmin: (id: number, isAdmin: boolean) => request<User>('PATCH', `/api/v1/users/${id}`, { isAdmin }),
  // Signs the person out on every device.
  resetPassword: (id: number, password: string) => request<void>('PUT', `/api/v1/users/${id}/password`, { password }),
  deleteUser: (id: number) => request<void>('DELETE', `/api/v1/users/${id}`),
  // Signs out every other device of the signed-in user.
  changePassword: (current: string, next: string) =>
    request<void>('PUT', '/api/v1/me/password', { current, new: next }),

  integrations: () => request<Record<ArrApp, Integration>>('GET', '/api/v1/integrations'),
  testIntegration: (app: ArrApp, c: IntegrationConfig) => request<ConnectionCheck>('POST', `/api/v1/integrations/${app}/test`, c),
  saveIntegration: (app: ArrApp, c: IntegrationConfig) => request<Integration>('PUT', `/api/v1/integrations/${app}`, c),
  deleteIntegration: (app: ArrApp) => request<void>('DELETE', `/api/v1/integrations/${app}`),
  requestOptions: () => request<RequestOptions>('GET', '/api/v1/requests/options'),
  overseerr: () => request<OverseerrConnection>('GET', '/api/v1/integrations/overseerr'),
  testOverseerr: (c: OverseerrConfig) => request<OverseerrCheck>('POST', '/api/v1/integrations/overseerr/test', c),
  saveOverseerr: (c: OverseerrConfig) => request<OverseerrConnection>('PUT', '/api/v1/integrations/overseerr', c),
  deleteOverseerr: () => request<void>('DELETE', '/api/v1/integrations/overseerr'),
  discover: () => request<{ rows: DiscoverRow[]; canRequest: boolean }>('GET', '/api/v1/discover'),
  // A page (from 1) of one of Discover's rows, for the row's own page.
  discoverRow: (id: string, page: number) =>
    request<{ row: DiscoverRow; page: number; pages: number; canRequest: boolean }>('GET', `/api/v1/discover/rows/${encodeURIComponent(id)}?page=${page}`),
  discoverSearch: (q: string) => request<DiscoverTitle[]>('GET', `/api/v1/discover/search?q=${encodeURIComponent(q)}`),
  discoverRequest: (t: DiscoverTitle) => request<DiscoverTitle>('POST', '/api/v1/discover/requests', { kind: t.kind, tmdbId: t.tmdbId }),
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

  // newReleases and rows come from servers new enough to have them. more
  // says which of the lists have more titles than they show.
  home: () =>
    request<{
      continueWatching: Item[]
      watchList: Item[]
      recentlyAdded: Item[]
      newReleases?: Item[]
      rows?: HomeRow[]
      more?: Partial<Record<'continueWatching' | 'recentlyAdded' | 'newReleases' | 'watchList', boolean>>
    }>('GET', '/api/v1/home'),
  // A row with all its titles, by its id (see HomeRow).
  row: (id: string) => request<HomeRow>('GET', `/api/v1/rows/${encodeURIComponent(id)}`),
  search: (q: string) => request<Item[]>('GET', `/api/v1/search?q=${encodeURIComponent(q)}`),
  item: (id: number) => request<ItemDetail>('GET', `/api/v1/items/${id}`),
  children: (id: number) => request<Item[]>('GET', `/api/v1/items/${id}/children`),
  related: (id: number) => request<Related>('GET', `/api/v1/items/${id}/related`),
  next: (id: number) => request<NextUp>('GET', `/api/v1/items/${id}/next`),
  person: (id: number) => request<Person>('GET', `/api/v1/people/${id}`),
  collections: (q: { library?: number; item?: number } = {}) => {
    const p = new URLSearchParams()
    if (q.library) p.set('library', String(q.library))
    if (q.item) p.set('item', String(q.item))
    return request<{ collections: Collection[]; boxSets: BoxSet[] }>('GET', `/api/v1/collections${p.toString() ? `?${p}` : ''}`)
  },
  collection: (id: string) => request<Collection>('GET', `/api/v1/collections/${encodeURIComponent(id)}`),
  createCollection: (c: CollectionInput) => request<Collection>('POST', '/api/v1/collections', c),
  updateCollection: (id: string, c: CollectionInput) => request<Collection>('PUT', `/api/v1/collections/${encodeURIComponent(id)}`, c),
  deleteCollection: (id: string) => request<void>('DELETE', `/api/v1/collections/${encodeURIComponent(id)}`),
  addToCollection: (id: string, item: number) => request<void>('PUT', `/api/v1/collections/${encodeURIComponent(id)}/items/${item}`),
  removeFromCollection: (id: string, item: number) =>
    request<void>('DELETE', `/api/v1/collections/${encodeURIComponent(id)}/items/${item}`),
  boxSet: (id: number) => request<{ id: number; name: string; items: Item[] }>('GET', `/api/v1/box-sets/${id}`),
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
    if (!res.ok) throw new ApiError(res.status, i18n.t('web:player.subtitlesFailed'))
    return res.text()
  },
}

export const imageUrl = (id: number, type: 'poster' | 'backdrop') => `/api/v1/items/${id}/image/${type}`
export const personImageUrl = (id: number) => `/api/v1/people/${id}/image`
export const fileUrl = (id: number) => `/api/v1/items/${id}/file`
export const trailerFileUrl = (id: number, trailer: string) => `/api/v1/items/${id}/trailers/${encodeURIComponent(trailer)}/file`
