// Typed client for the Matinee server API (/api/v1, documented in
// internal/api/openapi.yaml). The session lives in an HttpOnly cookie, so
// requests just need to be same-origin.

import { type ItemFilter, toSearchParams } from './filter'
import i18n from './i18n'
import type { LevelsJSON } from './player/levels'
import type { Lyrics } from './player/lyricLines'
import type { Thumbnails } from './player/thumbnails'
import type { SubtitlePrefs } from './player/subtitlePrefs'
import { heardRestarting, RESTARTING_HEADER } from './player/reconnect'

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
  // An account made from an invite, or one of its profiles: it sees only
  // the libraries it was given.
  friend?: boolean
}
// An account in People; friends come with the libraries they see.
export type Account = User & { libraries?: number[] }
// A link an admin sent to let one friend in (see Friends in Settings).
export type Invite = {
  code: string
  url: string
  name?: string
  libraries: number[]
  createdBy?: string
  createdAt: number
  expiresAt: number
  usedBy?: string
  usedAt?: number
  state: 'open' | 'used' | 'expired'
}
// What an invite page shows before someone makes their account.
export type InviteInfo = {
  server: string
  invitedBy: string
  name?: string
  libraries: { id: number; name: string; kind: string }[]
  expiresAt: number
  remoteURL: string
}
// A Watch together session (see player/together.ts). Playback was at
// positionMs at updatedAt on the server's clock, and has moved on since
// unless paused.
export type TogetherMember = { clientId: string; userId: number; name: string; device?: string; platform?: string; host?: boolean }
export type TogetherAction = 'start' | 'join' | 'leave' | 'play' | 'pause' | 'seek' | 'item'
export type TogetherState = {
  code: string
  version: number
  itemId: number
  positionMs: number
  paused: boolean
  updatedAt: number
  serverTime: number
  members: TogetherMember[]
  last?: { action: TogetherAction; by: string; clientId: string; at: number }
  ended?: boolean
}
export type TogetherRequest = {
  clientId: string
  device?: string
  platform?: string
  itemId?: number
  positionMs?: number
  paused?: boolean
  action?: TogetherAction
  resume?: string
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
  // How converted HDR is made SDR, and what this server's FFmpeg can do it with.
  toneMapping: 'hable' | 'mobius' | 'reinhard' | 'off'
  toneMapTools?: { zscale: boolean; placebo: boolean }
  // Offer TMDB's YouTube trailers next to trailer files.
  remoteTrailers: boolean
  // A row of titles for a holiday on Home in the weeks before it.
  holidayRows: boolean
  // Other editions and better copies from Box Office always wait for an admin.
  boxOfficeAsksNeedApproval?: boolean
  // Look up songs' words on LRCLIB when there are none next to the song or in its tags.
  onlineLyrics: boolean
  // Line subtitle files up with the audio the first time a video plays with them.
  subtitleSync?: boolean
  // Transcodes become adaptive streams of qualitySteps, and people can pick
  // a quality in the player (see GET /streaming).
  adaptiveQuality: boolean
  qualitySteps: string[]
  // The best quality step away from home, adaptive or not; '' for no cap.
  remoteQualityCap: string
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

// A special feature from a file next to the title: behind the scenes, a
// deleted scene, a featurette and so on. It plays like a trailer file. name
// is empty when the file has no name of its own, so it's called by its kind.
export type ExtraKind = 'trailer' | 'behindTheScenes' | 'deleted' | 'featurette' | 'interview' | 'scene' | 'short' | 'other'
export type Extra = {
  id: string
  kind: ExtraKind
  name: string
  mimeType?: string
  container?: string
  videoCodec?: string
  audioCodec?: string
  durationMs?: number
}

export type ArrApp = 'radarr' | 'sonarr'
// A Radarr or Sonarr connection. The API key stays on the server.
// Notifications (see internal/api/notifications.go): Box Office telling
// people what they asked for is on its way or ready, and admins about
// downloads that failed.
export type AppNotification = {
  id: string
  event: string
  level: 'info' | 'warning' | 'critical'
  title: string
  body?: string
  kind?: 'movie' | 'show'
  tmdbId?: number
  tvdbId?: number
  poster?: string
  itemId?: number // the title in the library, once it's there
  createdAt: number // seconds, when it last came
  firstAt?: number // seconds, when it first came, for one that came more than once
  repeats?: number // how many times it came, when more than once
  read: boolean
}
export type NotifyAdmins = 'all' | 'problems' | 'off'
export type NotificationSettings = {
  admins: NotifyAdmins
  boxOffice: { app: ArrApp; id?: string; checkedAt: number; error?: string }[]
}
export type BoxOfficeConnection = { connected: boolean; movies: boolean; shows: boolean; url: string; hasApiKey: boolean; id?: string; error?: string }
export type BoxOfficeConfig = { url: string; apiKey?: string }
export type BoxOfficeCheck = { url: string; id: string; version: string; movies: ConnectionCheck; shows: ConnectionCheck }
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
  // What Box Office offers beyond Radarr and Sonarr; absent without it.
  boxOffice?: BoxOfficePairing
}

// What a Box Office connected to Radarr or Sonarr offers: titles coming
// soon with their progress, other editions of movies, and better or
// replacement copies. asksNeedApproval: what this account asks it waits
// for an admin.
export type BoxOfficePairing = {
  progress: boolean
  editions: boolean
  upgrades: boolean
  upgradeMovies: boolean
  upgradeShows: boolean
  asksNeedApproval: boolean
}

// A title on its way from Box Office: searching (asked for, not found yet),
// or where its download is.
export type ComingSoonStage =
  | 'searching'
  | 'queued'
  | 'downloading'
  | 'paused'
  | 'waiting'
  | 'checking'
  | 'repairing'
  | 'unpacking'
  | 'importing'
  | 'waitingToMove'
  | 'imported'
  | 'failed'
  | 'quarantined'
export type ComingSoonTitle = {
  kind: TitleKind
  tmdbId?: number
  tvdbId?: number
  title: string
  year?: number
  poster?: string
  stage: ComingSoonStage
  percent: number
  etaSeconds?: number
  downloads?: number
  seasonNumber?: number
  episodeNumber?: number
  edition?: string
  mode?: 'replace' | 'alongside'
  message?: string
  itemId?: number
  notifyMe?: boolean
  requested?: boolean
}

export type BoxOfficeEditions = {
  hasFile: boolean
  current?: string
  alongside?: string[]
  editions: { edition: string; releases: number; quality?: string; size?: number; owned?: boolean }[]
}
export type AskResult = { state: 'approved' | 'pending' | 'failed'; request: TitleRequest }

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

// boxOffice: a Box Office fills Discover, so Overseerr isn't used.
export type OverseerrConnection = { connected: boolean; url: string; hasApiKey: boolean; requestAs: number; boxOffice?: boolean }
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
  // With Box Office: another edition (edition, replacing the copy or
  // alongside it), a better copy, or another for a bad one (reason).
  ask?: 'edition' | 'better' | 'replace'
  edition?: string
  mode?: 'replace' | 'alongside'
  reason?: string
  seasonNumber?: number
  episodeNumber?: number
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
  // Where a movie or TV library's details come from first: '' .nfo files
  // next to the media, then online; 'online' online first; 'localOnly'
  // only the files.
  metadataMode?: MetadataMode
  scanning: boolean
  // Empty until the server has set up watching, just after it starts.
  watch: FolderWatch[]
}

export type MetadataMode = '' | 'online' | 'localOnly'

/** An admin's change to a title's details; fields left out stay. */
export type DetailsEdit = { title?: string; year?: number; overview?: string; genres?: string[]; rating?: number }

export type GaplessStream = { url: string; startSec: number; tracks: { id: number; startSec: number }[] }

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
  // The person's own rating, 1 to 10: half stars out of five.
  myRating?: number
  // An album whose songs play without a gap between them (a DJ mix, a live
  // album), or a song on one: the player joins them into one stream. An
  // album also says whether the household chose (gaplessChoice, missing
  // when Matinee decides) and what Matinee heard (gaplessFound).
  gapless?: boolean
  gaplessChoice?: boolean
  gaplessFound?: boolean
  hasPoster: boolean
  // Set when an admin picked the cover; it goes on the image URL so a cached old one isn't shown.
  posterTag?: string
  hasBackdrop: boolean
  showTitle?: string
  seasonIndex?: number
  // Albums and tracks: the artist; tracks: the album too (its id is parentId).
  artist?: string
  artistId?: number
  album?: string
  // Albums and tracks: the record label the album came out on.
  recordLabel?: string
  // Movies and episodes: the title this file is another version of, and the
  // cut its file name names.
  versionOf?: number
  edition?: string
  // Books and their parts: the author; parts: the book too (its id is
  // parentId). Books name their narrators and place in a series.
  author?: string
  authorId?: number
  book?: string
  narrators?: string[]
  series?: string
  seriesIndex?: number
  // Podcasts and their episodes: who makes the show (author); episodes: the
  // show's title (its id is parentId) and the day it came out, YYYY-MM-DD.
  podcast?: string
  released?: string
  // True until the server has looked the item up online.
  pending?: boolean
  onWatchList?: boolean
  /** Movies and shows: genres from the online match. */
  genres?: string[]
  /** Details an admin edited by hand, which scans and lookups leave alone. */
  lockedFields?: string[]
  /** Details an .nfo file next to the media set. */
  localFields?: string[]
  // What the background analysis found, like 4k, hdr, dolby-vision, atmos,
  // 5.1 (see openapi.yaml). Shows and seasons get their episodes' tags.
  mediaTags?: string[]
  // Items of a plugin's kinds: labelled lines the plugin found, and what it
  // keeps with the item for its player.
  facts?: { label: string; value: string }[]
  pluginData?: Record<string, unknown>
}

// An audiobook with its parts (files) and chapters, all on the book's own
// clock, which runs across the parts (GET /items/{id}/book).
export type BookPart = Item & { startMs: number; mimeType: string }
export type BookChapter = { title: string; startMs: number; endMs: number; partId: number }
export type Book = Item & { parts: BookPart[]; chapters: BookChapter[] }
// A series or a narrator, with a few of its books for artwork.
export type BookGroup = { name: string; books: number; itemIds: number[] }

// A record label in a music library, with a few of its albums for its tile.
// Sites outside the server that aren't answering (see internal/netstate).
export type InternetStatus = {
  offline: boolean
  since?: number
  down: { host: string; since: number; retry: number; error?: string; home?: boolean }[]
}
export type MatchCandidate = { id: number; title: string; year?: number; overview?: string; poster?: string }
export type ItemMatch = { tmdbId: number; locked: boolean; ready: boolean; results: MatchCandidate[] }
export type RecordLabel = { name: string; albums: number; artists: number; covers: Item[] }

// A radio station found in the station directory (Radio Browser); itemId
// is set when the library asked about has it already.
export type FoundStation = {
  name: string
  url: string
  homepage?: string
  image?: string
  tags?: string[]
  country?: string
  codec?: string
  bitrate?: number
  itemId?: number
}

// A plugin as Settings shows it (internal/plugin, docs/guide/plugins).
export type Plugin = {
  id: string
  name: string
  version: string
  api: number
  description?: string
  author?: string
  license?: string
  homepage?: string
  icon?: string
  server?: string
  libraryKinds?: { name: string; label: string; description?: string; extensions: string[]; itemKinds: string[] }[]
  metadata?: string[]
  events?: string[]
  players?: { itemKinds: string[]; entry: string }[]
  settings?: { key: string; label: string; description?: string; secret?: boolean }[]
  permissions?: { network?: string[] }
  enabled: boolean
  running: boolean
  // Why an enabled plugin isn't running.
  problem?: string
  // This server runs the plugin API version it was made for.
  supported: boolean
  // Its settings; secret ones come back as ••••••.
  config: Record<string, string>
  hasIcon: boolean
}
// An official plugin from the catalog; installed is the version here, if any.
export type CatalogPlugin = { id: string; name: string; description: string; version: string; url: string; homepage?: string; installed?: string }
// A player a running plugin has: its page is pluginFileUrl(plugin, entry).
export type PluginPlayer = { plugin: string; itemKinds: string[]; entry: string }
// A kind of library; plugins' kinds have their own label.
export type LibraryKind = { name: string; label?: string; description?: string; plugin?: string }

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
  // ASS/SSA: the player can draw its positions, fonts and effects from the
  // original (api.styledSubtitles, with api.fonts).
  styled?: boolean
  // Found online by someone watching; removable by the person asking.
  found?: boolean
  removable?: boolean
  // Whether its times were lined up with the audio; absent when nobody synced it.
  sync?: SubtitleSync
}

/** What lining a subtitle track up with the audio did (GET …/subtitles/{track}/sync). */
export type SubtitleSync = {
  state: 'syncing' | 'synced' | 'inSync' | 'unsure' | 'failed' | 'off' | ''
  // Synced: each time t shows at t×rate + offsetMs. rate is absent for 1.
  offsetMs?: number
  rate?: number
  auto?: boolean
  reference?: 'audio' | 'subtitles'
}

/** syncVersion changes whenever a track's served times do, to fetch it again. */
export const syncVersion = (t: SubtitleTrack) => (t.sync?.state === 'synced' ? `${t.sync.offsetMs ?? 0}x${t.sync.rate ?? 1}` : '')

/** A font attached to a video, which its ASS subtitles use. */
export type AttachedFont = { index: number; name: string; mimeType?: string }

/** A subtitle file found online (GET /items/{id}/subtitles/search). */
export type FoundSubtitle = {
  id: number
  language?: string
  variant?: string // OpenSubtitles' own code where it says more, such as pt-br
  label: string // in English
  release?: string
  sdh?: boolean
  forced?: boolean
  machine?: boolean // machine translated
  downloads: number
  matchesFile?: boolean // made for this very file
  trusted?: boolean
}

export type SubtitleSearch = { languages: string[]; results: FoundSubtitle[]; have: number[] }

export type OpenSubtitlesConnection = { connected: boolean; hint?: string; username?: string }

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
  // Set on the rows of Recently added split by kind (recentlyAdded:{kind}).
  kind?: 'movie' | 'show' | 'album' | 'book' | 'podcast'
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

/** A playlist: songs someone put in their own order. Each person and profile has their own. */
export type Playlist = {
  id: string
  name: string
  /** Songs the person may play, and how long they last. */
  count: number
  durationMs: number
  /** Up to four different albums on it, for its cover. */
  albumIds: number[]
  createdAt: number
  updatedAt: number
}

/** A song in its place on a playlist; the same song can be on it twice, so each place has its own entryId. */
export type PlaylistEntry = Item & { entryId: string }
export type PlaylistDetail = Playlist & { entries: PlaylistEntry[] }

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
// One picture quality the server streams at (see GET /streaming).
export type QualityStep = { id: string; label: string; width: number; height: number; videoBps: number; audioBps: number }
// Which qualities this device may offer: Auto, steps and Original with
// adaptive quality on; cap is the best it may stream right now (away from
// home), or null.
export type Streaming = { adaptive: boolean; steps: QualityStep[]; cap: QualityStep | null }

/** A video's own marked chapter. */
export type Chapter = { title?: string; startMs: number; endMs: number }

export type EndCredits = { startMs: number; endMs?: number; source: 'chapters' | 'audio' | 'video' }

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
  introStartMs?: number // where a chapter titled Intro or Opening starts
  introEndMs?: number
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
  skipIntros?: boolean // jump past intros without pressing Skip intro
  podcastsPlayOn?: boolean // when a podcast episode ends, play the next one
  subtitles?: SubtitlePrefs // when subtitles come on and how they look
}

// One file of a movie or episode with several; its id plays it.
export type Version = {
  id: number
  edition?: string
  width?: number
  height?: number
  videoCodec?: string
  audioCodec?: string
  container?: string
  mimeType?: string
  mediaTags?: string[]
  durationMs?: number
  size?: number
}

export type ItemDetail = Item & {
  mimeType?: string
  // The files of a movie or episode with more than one, the title's own first.
  versions?: Version[]
  subtitles?: SubtitleTrack[]
  // The person can look for more subtitles online.
  subtitleSearch?: boolean
  parents: Item[]
  // Left out until the file is analyzed; stale while it changed and waits to be analyzed again.
  mediaInfo?: MediaInfo
  mediaInfoStale?: boolean
  // Episodes: where the intro plays, from a chapter or found by matching the
  // season's audio. Left out when unknown.
  intro?: { startMs: number; endMs: number; source: 'chapters' | 'audio' }
  // Episodes and movies: where the closing credits play, from a chapter, the
  // music a season's episodes share, or the picture going dark. endMs is set
  // when something plays after them. Left out when unknown.
  endCredits?: EndCredits
  // Videos: the file's own chapters, for marks on the seek bar. Left out
  // with fewer than two, or until the file is analyzed.
  chapters?: Chapter[]
  filePath?: string // admins only
  fileSize?: number
  details?: Details
  credits?: Credits
  // Podcasts: their feed and whether the person follows them.
  feed?: PodcastFeed
  // Podcast episodes: their web page and type.
  episode?: { link?: string; episodeType: 'full' | 'trailer' | 'bonus' }
}

export type PodcastFeed = {
  feedUrl: string
  link?: string // the show's website
  refreshedAt: number // Unix seconds
  error?: string // admins only: why the last check failed
  following: boolean
  unplayed: number
}
// A show in Podcast Index, with its id when a library has it already.
/** A kind of show to browse in Podcast Index; the name is English, translated by id under podcasts.categories. */
export type PodcastCategory = { id: string; name: string }
export type DirectoryShow = {
  indexId: number
  title: string
  author?: string
  description?: string
  image?: string
  feedUrl: string
  link?: string
  language?: string
  categories?: string[]
  episodes?: number
  itemId?: number
}
export type FollowedShow = Item & { unplayed: number }
export type OpmlImport = { added: number; problems: { title: string; feedUrl: string; error: string }[] }

export type AnalysisMode = 'gentle' | 'normal' | 'off'
// The admin dashboard (Settings → Server health), from GET /admin/health.
export type HealthPoint = {
  t: number
  cpu: number
  hostCpu?: number
  mem: number
  out: number
  req: number
  serverErrors: number
  problems: number
  streams: number
  transcodes: number
}
export type HealthProblem = {
  level: 'warn' | 'error'
  message: string
  detail?: string
  count: number
  first: number
  last: number
  attrs?: LogAttr[]
}
export type LogAttr = { key: string; value: string }
export type LogLine = { seq: number; time: number; level: 'debug' | 'info' | 'warn' | 'error'; message: string; attrs?: LogAttr[] }
export type LogLevel = LogLine['level']
export type ScanReport = { started: number; tookMs: number; added: number; updated: number; removed: number; failed: number; unplayable: number; error?: string }
export type Health = {
  server: {
    name: string
    version: string
    apiVersion: number
    state: string
    ffmpeg?: string
    ffmpegVersion?: string
    publicUrl?: string
    remote?: RemoteStatus
    scanIntervalMs: number
    watching?: string
    internet?: InternetStatus
  }
  system: {
    os: string
    arch: string
    goVersion: string
    hostname?: string
    pid: number
    started: number
    scope: 'group' | 'process'
    cores: number
    cpuLimit?: number
    cpu: number
    hostCpu?: number
    load?: number[]
    memTotal?: number
    memAvailable?: number
    memory: number
    memLimit?: number
    goHeap: number
    goroutines: number
    bytesOut: number
    requests: number
    serverErrors: number
    warnings: number
    errors: number
    history: HealthPoint[]
  }
  database?: {
    kind: 'sqlite' | 'postgres'
    version?: string
    size: number
    schema: number
    pingMs: number
    openConns: number
    inUse: number
    waitCount: number
    waitMs: number
    items: number
    users: number
    libraries: number
  }
  databaseError?: string
  disks: {
    total: number
    free: number
    reachable: boolean
    uses: { use: 'data' | 'transcode' | 'thumbnails' | 'images' | 'downloads' | 'library'; library?: string; path: string }[]
  }[]
  hw?: { active: Accelerator | null; available: Accelerator[]; detecting: boolean; problem?: string }
  streams: {
    id: string
    name: string
    platform: string
    app?: string
    version?: string
    user: { id: number; name: string; guest?: boolean }
    remote?: boolean
    /** Why it last stopped to buffer, in the last few minutes. */
    stalled?: PlaybackCause
    playing: {
      item: { id: number; kind: string; title: string; year?: number; showTitle?: string; season?: number; episode?: number }
      positionMs: number
      durationMs?: number
      paused: boolean
      buffering?: boolean
      transcoding?: boolean
      updatedAt: number
    }
  }[]
  transcodes: {
    id: string
    userId: number
    itemId: number
    started: number
    startSec: number
    lastSeen: number
    kind: 'video' | 'copy' | 'audio'
    /** Why the picture is converted rather than copied. */
    why?: 'codec' | 'hdr' | 'subtitle' | 'quality'
    toneMapped?: boolean
    accel?: string
    accelId?: string
    mode: 'cpu' | 'encode' | 'full'
    quality?: string[]
    burnSub?: boolean
    adaptive?: boolean
    pid?: number
    user?: string
    title?: string
    showTitle?: string
  }[]
  jobs: {
    scans: { libraryId: number; name: string; kind: string; scanning: boolean; since?: number; last?: ScanReport }[]
    analysis?: AnalysisStatus
    downloads: Record<string, number>
    importing?: boolean
  }
  problems: HealthProblem[]
  logLines: number
  logsSince?: number
}
export type LogQuery = { level?: LogLevel; q?: string; before?: number; limit?: number }

export type AnalysisStatus = {
  mode: AnalysisMode
  state: 'analyzing' | 'intros' | 'credits' | 'gapless' | 'thumbnails' | 'waiting' | 'slowDisk' | 'idle' | 'off'
  total: number
  done: number
  unreadable: number
  current?: string // the file; while looking for intros or credits the show and season (or movie); while making thumbnails the video
  // Looking for TV episodes' intros; left out by servers that don't.
  intros?: { on: boolean; total: number; done: number; found: number }
  // Looking for episodes' and movies' end credits; left out by servers that don't.
  credits?: { on: boolean; total: number; done: number; found: number }
  // Making the seek bar's pictures; left out by servers that don't.
  thumbnails?: { mode: ThumbnailMode; total: number; done: number }
}

export type ThumbnailMode = 'all' | 'onPlay' | 'off'

// CoverOption is a cover found online that an admin can pick for an album or book.
export interface CoverOption {
  url: string
  thumb: string
  source: string
  title: string
  detail?: string
}

// message is in the page's language where the catalog has the server's
// error code (errors.json), else the server's English.
// An import from another media server. path is its data folder on the
// server ('' for an uploaded database); changes turn its paths into Matinee's.
export type PathChange = { from: string; to: string; files?: number }
export type ImportRequest = {
  path: string
  changes: PathChange[]
  accounts?: Record<string, number>
  watched?: boolean
  collections?: boolean
  posters?: boolean
  ratings?: boolean
}
export type ImportPreview = {
  database: string
  canPosters: boolean
  accounts: { id: number; name: string; watched: number; inProgress: number; rated: number; found: number; suggested: number }[]
  viewers: { id: number; name: string; profileOf?: string }[]
  files: number
  matched: number
  unmatched: string[]
  suggested: PathChange[]
  collections: number
  posters: number
}
export type ImportResult = {
  watched: number
  inProgress: number
  kept: number
  collections: number
  collectionItems: number
  posters: number
  ratings: number
}
export type ImportStatus = { running: boolean; done: number; total: number; result?: ImportResult; error?: string }

// Admin alerts (Settings → Server health → Alerts), from GET /admin/alerts.
export type AlertRule = 'disk' | 'folder' | 'errors' | 'transcodes' | 'restart' | 'backup' | 'test'
export type Alert = {
  id: string
  rule: AlertRule
  key: string
  level: 'info' | 'warning' | 'critical'
  title: string
  message: string
  time: number
  resolved?: boolean
  sent: number
  failed?: string[]
}
export type RateRule = { on: boolean; count: number; minutes: number }
export type AlertRules = {
  disk: { on: boolean; percent: number }
  folder: { on: boolean }
  errors: RateRule
  transcodes: RateRule
  restart: { on: boolean }
  backup?: { on: boolean }
}
export type AlertEmail = {
  on: boolean
  host: string
  port: number
  security: 'starttls' | 'tls' | 'none'
  username: string
  password?: string
  from: string
  to: string
}
export type WebhookKind = 'discord' | 'ntfy' | 'homeassistant' | 'json'
export type AlertWebhook = { id?: string; kind: WebhookKind; name?: string; url?: string; token?: string }
export type AlertConfig = { email: AlertEmail; webhooks: AlertWebhook[]; rules: AlertRules }
export type AlertSettings = {
  email: AlertEmail & { hasPassword: boolean }
  webhooks: { id: string; kind: WebhookKind; name?: string; hint: string; hasToken?: boolean }[]
  rules: AlertRules
  firing: Alert[]
  history: Alert[]
  links: boolean
}
export type MetricsSettings = { on: boolean; token?: string; path: string }

export class ApiError extends Error {
  status: number
  code?: string
  constructor(status: number, message: string, code?: string, params?: Record<string, string>) {
    super(code && i18n.exists(`errors:${code}`) ? i18n.t(`errors:${code}`, params) : message)
    this.status = status
    this.code = code
  }
}

// Live TV (see the server's api/livetv.go).
export type LiveProgram = {
  channelId: string
  start: number // Unix seconds
  stop: number
  title: string
  subtitle?: string
  description?: string
  category?: string
  image?: string
}
export type LiveChannel = {
  id: string
  sourceId: string
  name: string
  number?: string
  logo?: string
  categories?: string[]
  hidden?: boolean
  broken?: boolean
  checkedAt?: number
}
export type LiveListing = LiveChannel & { now?: LiveProgram; next?: LiveProgram }
export type LiveFilter = { countries?: string[]; categories?: string[]; languages?: string[] }
export type LiveSource = {
  id: string
  name: string
  playlistUrl?: string
  iptvOrg?: LiveFilter
  guideUrl?: string
  createdAt: number
  refreshedAt?: number
  error?: string
  guideAt?: number
  guideError?: string
  channels: number
  broken: number
  hidden: number
  programs: number
}
export type LiveSourceInput = { name?: string; playlistUrl?: string; iptvOrg?: LiveFilter; guideUrl?: string }
export type LiveChoice = { code: string; name: string; flag?: string }
export type IPTVOrgChoices = { countries: LiveChoice[]; categories: LiveChoice[]; languages: LiveChoice[] }

export type WatchedCount = { total: number; played: number }

export type BackupSettings = { everyHours: number; keep: number }
export type Backup = { name: string; size: number; time: number; reason: 'scheduled' | 'manual' | 'upgrade' | 'before-restore' }
/** Copies of the server's database (Settings → Server health → Backups). */
export type Backups = {
  /** False for PostgreSQL, which is backed up with pg_dump. */
  supported: boolean
  database: 'sqlite' | 'postgres'
  dir?: string
  settings: BackupSettings
  backups: Backup[]
  /** The backup that replaces the database at the next start. */
  restore?: string
  lastError?: string
}

export type UpdateChannel = 'release' | 'main'
export type UpdateSettings = { channel: UpdateChannel; auto: boolean }
export type InstallKind = 'argocd' | 'kubernetes' | 'docker' | 'apt' | 'dnf' | 'homebrew' | 'app' | 'binary' | 'source'
/** Newer versions of Matinee (Settings → Server health → Updates). */
export type Updates = {
  build: { version: string; release?: string; commit?: string }
  install: { kind: InstallKind; canApply: boolean; channels: UpdateChannel[] | null; application?: string }
  settings: UpdateSettings
  checked?: string
  available?: { channel: UpdateChannel; version: string; revision: string; imageTag: string; url: string; published?: string; commits?: number }
  /** What the Argo CD Application installs now. */
  source?: { targetRevision: string; imageTag?: string }
  /** Argo CD follows main by itself. */
  following?: boolean
  /** The version on its way. */
  applying?: string
  lastError?: string
  /** An automatic update that installs once nobody is watching. */
  waiting?: boolean
}

export interface PlaybackReport {
  kind: 'stall' | 'error'
  positionMs: number
  /** How fast the player got the stream, in bits a second; 0 when it doesn't know. */
  downloadBps: number
  bufferMs: number
  neededBps?: number
  code?: string
  message?: string
}

/** Why playback stalled, as the server worked it out. */
export interface PlaybackCause {
  code: 'slowDisk' | 'slowConvert' | 'slowConnection' | 'unknown'
  values?: Record<string, string>
  /** What an admin could change; only admins get one. */
  tip?: 'gpu' | 'lowerQuality' | 'adaptive' | 'disk'
  /** Why the server converts this video at all. */
  converted?: 'codec' | 'hdr' | 'subtitle' | 'quality'
  at: number
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    credentials: 'same-origin',
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  return response<T>(res)
}

// response reads an API answer, throwing an ApiError for a failure.
export async function response<T>(res: Response): Promise<T> {
  if (res.headers.get(RESTARTING_HEADER)) heardRestarting()
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

function logParams(q: LogQuery): string {
  const p = new URLSearchParams()
  if (q.level) p.set('level', q.level)
  if (q.q) p.set('q', q.q)
  if (q.before) p.set('before', String(q.before))
  if (q.limit) p.set('limit', String(q.limit))
  return p.toString()
}

export const api = {
  server: () => request<{ version: string; apiVersion: number; name?: string; remoteURL?: string }>('GET', '/api/v1/server'),
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
  // Looks again in the folders and online for a music or audiobook library's missing covers.
  findArtwork: (id: number) => request<{ lookingUp: number }>('POST', `/api/v1/libraries/${id}/artwork`),
  libraryItems: (id: number, filter: ItemFilter = {}) => {
    const q = toSearchParams({ ...filter, library: undefined }).toString()
    return request<Item[]>('GET', `/api/v1/libraries/${id}/items${q ? `?${q}` : ''}`)
  },
  filterItems: (filter: ItemFilter) => request<Item[]>('GET', `/api/v1/items?${toSearchParams(filter)}`),
  libraryFilters: (id: number) => request<FilterOptions>('GET', `/api/v1/libraries/${id}/filters`),
  libraryPeople: (id: number, q: string, role?: 'cast' | 'director') =>
    request<FilterPerson[]>('GET', `/api/v1/libraries/${id}/people?${new URLSearchParams({ q, ...(role ? { role } : {}) })}`),
  browse: (path: string) =>
    request<{ path: string; parent: string; dirs: string[]; paths?: string[]; drives?: boolean }>(
      'GET',
      `/api/v1/fs?path=${encodeURIComponent(path)}`,
    ),

  analysis: () => request<AnalysisStatus>('GET', '/api/v1/analysis'),
  health: () => request<Health>('GET', '/api/v1/admin/health'),
  logs: (q: LogQuery) => request<{ lines: LogLine[]; more: boolean }>('GET', `/api/v1/admin/logs?${logParams(q)}`),
  logsDownloadURL: (q: LogQuery) => `/api/v1/admin/logs/download?${logParams(q)}`,
  alerts: () => request<AlertSettings>('GET', '/api/v1/admin/alerts'),
  saveAlerts: (c: AlertConfig) => request<AlertSettings>('PUT', '/api/v1/admin/alerts', c),
  testAlerts: (c: AlertConfig) => request<Alert>('POST', '/api/v1/admin/alerts/test', c),
  metricsSettings: () => request<MetricsSettings>('GET', '/api/v1/admin/metrics'),
  newMetricsToken: () => request<MetricsSettings>('POST', '/api/v1/admin/metrics/token'),
  metricsOff: () => request<void>('DELETE', '/api/v1/admin/metrics/token'),
  stopTranscode: (id: string) => request<void>('DELETE', `/api/v1/admin/transcodes/${encodeURIComponent(id)}`),
  setAnalysisMode: (mode: AnalysisMode) => request<AnalysisStatus>('PUT', '/api/v1/analysis', { mode }),
  setIntroDetection: (intros: boolean) => request<AnalysisStatus>('PUT', '/api/v1/analysis', { intros }),
  setCreditsDetection: (credits: boolean) => request<AnalysisStatus>('PUT', '/api/v1/analysis', { credits }),
  setThumbnailMode: (thumbnails: ThumbnailMode) => request<AnalysisStatus>('PUT', '/api/v1/analysis', { thumbnails }),
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
  setPlayback: (p: Partial<PlaybackPrefs>) => request<PlaybackPrefs>('PUT', '/api/v1/me/playback', p),
  users: () => request<Account[]>('GET', '/api/v1/users'),
  // friend: false lets the account see every library again.
  setFriend: (id: number, friend: boolean, libraries?: number[]) =>
    request<User>('PATCH', `/api/v1/users/${id}`, { friend, ...(libraries ? { libraries } : {}) }),
  invites: () => request<{ invites: Invite[]; remoteReady: boolean }>('GET', '/api/v1/invites'),
  createInvite: (name: string, libraries: number[], days: number) => request<Invite>('POST', '/api/v1/invites', { name, libraries, days }),
  deleteInvite: (code: string) => request<void>('DELETE', `/api/v1/invites/${encodeURIComponent(code)}`),
  inviteInfo: (code: string) => request<InviteInfo>('GET', `/api/v1/join/${encodeURIComponent(code)}`),
  acceptInvite: (code: string, username: string, password: string) =>
    request<{ user: User }>('POST', `/api/v1/join/${encodeURIComponent(code)}`, { username, password }),

  startTogether: (r: TogetherRequest) => request<TogetherState>('POST', '/api/v1/together', r),
  peekTogether: (code: string) => request<TogetherState & { canWatch: boolean }>('GET', `/api/v1/together/${encodeURIComponent(code)}`),
  joinTogether: (code: string, r: TogetherRequest) => request<TogetherState>('POST', `/api/v1/together/${encodeURIComponent(code)}/join`, r),
  actTogether: (code: string, r: TogetherRequest) => request<TogetherState>('POST', `/api/v1/together/${encodeURIComponent(code)}/actions`, r),
  endTogether: (code: string, r: TogetherRequest) => request<void>('POST', `/api/v1/together/${encodeURIComponent(code)}/end`, r),
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
  notifications: () => request<{ notifications: AppNotification[]; unread: number }>('GET', '/api/v1/notifications'),
  readNotifications: (ids: string[] = []) => request<void>('POST', '/api/v1/notifications/read', { ids }),
  clearNotifications: (ids: string[] = []) => request<void>('POST', '/api/v1/notifications/clear', { ids }),
  notificationSettings: () => request<NotificationSettings>('GET', '/api/v1/admin/notifications'),
  saveNotificationSettings: (admins: NotifyAdmins) => request<NotificationSettings>('PUT', '/api/v1/admin/notifications', { admins }),
  testIntegration: (app: ArrApp, c: IntegrationConfig) => request<ConnectionCheck>('POST', `/api/v1/integrations/${app}/test`, c),
  saveIntegration: (app: ArrApp, c: IntegrationConfig) => request<Integration>('PUT', `/api/v1/integrations/${app}`, c),
  deleteIntegration: (app: ArrApp) => request<void>('DELETE', `/api/v1/integrations/${app}`),
  requestOptions: () => request<RequestOptions>('GET', '/api/v1/requests/options'),
  boxOffice: () => request<BoxOfficeConnection>('GET', '/api/v1/integrations/boxoffice'),
  findBoxOffice: () => request<{ found: { url: string; version: string }[] }>('POST', '/api/v1/integrations/boxoffice/find'),
  testBoxOffice: (c?: BoxOfficeConfig) => request<BoxOfficeCheck>('POST', '/api/v1/integrations/boxoffice/test', c),
  saveBoxOffice: (c: BoxOfficeConfig) => request<BoxOfficeConnection>('PUT', '/api/v1/integrations/boxoffice', c),
  deleteBoxOffice: () => request<void>('DELETE', '/api/v1/integrations/boxoffice'),
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
  comingSoon: () => request<{ available: boolean; titles: ComingSoonTitle[] }>('GET', '/api/v1/boxoffice/coming-soon'),
  notifyMe: (t: { kind: TitleKind; tmdbId?: number; tvdbId?: number }, on: boolean) =>
    request<{ notifyMe: boolean }>('POST', '/api/v1/boxoffice/notify-me', { kind: t.kind, tmdbId: t.tmdbId, tvdbId: t.tvdbId, on }),
  itemEditions: (id: number) => request<BoxOfficeEditions>('GET', `/api/v1/items/${id}/editions`),
  askEdition: (id: number, edition: string, mode: 'replace' | 'alongside') =>
    request<AskResult>('POST', `/api/v1/items/${id}/editions`, { edition, mode }),
  askCopy: (id: number, want: 'better' | 'replace', reason = '') => request<AskResult>('POST', `/api/v1/items/${id}/copy`, { want, reason }),
  upcoming: (days = 30) => request<{ items: Upcoming[]; unavailable?: string[] }>('GET', `/api/v1/upcoming?days=${days}`),
  itemQuality: (id: number) =>
    request<{ app: ArrApp; qualityProfileId: number; qualityProfiles: QualityProfile[] }>('GET', `/api/v1/items/${id}/quality`),
  setItemQuality: (id: number, qualityProfileId: number) =>
    request<{ app: ArrApp; qualityProfileId: number; qualityProfiles: QualityProfile[] }>('PUT', `/api/v1/items/${id}/quality`, { qualityProfileId }),
  reportIssue: (id: number, kind: IssueKind, note: string) => request<Issue>('POST', `/api/v1/items/${id}/issues`, { kind, note }),
  issues: (all = false) => request<Issue[]>('GET', `/api/v1/issues${all ? '?all=1' : ''}`),
  resolveIssue: (id: string) => request<void>('POST', `/api/v1/issues/${id}/resolve`),
  replaceIssueFile: (id: string) => request<void>('POST', `/api/v1/issues/${id}/replace`),

  // newReleases, rows and recentlyAddedRows (Recently added split by kind)
  // come from servers new enough to have them. more says which of the lists
  // have more titles than they show.
  home: () =>
    request<{
      continueWatching: Item[]
      watchList: Item[]
      recentlyAdded: Item[]
      recentlyAddedRows?: HomeRow[]
      newReleases?: Item[]
      rows?: HomeRow[]
      more?: Partial<Record<'continueWatching' | 'recentlyAdded' | 'newReleases' | 'watchList', boolean>>
    }>('GET', '/api/v1/home'),
  // A row with all its titles, by its id (see HomeRow).
  row: (id: string) => request<HomeRow>('GET', `/api/v1/rows/${encodeURIComponent(id)}`),
  search: (q: string) => request<Item[]>('GET', `/api/v1/search?q=${encodeURIComponent(q)}`),
  item: (id: number) => request<ItemDetail>('GET', `/api/v1/items/${id}`),
  children: (id: number) => request<Item[]>('GET', `/api/v1/items/${id}/children`),
  // Every track of an artist or album, in playing order.
  tracks: (id: number) => request<Item[]>('GET', `/api/v1/items/${id}/tracks`),
  // A song's sound levels, for the visualizer (player/levels.ts).
  levels: (id: number) => request<LevelsJSON>('GET', `/api/v1/items/${id}/levels`),
  // A song's words, timed when they can be (player/lyricLines.ts).
  lyrics: (id: number) => request<Lyrics>('GET', `/api/v1/items/${id}/lyrics`),
  // A video's seek bar pictures (player/thumbnails.ts); asking has them made.
  thumbnails: (id: number) => request<Thumbnails>('GET', `/api/v1/items/${id}/thumbnails`),
  book: (id: number) => request<Book>('GET', `/api/v1/items/${id}/book`),
  series: (library: number) => request<BookGroup[]>('GET', `/api/v1/libraries/${library}/series`),
  narrators: (library: number) => request<BookGroup[]>('GET', `/api/v1/libraries/${library}/narrators`),
  // The record labels of a music library's albums (servers from before labels answer 404).
  labels: (library: number) => request<RecordLabel[]>('GET', `/api/v1/libraries/${library}/labels`),
  // Radio stations in a music library (servers from before radio answer 404).
  stations: (library: number) => request<Item[]>('GET', `/api/v1/libraries/${library}/stations`),
  stationDirectory: (q: { q?: string; tag?: string; library?: number }) =>
    request<FoundStation[]>('GET', `/api/v1/stations/directory?${new URLSearchParams(Object.entries(q).filter(([, v]) => v !== undefined && v !== '').map(([k, v]) => [k, String(v)]))}`),
  addStation: (s: { libraryId: number; url: string; name?: string; image?: string; homepage?: string; tags?: string[] }) => request<Item>('POST', '/api/v1/stations', s),
  renameStation: (id: number, name: string) => request<Item>('PUT', `/api/v1/stations/${id}`, { name }),
  deleteStation: (id: number) => request<void>('DELETE', `/api/v1/stations/${id}`),
  // What a station is playing, from the titles it sends while someone listens.
  stationNow: (id: number) => request<{ title?: string }>('GET', `/api/v1/items/${id}/now`),
  // Podcasts: finding shows in Podcast Index, adding them by their feed,
  // following them, and moving them between apps as OPML.
  podcastDirectory: (q: { q?: string; lang?: string; category?: string }) =>
    request<{ connected: boolean; shows: DirectoryShow[]; categories?: PodcastCategory[] }>('GET', `/api/v1/podcasts/directory?${new URLSearchParams(q as Record<string, string>)}`),
  followedPodcasts: () => request<FollowedShow[]>('GET', '/api/v1/podcasts/following'),
  addPodcast: (libraryId: number, feedUrl: string) => request<Item>('POST', '/api/v1/podcasts', { libraryId, feedUrl }),
  deletePodcast: (id: number) => request<void>('DELETE', `/api/v1/podcasts/${id}`),
  refreshPodcast: (id: number) => request<{ added: number }>('POST', `/api/v1/podcasts/${id}/refresh`),
  followPodcast: (id: number, follow: boolean) => request<void>(follow ? 'PUT' : 'DELETE', `/api/v1/podcasts/${id}/follow`),
  markPodcastPlayed: (id: number, played: boolean) => request<void>('PUT', `/api/v1/podcasts/${id}/played`, { played }),
  covers: (id: number) => request<{ options: CoverOption[]; chosen: boolean; chosenSource?: string }>('GET', `/api/v1/items/${id}/covers`),
  setCover: (id: number, url: string) => request<Item>('PUT', `/api/v1/items/${id}/cover`, { url }),
  uploadCover: async (id: number, file: Blob) => {
    const res = await fetch(`/api/v1/items/${id}/cover`, {
      method: 'PUT',
      credentials: 'same-origin',
      headers: { 'Content-Type': file.type || 'application/octet-stream' },
      body: file,
    })
    return response<Item>(res)
  },
  resetCover: (id: number) => request<void>('DELETE', `/api/v1/items/${id}/cover`),
  // Fixing a movie's or show's online match (admins).
  match: (id: number, q = '', year?: number) =>
    request<ItemMatch>('GET', `/api/v1/items/${id}/match?${new URLSearchParams({ q, ...(year ? { year: String(year) } : {}) })}`),
  setMatch: (id: number, ref: string) => request<Item>('PUT', `/api/v1/items/${id}/match`, { ref }),
  clearMatch: (id: number) => request<Item>('DELETE', `/api/v1/items/${id}/match`),
  editDetails: (id: number, edit: DetailsEdit) => request<Item>('PUT', `/api/v1/items/${id}/details`, edit),
  undoDetailEdits: (id: number) => request<Item>('DELETE', `/api/v1/items/${id}/details`),
  setMetadataMode: (id: number, mode: MetadataMode) => request<void>('PUT', `/api/v1/libraries/${id}/metadata-mode`, { mode }),
  refreshItem: (id: number) => request<Item>('POST', `/api/v1/items/${id}/refresh`),
  // Bringing history over from another media server (Settings → Import).
  findImports: () => request<{ found: string[]; uploaded: boolean }>('GET', '/api/v1/import/find'),
  uploadImport: async (file: Blob) => {
    const res = await fetch('/api/v1/import/upload', {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/octet-stream' },
      body: file,
    })
    return response<void>(res)
  },
  previewImport: (req: ImportRequest) => request<ImportPreview>('POST', '/api/v1/import/preview', req),
  runImport: (req: ImportRequest) => request<void>('POST', '/api/v1/import/run', req),
  importStatus: () => request<ImportStatus>('GET', '/api/v1/import'),
  importOpml: async (library: number, file: Blob) => {
    const res = await fetch(`/api/v1/libraries/${library}/opml`, {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'text/x-opml' },
      body: file,
    })
    return response<OpmlImport>(res)
  },
  openSubtitles: () => request<OpenSubtitlesConnection>('GET', '/api/v1/integrations/opensubtitles'),
  saveOpenSubtitles: (v: { apiKey?: string; username?: string; password?: string }) =>
    request<OpenSubtitlesConnection>('PUT', '/api/v1/integrations/opensubtitles', v),
  deleteOpenSubtitles: () => request<void>('DELETE', '/api/v1/integrations/opensubtitles'),
  searchSubtitles: (id: number, language?: string) =>
    request<SubtitleSearch>('GET', `/api/v1/items/${id}/subtitles/search${language ? `?languages=${encodeURIComponent(language)}` : ''}`),
  addSubtitle: (id: number, found: FoundSubtitle) => request<SubtitleTrack>('POST', `/api/v1/items/${id}/subtitles`, found),
  removeSubtitle: (id: number, track: string) => request<void>('DELETE', `/api/v1/items/${id}/subtitles/${encodeURIComponent(track)}`),
  subtitleSync: (id: number, track: string) => request<SubtitleSync>('GET', `/api/v1/items/${id}/subtitles/${encodeURIComponent(track)}/sync`),
  syncSubtitle: (id: number, track: string) => request<SubtitleSync>('POST', `/api/v1/items/${id}/subtitles/${encodeURIComponent(track)}/sync`),
  unsyncSubtitle: (id: number, track: string) => request<SubtitleSync>('DELETE', `/api/v1/items/${id}/subtitles/${encodeURIComponent(track)}/sync`),
  liveChannels: () => request<{ channels: LiveListing[]; categories: string[] }>('GET', '/api/v1/live/channels'),
  liveGuide: (from: number, hours = 4) =>
    request<{ from: number; to: number; channels: LiveChannel[]; programs: LiveProgram[] }>('GET', `/api/v1/live/guide?from=${from}&hours=${hours}`),
  liveChannel: (id: string) => request<{ channel: LiveChannel; programs: LiveProgram[] }>('GET', `/api/v1/live/channels/${encodeURIComponent(id)}`),
  playLiveChannel: (id: string) => request<{ url: string }>('POST', `/api/v1/live/channels/${encodeURIComponent(id)}/play`),
  hideLiveChannel: (id: string, hidden: boolean) => request<LiveChannel>('PUT', `/api/v1/live/channels/${encodeURIComponent(id)}`, { hidden }),
  liveSources: () => request<LiveSource[]>('GET', '/api/v1/live/sources'),
  addLiveSource: (src: LiveSourceInput) => request<LiveSource>('POST', '/api/v1/live/sources', src),
  updateLiveSource: (id: string, src: LiveSourceInput) => request<LiveSource>('PUT', `/api/v1/live/sources/${encodeURIComponent(id)}`, src),
  deleteLiveSource: (id: string) => request<void>('DELETE', `/api/v1/live/sources/${encodeURIComponent(id)}`),
  refreshLiveSource: (id: string) => request<LiveSource>('POST', `/api/v1/live/sources/${encodeURIComponent(id)}/refresh`),
  liveSourceChannels: (id: string) => request<LiveChannel[]>('GET', `/api/v1/live/sources/${encodeURIComponent(id)}/channels`),
  iptvOrgChoices: () => request<IPTVOrgChoices>('GET', '/api/v1/live/iptv-org'),
  podcastIndex: () => request<{ connected: boolean; hint?: string }>('GET', '/api/v1/integrations/podcastindex'),
  savePodcastIndex: (key: string, secret: string) =>
    request<{ connected: boolean; hint?: string }>('PUT', '/api/v1/integrations/podcastindex', { key, secret }),
  deletePodcastIndex: () => request<void>('DELETE', '/api/v1/integrations/podcastindex'),
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
  playlists: (q?: string) => request<Playlist[]>('GET', `/api/v1/playlists${q ? `?q=${encodeURIComponent(q)}` : ''}`),
  playlist: (id: string) => request<PlaylistDetail>('GET', `/api/v1/playlists/${encodeURIComponent(id)}`),
  createPlaylist: (name: string, items: number[] = []) => request<PlaylistDetail>('POST', '/api/v1/playlists', { name, items }),
  renamePlaylist: (id: string, name: string) => request<PlaylistDetail>('PUT', `/api/v1/playlists/${encodeURIComponent(id)}`, { name }),
  deletePlaylist: (id: string) => request<void>('DELETE', `/api/v1/playlists/${encodeURIComponent(id)}`),
  addToPlaylist: (id: string, items: number[]) =>
    request<PlaylistDetail>('POST', `/api/v1/playlists/${encodeURIComponent(id)}/items`, { items }),
  removeFromPlaylist: (id: string, entry: string) =>
    request<PlaylistDetail>('DELETE', `/api/v1/playlists/${encodeURIComponent(id)}/entries/${encodeURIComponent(entry)}`),
  orderPlaylist: (id: string, entries: string[]) =>
    request<PlaylistDetail>('PUT', `/api/v1/playlists/${encodeURIComponent(id)}/order`, { entries }),
  addToWatchList: (id: number) => request<void>('PUT', `/api/v1/watchlist/${id}`),
  removeFromWatchList: (id: number) => request<void>('DELETE', `/api/v1/watchlist/${id}`),
  // rating: 1 to 10 (half stars), or null to take it back.
  rate: (id: number, rating: number | null) =>
    rating ? request<void>('PUT', `/api/v1/items/${id}/rating`, { rating }) : request<void>('DELETE', `/api/v1/items/${id}/rating`),
  updates: () => request<Updates>('GET', '/api/v1/admin/updates'),
  saveUpdateSettings: (s: UpdateSettings) => request<Updates>('PUT', '/api/v1/admin/updates', s),
  checkUpdates: () => request<Updates>('POST', '/api/v1/admin/updates/check'),
  applyUpdate: () => request<Updates>('POST', '/api/v1/admin/updates/apply'),
  backups: () => request<Backups>('GET', '/api/v1/admin/backups'),
  makeBackup: () => request<Backups>('POST', '/api/v1/admin/backups'),
  saveBackupSettings: (s: BackupSettings) => request<Backups>('PUT', '/api/v1/admin/backups/settings', s),
  restoreBackup: (name: string) => request<Backups>('POST', `/api/v1/admin/backups/${encodeURIComponent(name)}/restore`),
  cancelRestore: () => request<Backups>('DELETE', '/api/v1/admin/backups/restore'),
  deleteBackup: (name: string) => request<Backups>('DELETE', `/api/v1/admin/backups/${encodeURIComponent(name)}`),
  backupUrl: (name: string) => `/api/v1/admin/backups/${encodeURIComponent(name)}`,
  // How much of a movie, episode, season or show is watched, and marking it all.
  watched: (id: number) => request<WatchedCount>('GET', `/api/v1/items/${id}/watched`),
  setWatched: (id: number, played: boolean) => request<WatchedCount>('PUT', `/api/v1/items/${id}/watched`, { played }),
  hideFromContinueWatching: (id: number) => request<void>('DELETE', `/api/v1/continue-watching/${id}`),
  setProgress: (id: number, positionMs: number, played = false) =>
    request<void>('PUT', `/api/v1/items/${id}/progress`, { positionMs, played }),
  // quality: auto (adaptive), original, or a step's id. The answer says
  // which the server made.
  // plays is the query saying what the browser takes as it is (see player/plays.ts).
  // audioDelay moves the sound against the picture, in milliseconds, and
  // audio picks one of the file's audio tracks (its index in mediaInfo), and
  // voice turns on Clear voice (dialogue forward, loud moments evened out).
  startHls: (id: number, startSec = 0, burnSubtitle?: string, quality?: string, plays = '', audioDelay = 0, audio?: number, voice = false) =>
    request<{ url: string; startSec: number; quality?: string }>(
      'POST',
      `/api/v1/items/${id}/hls?start=${startSec}${burnSubtitle ? `&subtitle=${encodeURIComponent(burnSubtitle)}` : ''}${quality ? `&quality=${encodeURIComponent(quality)}` : ''}${plays}${audioDelay ? `&audioDelay=${audioDelay}` : ''}${audio !== undefined ? `&audio=${audio}` : ''}${voice ? '&voice=1' : ''}`,
    ),
  // One stream of an album's songs, joined without gaps, from start
  // seconds into the first; each song's startSec is on the stream's clock.
  startGapless: (tracks: number[], start = 0) =>
    request<GaplessStream>('POST', '/api/v1/gapless', { tracks, start }),
  // on, off, or null to leave it to Matinee.
  setGapless: (id: number, gapless: boolean | null) => request<Item>('PUT', `/api/v1/items/${id}/gapless`, { gapless }),
  streaming: () => request<Streaming>('GET', '/api/v1/streaming'),
  // Tell the server playback stalled (or failed) and hear why (see player/why.ts).
  reportPlayback: (id: number, report: PlaybackReport) =>
    request<{ cause: PlaybackCause }>('POST', `/api/v1/items/${id}/playback-report`, report),
  playbackWhy: (id: number) => request<{ cause: PlaybackCause | null }>('GET', `/api/v1/items/${id}/playback-why`),
  trailers: (id: number) => request<Trailer[]>('GET', `/api/v1/items/${id}/trailers`),
  startTrailerHls: (id: number, trailer: string) =>
    request<{ url: string; startSec: number }>('POST', `/api/v1/items/${id}/trailers/${encodeURIComponent(trailer)}/hls`),
  extras: (id: number) => request<Extra[]>('GET', `/api/v1/items/${id}/extras`),
  startExtraHls: (id: number, extra: string) =>
    request<{ url: string; startSec: number }>('POST', `/api/v1/items/${id}/extras/${encodeURIComponent(extra)}/hls`),
  libraryKinds: () => request<LibraryKind[]>('GET', '/api/v1/library-kinds'),
  plugins: () => request<Plugin[]>('GET', '/api/v1/plugins'),
  pluginCatalog: () => request<CatalogPlugin[]>('GET', '/api/v1/plugins/catalog'),
  installPluginFrom: (url: string) => request<Plugin>('POST', '/api/v1/plugins', { url }),
  installPluginFile: async (file: Blob) => {
    const res = await fetch('/api/v1/plugins', {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/octet-stream' },
      body: file,
    })
    return response<Plugin>(res)
  },
  updatePlugin: (id: string, changes: { enabled?: boolean; config?: Record<string, string> }) =>
    request<Plugin>('PUT', `/api/v1/plugins/${encodeURIComponent(id)}`, changes),
  removePlugin: (id: string) => request<void>('DELETE', `/api/v1/plugins/${encodeURIComponent(id)}`),
  players: () => request<PluginPlayer[]>('GET', '/api/v1/players'),
  styledSubtitles: async (id: number, track: string, version = '') => {
    const res = await fetch(`/api/v1/items/${id}/subtitles/${encodeURIComponent(track)}.ass${version ? `?v=${version}` : ''}`, { credentials: 'same-origin' })
    if (!res.ok) throw new ApiError(res.status, i18n.t('web:player.subtitlesFailed'))
    return res.text()
  },
  fonts: (id: number) => request<AttachedFont[]>('GET', `/api/v1/items/${id}/fonts`),
  subtitles: async (id: number, track: string, version = '') => {
    const res = await fetch(`/api/v1/items/${id}/subtitles/${encodeURIComponent(track)}.vtt${version ? `?v=${version}` : ''}`, { credentials: 'same-origin' })
    if (!res.ok) throw new ApiError(res.status, i18n.t('web:player.subtitlesFailed'))
    return res.text()
  },
}

export const imageUrl = (id: number, type: 'poster' | 'backdrop', tag?: string) =>
  `/api/v1/items/${id}/image/${type}${tag ? `?v=${encodeURIComponent(tag)}` : ''}`
export const personImageUrl = (id: number) => `/api/v1/people/${id}/image`
export const fileUrl = (id: number) => `/api/v1/items/${id}/file`
export const fontUrl = (id: number, index: number) => `/api/v1/items/${id}/fonts/${index}`
export const opmlUrl = (library: number) => `/api/v1/libraries/${library}/opml`
export const pluginFileUrl = (plugin: string, path: string) =>
  `/api/v1/plugins/${encodeURIComponent(plugin)}/files/${path.split('/').map(encodeURIComponent).join('/')}`
export const extraFileUrl = (id: number, extra: string) => `/api/v1/items/${id}/extras/${encodeURIComponent(extra)}/file`
export const extraImageUrl = (id: number, extra: string) => `/api/v1/items/${id}/extras/${encodeURIComponent(extra)}/image`
export const trailerFileUrl = (id: number, trailer: string) => `/api/v1/items/${id}/trailers/${encodeURIComponent(trailer)}/file`
