import { useQuery, useQueryClient } from '@tanstack/react-query'
import type Hls from 'hls.js'
import { ArrowLeft, ChevronsRight, Maximize, Minimize, Moon, Pause, Play, RotateCcw, RotateCw, Volume2, VolumeX } from 'lucide-react'
import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { api, fileUrl, syncVersion, type ItemDetail, type PlaybackCause, type SubtitleSync, type SubtitleTrack } from '../api'
import { Spinner } from '../components/ui'
import { clock, episodeCode } from '../format'
import i18n from '../i18n'
import { canDirectPlay } from '../player/direct'
import { currentPlays, playsQuery, repackages } from '../player/plays'
import { savedPreference, savePreference, SubtitleMenu, SubtitleOverlay, syncStatus } from '../player/Subtitles'
import { useStyledSubtitles } from '../player/StyledSubtitles'
import { afterAuto, afterPick, audioLanguageOf, autoSubtitles, fromOldChoice, pickSubtitles, saveSubtitleTiming, subtitleTiming, type SubtitlePrefs } from '../player/subtitlePrefs'
import { UpNextCard, type UpNextMode } from '../player/UpNext'
import { COUNTDOWN_SECONDS, stillWatching, upNextAt } from '../player/autoplay'
import { creditsEnd, creditsOf, inCredits } from '../player/credits'
import { inIntro, introEnd } from '../player/intro'
import { sleepTimer, sleepVolume, useSleep } from '../player/sleep'
import { SleepMenu } from '../player/SleepMenu'
import { AudioDelayMenu } from '../player/AudioDelayMenu'
import { AudioMenu } from '../player/AudioMenu'
import { clampDelay, DELAY_STEP_MS, delayLabel, loadClearVoice, loadDelay, saveClearVoice, saveDelay, type Delay } from '../player/audioDelay'
import { QualityMenu } from '../player/QualityMenu'
import { LONG_STALL_MS, levelName, overCap, Rebuffers, startQuality, type Quality } from '../player/quality'
import { parseVTT, type Cue } from '../player/vtt'
import { chapterMarks, chapterTitleAt } from '../player/chapters'
import { grabFrame, HeldFrame, preloadSheets, SeekPreview } from '../player/SeekPreview'
import { askAgain, spotAt } from '../player/thumbnails'
import { PluginPlayer } from '../player/PluginPlayer'
import { GamePlayer } from '../games/GamePlayer'
import { useAudio } from '../player/AudioPlayer'
import { playerFor, usePlayers } from '../plugins'
import { remote, watchPath, type CommandType, type PlayerHooks } from '../remote'
import { health, onRestarting, outcome, restarting, waitForServer, type Health, type Waiting } from '../player/reconnect'
import { expectedSeconds, needsSeek, together } from '../player/together'
import { TogetherMenu } from '../player/TogetherMenu'
import { WhyMenu } from '../player/WhyMenu'
import { Arrivals, bufferedAheadMs, causeText, ReportGate, STALL_MS, worthSaying } from '../player/why'
import { clientId } from '../remote'
import { defaultVersion } from '../components/Versions'
import type { TogetherState } from '../api'

const displayTitle = (item: ItemDetail) => {
  const show = item.parents.find((p) => p.kind === 'show')
  const season = item.parents.find((p) => p.kind === 'season')
  if (item.kind === 'episode' && show) {
    return { title: show.title, sub: `${episodeCode(season?.index, item.index)} · ${item.title}` }
  }
  return { title: item.title, sub: item.year ? String(item.year) : '' }
}

// Each episode gets a fresh player, so auto-play can move on to the next.
// Items of a plugin's kinds play in the plugin's own player.
export default function Watch() {
  const id = Number(useParams().id)
  const navigate = useNavigate()
  const { data: item } = useQuery({ queryKey: ['item', id], queryFn: () => api.item(id) })
  const { data: players, isPending } = usePlayers()
  const plugin = item && playerFor(players, item.kind)
  const exit = useCallback(() => navigate(-1), [navigate])
  const audio = useAudio()
  // Songs play in the music player, which keeps going while you browse.
  const song = item?.kind === 'track'
  const [params] = useSearchParams()
  const t = params.get('t')
  // A title with several files plays the one that suits this screen, unless
  // one was picked (?v=1) or a version was opened.
  const pick = item?.versions && !item.versionOf && !params.get('v') ? defaultVersion(item) : undefined
  const otherVersion = pick && pick.id !== item?.id ? pick.id : undefined
  useEffect(() => {
    if (!otherVersion) return
    const next = new URLSearchParams(params)
    next.set('v', '1')
    navigate(`/watch/${otherVersion}?${next}`, { replace: true })
  }, [otherVersion]) // eslint-disable-line react-hooks/exhaustive-deps
  // Watch together lasts while the watch pages are open (player/together.ts).
  useEffect(() => together.attach(), [])
  useEffect(() => {
    if (!item) return
    if (song) {
      audio.playItem(item, t ? Number(t) * 1000 : undefined)
      navigate(item.parentId ? `/item/${item.parentId}` : '/', { replace: true })
    } else audio.pause()
  }, [item?.id]) // eslint-disable-line react-hooks/exhaustive-deps
  if (song || otherVersion) return null
  // Games the server plays itself stream from it, ahead of any plugin's player.
  if (item?.streamedGame) return <GamePlayer name={item.title} itemId={item.id} onClose={exit} />
  if (item && plugin) return <PluginPlayer item={item} player={plugin} onExit={exit} />
  if (!item || (isPending && !builtInKinds.includes(item.kind))) {
    return (
      <div className="fixed inset-0 grid place-items-center bg-black">
        <Spinner />
      </div>
    )
  }
  if (!builtInKinds.includes(item.kind)) return <NoPlayer onExit={exit} />
  return <Player key={id} id={id} />
}

// NoPlayer is for an item of a plugin's kind while the plugin is off.
function NoPlayer({ onExit }: { onExit: () => void }) {
  const { t } = useTranslation()
  return (
    <div className="fixed inset-0 grid place-items-center bg-black p-6 text-center text-white">
      <div className="max-w-sm space-y-4">
        <p>{t('plugins.noPlayer')}</p>
        <button onClick={onExit} className="rounded-full bg-white/15 px-4 py-2 text-sm hover:bg-white/25">
          {t('back')}
        </button>
      </div>
    </div>
  )
}

const builtInKinds = ['movie', 'episode']

// Why the player is getting playback going again (see recover): a transcode
// or direct play that errored, buffering that went on too long, or the
// viewer pressing Try again.
type Cause = 'transcode' | 'direct' | 'stall' | 'retry'

function Player({ id }: { id: number }) {
  const { t: tr } = useTranslation()
  const navigate = useNavigate()
  const qc = useQueryClient()
  const { data: item } = useQuery({ queryKey: ['item', id], queryFn: () => api.item(id) })

  const video = useRef<HTMLVideoElement>(null)
  const hls = useRef<Hls | null>(null)
  const wrap = useRef<HTMLDivElement>(null)
  // HLS sessions start at an offset, so the video's clock is relative to it.
  const offset = useRef(0)
  // The same, for what draws on the video's clock (styled subtitles).
  const [clockOffset, setClockOffset] = useState(0)
  const [mode, setMode] = useState<'direct' | 'hls'>()
  const [time, setTime] = useState(0)
  const [paused, setPaused] = useState(true)
  const [waiting, setWaiting] = useState(true)
  const [muted, setMuted] = useState(false)
  const [full, setFull] = useState(false)
  const [chrome, setChrome] = useState(true)
  const [error, setError] = useState('')
  const hideTimer = useRef<number>(undefined)
  const [sub, setSub] = useState<SubtitleTrack>()
  const [cues, setCues] = useState<Cue[]>([])
  // The picture subtitle track drawn into the current transcode, if any.
  const burned = useRef<string>(undefined)
  const lastSub = useRef<SubtitleTrack>(undefined)
  const menuOpen = useRef(false)
  // Counts transcode starts so a slow, replaced one can't take over the player.
  const hlsSeq = useRef(0)
  // A transcode that dies partway (say, the server's GPU gave up) is started
  // again once from where it stopped; the server then uses its CPU.
  const recovered = useRef(false)
  // When the server itself goes away (an update, a restart, the network),
  // the player says why it's waiting and picks up where it was (see recover).
  const [away, setAway] = useState<Waiting | 'gaveUp'>()
  const server = useRef<Health>(undefined) // as of when it last answered
  const recovering = useRef<AbortController>(undefined)
  const recoverRef = useRef<(at: number, cause: Cause) => void>(undefined)
  const resumeAt = useRef(0)
  const modeNow = useRef(mode)
  modeNow.current = mode
  // While the seek bar is held, where it points. The seek happens on release.
  const [scrub, setScrub] = useState<number>()
  const scrubAt = useRef<number>(undefined)
  const dragging = useRef(false)
  const scrubTimer = useRef<number>(undefined)
  // The seek bar's preview pictures (player/thumbnails.ts), and where the
  // pointer is over the bar, for the picture to show before a click.
  const thumbAsks = useRef(0)
  const { data: thumbs } = useQuery({
    queryKey: ['thumbnails', id],
    queryFn: () => (thumbAsks.current++, api.thumbnails(id)),
    enabled: item?.kind === 'movie' || item?.kind === 'episode',
    retry: false,
    staleTime: Infinity,
    refetchInterval: (q) => askAgain(q.state.data, thumbAsks.current),
  })
  const [hoverAt, setHoverAt] = useState<number>()
  // While a far seek starts the stream again: where it's going, and the
  // last frame when there's no preview picture, so it isn't black.
  const [held, setHeld] = useState<{ at: number; still?: string }>()
  const bar = useRef<HTMLDivElement>(null)
  const preloaded = useRef(false)
  const preload = () => {
    if (preloaded.current || thumbs?.state !== 'ready') return
    preloaded.current = true
    preloadSheets(id, thumbs)
  }
  // Auto-play: the card that offers what's next, and its countdown.
  const { data: prefs, isFetched: prefsKnown } = useQuery({ queryKey: ['playback'], queryFn: api.playback, staleTime: 60_000, retry: false })
  // Subtitle choices live on the server; a browser that remembered one
  // before keeps it until the person picks again.
  const subPrefs: SubtitlePrefs = prefs?.subtitles ?? fromOldChoice(savedPreference())
  const titleId = item ? (item.versionOf ?? item.id) : id
  const [subTiming, setSubTiming] = useState(() => subtitleTiming(titleId))
  useEffect(() => setSubTiming(subtitleTiming(titleId)), [titleId])
  const changeTiming = (seconds: number) => setSubTiming(saveSubtitleTiming(titleId, seconds))
  const { data: upNext } = useQuery({
    queryKey: ['next', id],
    queryFn: () => api.next(id),
    enabled: item?.kind === 'episode',
    retry: false,
  })
  const [card, setCard] = useState<UpNextMode>()
  const [left, setLeft] = useState(COUNTDOWN_SECONDS)
  const cardClosed = useRef(false)
  // Set once this episode is done with, so leaving doesn't save a position.
  const finished = useRef(false)
  // Sleep timer: fades the sound out, then pauses. "End" stops auto-play.
  const { sleep, left: sleepLeft } = useSleep()
  const [slept, setSlept] = useState(false)
  // The volume people chose; the sleep timer fades from it.
  const volume = useRef(1)
  // Where to start, when Home Assistant or another device said (?t=seconds).
  const [params] = useSearchParams()
  const startAt = params.get('t')
  // The audio track picked (?audio=N, as from a commentary on the title
  // page, the server's pick, or the Audio menu); undefined plays the
  // file's own default.
  const audioParam = params.get('audio')
  const [audioPick, setAudioPick] = useState<number>()
  const audioNow = useRef<number>(undefined)
  // Streaming quality (see player/quality.ts): what the viewer picked, and
  // which step of an adaptive stream is playing.
  const { data: streaming, isFetched: streamingKnown } = useQuery({
    queryKey: ['streaming'],
    queryFn: api.streaming,
    staleTime: 60_000,
    retry: false,
  })
  const [quality, setQuality] = useState<Quality>()
  const qualityNow = useRef<Quality>('original')
  const [level, setLevel] = useState<string>()
  const rebuffers = useRef(new Rebuffers())
  // Auto is playing the original picture repackaged (see startHls), and
  // whether it already gave that up for an adaptive stream.
  const repacked = useRef(false)
  const repackFellBack = useRef(false)
  // Watch together: the session this browser is in, if any.
  const [session, setSession] = useState<TogetherState | undefined>(together.state)
  useEffect(() => together.subscribe(setSession), [])
  const sessionNow = useRef(session)
  sessionNow.current = session
  // Audio delay for lip sync (player/audioDelay.ts). Any delay plays through
  // the server, which moves the sound against the picture.
  const [delay, setDelay] = useState<Delay>({ ms: 0, forTitle: false })
  const delayNow = useRef(0)
  const delayTimer = useRef<number>(undefined)
  // Clear voice plays through the server too, which mixes the sound.
  const [clearVoice, setClearVoice] = useState(loadClearVoice)
  const voiceNow = useRef(clearVoice)
  // Set when the browser wouldn't start playback without a click.
  const [blocked, setBlocked] = useState(false)

  const duration = item?.durationMs ? item.durationMs / 1000 : 0
  const position = useCallback(() => offset.current + (video.current?.currentTime ?? 0), [])
  // Why playback buffered (see player/why.ts): pieces' arrival speed, and
  // when it last jumped (a start or a seek waits for good reason).
  const arrivals = useRef(new Arrivals())
  const jumped = useRef(0)

  const startHls = useCallback(
    async (at: number, burn = burned.current) => {
      const el = video.current
      if (!el || !item) return
      const seq = ++hlsSeq.current
      jumped.current = Date.now()
      arrivals.current = new Arrivals()
      setWaiting(true)
      setError('')
      setLevel(undefined)
      hls.current?.destroy()
      hls.current = null
      burned.current = burn
      try {
        // Auto takes the picture as it is, repackaged, when this browser
        // decodes it, until the connection can't keep up with it.
        const plays = currentPlays(el)
        const repack = qualityNow.current === 'auto' && !burn && !repackFellBack.current && repackages(item, plays) && !overCap(item, streaming?.cap)
        repacked.current = repack
        const { url } = await api.startHls(
          item.id,
          Math.floor(at),
          burn,
          repack ? 'original' : qualityNow.current,
          playsQuery(plays),
          delayNow.current,
          // Always said, since the server otherwise plays its own pick.
          audioNow.current ?? fileAudio(item),
          voiceNow.current && !!item.videoCodec,
        )
        if (seq !== hlsSeq.current) return
        offset.current = Math.floor(at)
        setClockOffset(Math.floor(at))
        // hls.js is only downloaded when a file needs converting.
        const { default: Hls } = await import('hls.js')
        if (seq !== hlsSeq.current) return
        if (Hls.isSupported()) {
          const h = new Hls({ maxBufferLength: 60 })
          h.loadSource(url)
          h.attachMedia(el)
          h.on(Hls.Events.ERROR, (_, d) => {
            if (seq !== hlsSeq.current) return
            // A piece the server cleared to make room (a long video when the
            // transcode folder is short of it): start again from here.
            if (d.response?.code === 410) startHls(position())
            else if (d.fatal) recoverRef.current?.(position(), 'transcode')
          })
          // How fast pieces arrive, for saying why it buffered.
          h.on(Hls.Events.FRAG_LOADED, (_, d) => {
            const st = d.frag.stats
            if (seq === hlsSeq.current && st.loaded > 0) arrivals.current.add(st.loaded, st.loading.end - st.loading.start)
          })
          // An adaptive stream says which step it's on.
          h.on(Hls.Events.LEVEL_SWITCHED, (_, d) => {
            if (seq === hlsSeq.current && h.levels.length > 1) setLevel(levelName(h.levels[d.level]))
          })
          hls.current = h
        } else {
          el.src = url // Safari plays HLS natively
        }
        setMode('hls')
        el.play().catch(() => setPaused(true))
      } catch (e) {
        // A newer seek replaced this transcode, which isn't a failure.
        if (seq !== hlsSeq.current) return
        setError(e instanceof Error ? e.message : i18n.t('player.failed'))
      }
    },
    [item, position, streaming],
  )
  const playDirect = useCallback(
    (at: number) => {
      const el = video.current
      if (!el || !item) return
      hlsSeq.current++
      jumped.current = Date.now()
      hls.current?.destroy()
      hls.current = null
      burned.current = undefined
      setLevel(undefined)
      rebuffers.current.reset()
      setMode('direct')
      offset.current = 0
      setClockOffset(0)
      el.src = fileUrl(item.id)
      el.currentTime = at
      el.play().catch(() => setPaused(true))
    },
    [item],
  )

  // direct says whether the file can play as it is: the browser can play
  // it, the viewer picked Auto or Original, and it's within any cap.
  const direct = (q: Quality) => {
    const el = video.current
    return (
      !!item &&
      !!el &&
      (q === 'auto' || q === 'original') &&
      delayNow.current === 0 &&
      !(voiceNow.current && item.videoCodec) &&
      audioNow.current === undefined &&
      canDirectPlay(item, el) &&
      !overCap(item, streaming?.cap)
    )
  }

  // Pick direct play or a transcode, and resume where the viewer left off.
  useEffect(() => {
    const el = video.current
    if (!item || !el || mode || !streamingKnown || !prefsKnown) return
    const s = sessionNow.current
    const resume =
      s?.itemId === item.id
        ? expectedSeconds(s)
        : startAt !== null
          ? Number(startAt) || 0
          : !item.played && item.positionMs
            ? item.positionMs / 1000
            : 0
    // The server says which tracks to start with (what the person picked
    // for this title last time, or the best sound in their language);
    // servers from before that leave it to the player.
    const serverPick = item.trackPick
    const pick = serverPick
      ? item.subtitles?.find((t) => t.id === serverPick.subtitle)
      : pickSubtitles(item.subtitles ?? [], subPrefs, { appLanguage: i18n.language, audioLanguage: audioLanguageOf(item) })
    const d = item.videoCodec ? loadDelay(item.versionOf ?? item.id) : { ms: 0, forTitle: false }
    delayNow.current = d.ms
    setDelay(d)
    // Only a track the file has; its own default plays as it is.
    const a = audioParam !== null ? Number(audioParam) : (serverPick?.audio ?? NaN)
    audioNow.current = item.mediaInfo?.audio.some((t) => t.index === a) && a !== fileAudio(item) ? a : undefined
    setAudioPick(audioNow.current)
    const q = startQuality(streaming)
    qualityNow.current = q
    setQuality(q)
    setSub(pick)
    if (pick?.burnIn) startHls(resume, pick.id)
    else if (direct(q)) playDirect(resume)
    else startHls(resume)
  }, [item, mode, startHls, playDirect, startAt, streamingKnown, prefsKnown]) // eslint-disable-line react-hooks/exhaustive-deps

  // Picking a quality starts playback again at the same spot.
  const selectQuality = (q: Quality) => {
    qualityNow.current = q
    repackFellBack.current = false
    setQuality(q)
    if (!burned.current && direct(q)) playDirect(position())
    else startHls(position())
  }

  // Another audio track plays again from the same spot, through the
  // server, which sends just that track. The file's default plays as it is
  // again when it can.
  const selectAudio = (index: number) => {
    if (!item) return
    // Played again next time, and in the rest of the show.
    api.setTracks(item.id, { audio: index }).catch(() => {})
    audioNow.current = index === fileAudio(item) ? undefined : index
    setAudioPick(audioNow.current)
    if (!burned.current && direct(qualityNow.current)) playDirect(position())
    else startHls(position())
  }

  // A new audio delay plays again from the same spot, once the viewer
  // stops pressing.
  const changeDelay = (d: Delay) => {
    if (!item) return
    d = { ...d, ms: clampDelay(d.ms) }
    setDelay(d)
    saveDelay(item.versionOf ?? item.id, d)
    if (d.ms === delayNow.current) return
    delayNow.current = d.ms
    window.clearTimeout(delayTimer.current)
    delayTimer.current = window.setTimeout(() => {
      if (!burned.current && direct(qualityNow.current)) playDirect(position())
      else startHls(position())
    }, 700)
  }
  const changeClearVoice = (on: boolean) => {
    setClearVoice(on)
    saveClearVoice(on)
    voiceNow.current = on
    if (!item || !mode) return
    if (!burned.current && direct(qualityNow.current)) playDirect(position())
    else startHls(position())
  }
  const changeDelayRef = useRef(changeDelay)
  changeDelayRef.current = changeDelay
  const delayRef = useRef(delay)
  delayRef.current = delay
  useEffect(() => () => window.clearTimeout(delayTimer.current), [])

  // Auto playing a file as it is moves to an adaptive stream when the
  // connection can't keep up with it.
  const fallBack = () => {
    const asIs = modeNow.current === 'direct' || (modeNow.current === 'hls' && repacked.current)
    if (!asIs || qualityNow.current !== 'auto' || !streaming?.adaptive) return false
    repackFellBack.current = true
    flash(tr('common:quality.fellBack'), 6000)
    startHls(position())
    return true
  }

  // Text subtitles are drawn by the player; fetch the cues when the choice
  // changes, or when the track's times do (a sync to the audio finished).
  const subVersion = sub ? syncVersion(sub) : ''
  const subNeedsSyncCheck = !!sub?.external && !sub.sync
  useEffect(() => {
    setCues([])
    if (!item || !sub || sub.burnIn) return
    let current = true
    api
      .subtitles(item.id, sub.id, subVersion)
      .then((text) => {
        if (!current) return
        setCues(parseVTT(text))
        // Playing with a subtitle file has the server line it up with the
        // audio, the first time: follow along so the new times show.
        if (subNeedsSyncCheck)
          api
            .subtitleSync(item.id, sub.id)
            .then((s) => current && s.state === 'syncing' && setTrackSync(sub.id, s))
            .catch(() => {})
      })
      .catch(() => {})
    return () => {
      current = false
    }
    // A new sub object with the same times needs no new fetch.
  }, [item?.id, sub?.id, sub?.burnIn, subVersion])

  // Lining subtitles up with the audio runs on the server; ask how it's
  // going until it's done.
  const askedSync = useRef<string>(undefined)
  const setTrackSync = (trackId: string, sync: SubtitleSync) => {
    qc.setQueryData<ItemDetail>(['item', id], (it) => (it ? { ...it, subtitles: it.subtitles?.map((s) => (s.id === trackId ? { ...s, sync } : s)) } : it))
    setSub((cur) => (cur?.id === trackId ? { ...cur, sync } : cur))
  }
  const subSyncing = sub?.sync?.state === 'syncing'
  useEffect(() => {
    if (!item || !sub || !subSyncing) return
    let gone = false
    let timer: ReturnType<typeof setTimeout>
    const check = () =>
      api
        .subtitleSync(item.id, sub.id)
        .then((s) => {
          if (gone) return
          if (s.state === 'syncing') {
            timer = setTimeout(check, 3000)
            return
          }
          const asked = askedSync.current === sub.id
          askedSync.current = undefined
          setTrackSync(sub.id, s)
          // The new times replace any nudging done by hand.
          if (s.state === 'synced' && subTiming !== 0) changeTiming(0)
          if (s.state === 'synced' || asked) flash(s.state === 'synced' ? tr('common:subtitleSync.done') : (syncStatus(tr, s) ?? ''), 4000)
        })
        .catch(() => {
          if (!gone) timer = setTimeout(check, 10_000)
        })
    timer = setTimeout(check, 2000)
    return () => {
      gone = true
      clearTimeout(timer)
    }
    // Only a new track or a new sync starts asking.
  }, [item?.id, sub?.id, subSyncing])
  const syncSub = () => {
    if (!item || !sub) return
    askedSync.current = sub.id
    api
      .syncSubtitle(item.id, sub.id)
      .then((s) => setTrackSync(sub.id, s))
      .catch((e: Error) => flash(e.message, 5000))
  }
  const unsyncSub = () => {
    if (!item || !sub) return
    api
      .unsyncSubtitle(item.id, sub.id)
      .then((s) => setTrackSync(sub.id, s))
      .catch((e: Error) => flash(e.message, 5000))
  }

  // ASS/SSA is drawn with its styling when the browser can; otherwise the
  // words show plainly, like any other text subtitles.
  const styledOn = item && sub?.styled && !sub.burnIn && !error ? { itemId: item.id, trackId: sub.id, version: subVersion } : undefined
  const styled = useStyledSubtitles(video.current, styledOn, clockOffset - subTiming)

  // Subtitles found online are added to the video: show them right away.
  const foundSub = (t: SubtitleTrack) => {
    if (!item) return
    qc.setQueryData<ItemDetail>(['item', id], (it) => (it ? { ...it, subtitles: [...(it.subtitles ?? []).filter((s) => s.id !== t.id), t] } : it))
    qc.invalidateQueries({ queryKey: ['subtitleSearch', item.id] })
    selectSub(t)
  }
  const removeSub = (t: SubtitleTrack) => {
    if (!item) return
    api
      .removeSubtitle(item.id, t.id)
      .then(() => {
        if (sub?.id === t.id) selectSub(undefined, false)
        qc.setQueryData<ItemDetail>(['item', id], (it) => (it ? { ...it, subtitles: it.subtitles?.filter((s) => s.id !== t.id) } : it))
        qc.invalidateQueries({ queryKey: ['subtitleSearch', item.id] })
      })
      .catch((e: Error) => flash(e.message, 5000))
  }

  // A pick in the player is what the person wants next time too, on every device.
  const rememberSub = (t: SubtitleTrack | undefined) => {
    savePreference(t)
    if (item) api.setTracks(item.id, { subtitle: t?.id ?? 'off' }).catch(() => {})
    const next = afterPick(subPrefs, t, i18n.language)
    qc.setQueryData<typeof prefs>(['playback'], (p) => (p ? { ...p, subtitles: next } : p))
    api.setPlayback({ subtitles: next }).catch(() => {})
  }

  // Turning subtitles On keeps the person's languages as they are.
  const autoSub = item ? autoSubtitles(item.subtitles ?? [], subPrefs, { appLanguage: i18n.language, audioLanguage: audioLanguageOf(item) }) : undefined
  const rememberAuto = () => {
    if (item && autoSub) api.setTracks(item.id, { subtitle: autoSub.id }).catch(() => {})
    const next = afterAuto(subPrefs)
    qc.setQueryData<typeof prefs>(['playback'], (p) => (p ? { ...p, subtitles: next } : p))
    api.setPlayback({ subtitles: next }).catch(() => {})
  }

  // Picture subtitles are part of the video, so switching to or from one
  // restarts playback at the same spot.
  const selectSub = (t: SubtitleTrack | undefined, remember = true) => {
    const el = video.current
    if (!item || !el) return
    if (remember) rememberSub(t)
    setSub(t)
    if (t) lastSub.current = t
    const burn = t?.burnIn ? t.id : undefined
    if (burn === burned.current) return
    if (!burn && direct(qualityNow.current)) playDirect(position())
    else startHls(position(), burn)
  }

  useEffect(
    () => () => {
      hls.current?.destroy()
      recovering.current?.abort()
      sleepTimer.leave()
    },
    [],
  )

  // What the server was when playback started, to tell later what happened
  // if it goes away.
  useEffect(() => {
    health()
      .then((h) => (server.current ??= h))
      .catch(() => {})
  }, [])

  // recover works out why playback stopped and gets it going again at `at`.
  // When the server is fine, it's this file: a direct play the browser can't
  // manage converts instead, and a transcode that died is started again once.
  // When the server is away or came back different, the player says so,
  // waits for it, and picks up where it was.
  const recover = async (at: number, cause: Cause) => {
    if (recovering.current || finished.current) return
    const ctl = new AbortController()
    recovering.current = ctl
    resumeAt.current = at
    const resume = () => (modeNow.current === 'direct' ? playDirect(at) : startHls(at))
    try {
      let now: Health | undefined
      try {
        now = await health(ctl.signal)
      } catch {
        // Away: wait for it below.
      }
      if (ctl.signal.aborted) return
      if (now && now.state !== 'restarting' && outcome(server.current, now) === 'same') {
        server.current = now
        if (cause === 'retry') resume()
        else if (cause === 'stall' && fallBack()) return
        else if (cause === 'direct') startHls(at)
        else if (cause === 'transcode' && !recovered.current) {
          recovered.current = true
          startHls(at)
        } else if (cause === 'transcode') setError(i18n.t('player.failedDetail'))
        return
      }
      hls.current?.destroy()
      hls.current = null
      if (!now || now.state === 'restarting') {
        setAway(restarting() ? 'restarting' : 'unreachable')
        now = await waitForServer(setAway, ctl.signal)
      }
      const what = outcome(server.current, now)
      server.current = now
      setAway(undefined)
      if (what === 'updated') flash(tr('common:reconnect.updated', { version: now.version }), 6000)
      else if (what === 'restarted') flash(tr('common:reconnect.restarted'), 6000)
      resume()
    } catch {
      if (!ctl.signal.aborted) setAway('gaveUp')
    } finally {
      if (recovering.current === ctl) recovering.current = undefined
    }
  }
  recoverRef.current = recover

  // A player that sits buffering for a while may have lost the server
  // without an error (a file playing directly just stops arriving).
  // Auto gives a file playing as it is less time before moving to an
  // adaptive stream.
  const canFallBack = (mode === 'direct' || (mode === 'hls' && repacked.current)) && quality === 'auto' && !!streaming?.adaptive
  useEffect(() => {
    if (!waiting || paused || error || away) return
    const t = window.setTimeout(() => recoverRef.current?.(position(), 'stall'), canFallBack ? LONG_STALL_MS : 10_000)
    return () => window.clearTimeout(t)
  }, [waiting, paused, error, away, position, canFallBack])

  // Heads-up as soon as the server says it's restarting; what's buffered
  // often carries playback through.
  useEffect(() => onRestarting(() => flash(tr('common:reconnect.heads'), 8000)), []) // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    const el = video.current
    if (!el) return
    el.volume = volume.current * sleepVolume(sleepLeft)
    if (sleepLeft === 0) {
      sleepTimer.set(undefined)
      el.pause()
      el.volume = volume.current
      setSlept(true)
    }
  }, [sleepLeft])

  // Save progress every 10 seconds and when leaving.
  useEffect(() => {
    if (!item) return
    const save = () => {
      if (finished.current) return
      const pos = position()
      if (pos > 1) api.setProgress(item.id, Math.round(pos * 1000)).catch(() => {})
    }
    const timer = window.setInterval(() => !video.current?.paused && save(), 10_000)
    window.addEventListener('pagehide', save)
    return () => {
      save()
      window.clearInterval(timer)
      window.removeEventListener('pagehide', save)
      qc.invalidateQueries({ queryKey: ['home'] })
      qc.invalidateQueries({ queryKey: ['item', item.id] })
      if (item.parentId) qc.invalidateQueries({ queryKey: ['children', item.parentId] })
    }
  }, [item, qc])

  // seek jumps there, and takes everyone watching together along.
  const seek = (to: number) => {
    to = Math.max(0, Math.min(duration || to, to))
    if (sessionNow.current) together.act('seek', Math.round(to * 1000))
    seekHere(to)
  }
  const seekHere = (to: number) => {
    const el = video.current
    if (!el) return
    to = Math.max(0, Math.min(duration || to, to))
    if (mode === 'direct') {
      el.currentTime = to
      return
    }
    // Inside what the transcode has produced: seek in place. Further out: restart there.
    const rel = to - offset.current
    const end = el.seekable.length ? el.seekable.end(el.seekable.length - 1) : 0
    if (rel >= 0 && rel <= end) el.currentTime = rel
    else {
      setHeld({ at: to, still: spotAt(thumbs, to) ? undefined : grabFrame(el) })
      startHls(to)
    }
  }

  // Direct play seeks live while dragging. A transcode restarts on each far
  // seek, so it waits for the bar to be let go (or a pause in key presses).
  const scrubTo = (to: number) => {
    window.clearTimeout(scrubTimer.current)
    if (mode === 'direct') {
      seek(to)
      return
    }
    scrubAt.current = to
    setScrub(to)
    if (!dragging.current) scrubTimer.current = window.setTimeout(commitScrub, 400)
  }
  const commitScrub = () => {
    window.clearTimeout(scrubTimer.current)
    dragging.current = false
    const to = scrubAt.current
    if (to === undefined) return
    scrubAt.current = undefined
    setScrub(undefined)
    setTime(to)
    seek(to)
  }

  const toggle = () => {
    const el = video.current
    if (!el) return
    setPlaying(el.paused)
  }
  // setPlaying plays or pauses, for everyone watching together too.
  const setPlaying = (play: boolean) => {
    const el = video.current
    if (!el) return
    setBlocked(false)
    if (sessionNow.current) together.act(play ? 'play' : 'pause', Math.round(position() * 1000))
    if (play) el.play().catch(() => {})
    else el.pause()
  }

  const toggleFull = () => {
    if (document.fullscreenElement) document.exitFullscreen()
    // The whole page, so full screen carries on into the next episode.
    else document.documentElement.requestFullscreen()
  }

  const autoPlay = (prefs?.autoPlay ?? true) && sleep !== 'end'
  const credits = creditsOf(item)
  const at = upNextAt(duration, credits)

  // Done with this episode: mark it watched and play the next.
  const playNext = useCallback(
    (auto: boolean) => {
      const next = upNext?.next
      if (!item || !next || finished.current) return
      finished.current = true
      if (auto) stillWatching.autoPlayed()
      sleepTimer.carryOver()
      api.setProgress(item.id, 0, true).catch(() => {})
      if (sessionNow.current) together.act('item', 0, next.id)
      navigate(`/watch/${next.id}`, { replace: true })
    },
    [item, upNext, navigate],
  )

  // Leave for the episode's page, as the card's Cancel does.
  const toDetails = () => {
    if (item) navigate(`/item/${item.id}`, { replace: true })
  }

  // Show the card once the credits start; take it away when someone seeks back before them.
  useEffect(() => {
    if (!item || at === undefined || !upNext) return
    if (time < at - 0.25) {
      if (card) setCard(undefined)
      setLeft(COUNTDOWN_SECONDS)
      cardClosed.current = false
      return
    }
    if (card) return
    if (upNext.next && autoPlay) {
      if (stillWatching.shouldAsk()) setCard('still')
      else if (prefs?.binge) playNext(true)
      else setCard('countdown')
    } else if (upNext.similar && !cardClosed.current && sleep !== 'end') {
      setCard('similar')
    }
  }, [item, at, upNext, time, card, autoPlay, prefs, playNext, sleep])

  // Count down while the video plays.
  useEffect(() => {
    if (card !== 'countdown' || paused) return
    if (left <= 0) {
      playNext(true)
      return
    }
    const t = window.setTimeout(() => setLeft((l) => l - 1), 1000)
    return () => window.clearTimeout(t)
  }, [card, paused, left, playNext])

  const ended = () => {
    if (!item) return
    if (sleep === 'end') sleepTimer.set(undefined)
    if (upNext?.next && autoPlay) {
      if (stillWatching.shouldAsk()) setCard('still')
      else playNext(true)
      return
    }
    finished.current = true
    api.setProgress(item.id, 0, true).then(() => navigate(-1))
  }

  const poke = useCallback(() => {
    setChrome(true)
    window.clearTimeout(hideTimer.current)
    hideTimer.current = window.setTimeout(() => !video.current?.paused && !menuOpen.current && setChrome(false), 2500)
  }, [])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      stillWatching.input()
      if (e.target instanceof HTMLInputElement) return
      poke()
      switch (e.key) {
        case ' ':
        case 'k':
          e.preventDefault()
          toggle()
          break
        case 'ArrowRight':
          seek(position() + 10)
          break
        case 'ArrowLeft':
          seek(position() - 10)
          break
        case 'f':
          toggleFull()
          break
        case 'm':
          if (video.current) video.current.muted = !video.current.muted
          break
        case 'c':
          if (sub) selectSub(undefined)
          else if (lastSub.current) selectSub(lastSub.current)
          else if (autoSub) {
            selectSub(autoSub, false)
            rememberAuto()
          }
          break
        case '[':
        case ']':
          if (item?.videoCodec) {
            const ms = clampDelay(delayRef.current.ms + (e.key === ']' ? DELAY_STEP_MS : -DELAY_STEP_MS))
            changeDelayRef.current({ ...delayRef.current, ms })
            flash(`${tr('common:audioDelay.title')}: ${delayLabel(ms, i18n.language)}`, 1500)
          }
          break
        case 'Escape':
          if (!document.fullscreenElement) navigate(-1)
      }
    }
    const onFull = () => setFull(!!document.fullscreenElement)
    window.addEventListener('keydown', onKey)
    document.addEventListener('fullscreenchange', onFull)
    return () => {
      window.removeEventListener('keydown', onKey)
      document.removeEventListener('fullscreenchange', onFull)
    }
  })

  // Remote control (see remote.ts): say what's playing, and do what Home
  // Assistant or another device asks.
  const [notice, setNotice] = useState<string>()
  const noticeTimer = useRef<number>(undefined)
  const flash = (text: string, ms = 3000) => {
    setNotice(text)
    window.clearTimeout(noticeTimer.current)
    noticeTimer.current = window.setTimeout(() => setNotice(undefined), ms)
  }
  // When it stops to buffer, ask the server why, and say so when it's clear.
  const [cause, setCause] = useState<PlaybackCause>()
  const reportGate = useRef(new ReportGate())
  const report = (kind: 'stall' | 'error', message?: string) => {
    const el = video.current
    if (!item || !el || (kind === 'stall' && !reportGate.current.take())) return
    const h = hls.current
    const level = h && h.currentLevel >= 0 ? h.levels[h.currentLevel] : undefined
    api
      .reportPlayback(item.id, {
        kind,
        positionMs: Math.round(position() * 1000),
        downloadBps: arrivals.current.bps(),
        bufferMs: bufferedAheadMs(el),
        neededBps: level?.bitrate ?? 0,
        message,
      })
      .then(({ cause }) => {
        setCause(cause)
        // A transcode that ran out of room is worth saying even when it starts again.
        if ((kind === 'stall' || cause.code === 'noSpace') && worthSaying(cause)) flash(causeText(tr, cause), 7000)
      })
      .catch(() => {})
  }
  const reportRef = useRef(report)
  reportRef.current = report
  useEffect(() => {
    if (!waiting || paused || error || away || !item) return
    const t = window.setTimeout(() => {
      if (!video.current?.seeking && Date.now() - jumped.current > STALL_MS * 2) reportRef.current('stall')
    }, STALL_MS)
    return () => window.clearTimeout(t)
  }, [waiting, paused, error, away, item])
  useEffect(() => {
    if (error) reportRef.current('error', error)
  }, [error])
  // A cause from just before (say, before a reload) is still worth seeing.
  useEffect(() => {
    api
      .playbackWhy(id)
      .then(({ cause }) => cause && setCause((c) => c ?? cause))
      .catch(() => {})
  }, [id])

  const hooks = useRef<PlayerHooks>(undefined)
  hooks.current = {
    commands: () => {
      const c: CommandType[] = ['play', 'pause', 'stop', 'seek', 'previous', 'playItem', 'volume', 'mute', 'sleep']
      if (upNext?.next) c.push('next')
      if (item?.subtitles?.length) c.push('subtitles')
      return c
    },
    state: () => {
      const el = video.current
      if (!item || !el) return undefined
      return {
        itemId: item.id,
        positionMs: Math.round(position() * 1000),
        durationMs: item.durationMs,
        paused: el.paused,
        buffering: waiting,
        volume: volume.current,
        muted: el.muted,
        subtitles: sub?.id ?? '',
        transcoding: mode === 'hls',
        sleepAt: sleep && sleep !== 'end' ? sleep.at : undefined,
        sleepAtEnd: sleep === 'end',
      }
    },
    handle: (c) => {
      const el = video.current
      if (!el || !item) return
      if (c.from) flash(tr('remote.controlledBy', { name: c.from }))
      switch (c.type) {
        case 'play':
          setPlaying(true)
          break
        case 'pause':
          setPlaying(false)
          break
        case 'stop':
          navigate(-1)
          break
        case 'seek':
          seek((c.positionMs ?? 0) / 1000)
          break
        case 'previous':
          seek(0)
          break
        case 'next':
          playNext(false)
          break
        case 'playItem':
          if (c.itemId === item.id) {
            if (c.positionMs !== undefined) seek(c.positionMs / 1000)
            setPlaying(true)
          } else if (c.itemId) {
            if (sessionNow.current) together.act('item', c.positionMs ?? 0, c.itemId)
            navigate(watchPath(c.itemId, c.positionMs), { replace: true })
          }
          break
        case 'volume':
          volume.current = Math.max(0, Math.min(1, c.volume ?? 1))
          el.volume = volume.current * sleepVolume(sleepLeft)
          remote.report()
          break
        case 'mute':
          el.muted = !!c.muted
          break
        case 'sleep':
          if (c.endOfItem) sleepTimer.set('end')
          else if (c.minutes) sleepTimer.after(c.minutes)
          else sleepTimer.set(undefined)
          break
        case 'subtitles':
          selectSub(c.track ? item.subtitles?.find((t) => t.id === c.track) : undefined)
          break
      }
    },
  }
  useEffect(
    () =>
      remote.attach({
        commands: () => hooks.current!.commands(),
        state: () => hooks.current!.state(),
        handle: (c) => hooks.current!.handle(c),
      }),
    [],
  )
  useEffect(() => remote.report(), [item, paused, waiting, muted, sub, sleep, mode, upNext])
  useEffect(() => {
    if (paused) return
    const t = window.setInterval(remote.report, 10_000)
    return () => window.clearInterval(t)
  }, [paused])

  // Watch together: follow the session. Another title means everyone moved
  // on; otherwise play or pause as it does, and seek back in line when this
  // player drifted (a slow connection, a transcode starting up).
  useEffect(() => {
    if (!session || !item) return
    if (session.itemId !== item.id) {
      navigate(`/watch/${session.itemId}`, { replace: true })
      return
    }
    const follow = () => {
      const s = sessionNow.current
      const el = video.current
      if (!s || !el || !mode || recovering.current || dragging.current || scrubAt.current !== undefined) return
      if (s.paused && !el.paused) el.pause()
      if (!s.paused && el.paused && !blockedNow.current) {
        el.play().catch((e: Error) => {
          // Browsers want a click before playing with sound.
          if (e.name === 'NotAllowedError') setBlocked(true)
        })
      }
      // Not while loading, and not again straight after: a transcode that
      // starts over at each seek would never catch up.
      if (!el.seeking && !waitingNow.current && Date.now() - lastDrift.current > 5000 && needsSeek(s, position())) {
        lastDrift.current = Date.now()
        seekHere(expectedSeconds(s))
      }
    }
    follow()
    const timer = window.setInterval(follow, 1000)
    return () => window.clearInterval(timer)
  }, [session, item, mode]) // eslint-disable-line react-hooks/exhaustive-deps
  const blockedNow = useRef(blocked)
  blockedNow.current = blocked
  const waitingNow = useRef(waiting)
  waitingNow.current = waiting
  const lastDrift = useRef(0)

  // Say what others did: "Sam paused".
  const lastSeen = useRef(session?.last?.at)
  useEffect(() => {
    const last = session?.last
    if (!last || last.at === lastSeen.current) return
    lastSeen.current = last.at
    if (last.clientId === clientId() || last.action === 'start' || last.action === 'item') return
    flash(tr(`common:together.did.${last.action}`, { name: last.by }))
  }, [session]) // eslint-disable-line react-hooks/exhaustive-deps

  const startTogether = () => {
    if (!item) return Promise.resolve()
    return together.start(item.id, Math.round(position() * 1000), !!video.current?.paused)
  }

  // Skip intro (see player/intro.ts): a button while the intro plays, or
  // straight past it, once per episode, for people who chose that.
  const intro = item?.intro
  const showSkip = inIntro(time, intro) && !card && !error && !away
  const skipIntro = (auto: boolean) => {
    if (!intro) return
    seek(introEnd(intro))
    if (auto) flash(tr('player.introSkipped'))
  }
  const autoSkipped = useRef<number>(undefined) // the episode it last happened in
  useEffect(() => {
    if (!item || !showSkip || paused || waiting || !prefs?.skipIntros || autoSkipped.current === item.id) return
    autoSkipped.current = item.id
    skipIntro(true)
  })

  // Skip credits (see player/credits.ts): when no Up next card shows at the
  // credits, a button jumps past them, to a scene after them or the end.
  const showSkipCredits = !showSkip && inCredits(time, duration, credits) && !card && !error && !away
  const skipCredits = () => {
    if (!credits) return
    const to = creditsEnd(credits, duration)
    if (to < duration) seek(to)
    else ended()
  }

  const t = item ? displayTitle(item) : { title: '', sub: '' }
  const shown = scrub ?? time
  // The preview shows where the pointer is, or where a drag or the arrow keys are taking the bar.
  const previewAt = hoverAt ?? scrub
  const frac = duration ? Math.min(1, shown / duration) : 0
  const marks = chapterMarks(item?.chapters, duration)

  return (
    <div
      ref={wrap}
      onMouseMove={poke}
      onTouchStart={poke}
      onPointerDown={() => stillWatching.input()}
      className={`fixed inset-0 z-50 bg-black text-white select-none ${chrome ? '' : 'cursor-none'}`}
    >
      <video
        ref={video}
        className="size-full"
        playsInline
        onClick={toggle}
        onDoubleClick={toggleFull}
        onTimeUpdate={() => setTime(position())}
        onPlay={() => {
          setPaused(false)
          setSlept(false)
          poke()
        }}
        onPause={() => {
          setPaused(true)
          setChrome(true)
        }}
        onWaiting={() => {
          setWaiting(true)
          // Buffering again and again (not after a seek) means the
          // connection can't keep up with the file as it is.
          if (canFallBack && !video.current?.seeking && time > 1 && rebuffers.current.add()) {
            rebuffers.current.reset()
            fallBack()
          }
        }}
        onPlaying={() => (setWaiting(false), setHeld(undefined))}
        onCanPlay={() => (setWaiting(false), setHeld(undefined))}
        onSeeking={() => (jumped.current = Date.now())}
        onVolumeChange={() => setMuted(!!video.current?.muted)}
        onSeeked={() => remote.report()}
        onEnded={ended}
        onError={() => recoverRef.current?.(position(), mode === 'direct' ? 'direct' : 'transcode')}
      />

      {sub && !sub.burnIn && !error && (!sub.styled || styled.failed) && <SubtitleOverlay cues={cues} position={position} lifted={chrome} prefs={subPrefs} timing={subTiming} />}

      {card && !error && (upNext?.next ?? upNext?.similar) && (
        <UpNextCard
          mode={card}
          item={(card === 'similar' ? upNext?.similar : upNext?.next)!}
          left={left}
          lifted={chrome}
          onPlay={() => {
            if (card === 'similar') navigate(`/item/${upNext!.similar!.id}`, { replace: true })
            else playNext(false)
          }}
          onCancel={() => {
            if (card === 'similar') {
              cardClosed.current = true
              setCard(undefined)
            } else toDetails()
          }}
        />
      )}

      {showSkip && (
        <button
          onClick={(e) => {
            e.stopPropagation()
            stillWatching.input()
            skipIntro(false)
          }}
          className={`absolute end-4 z-10 flex items-center gap-2 rounded-full bg-black/80 px-5 py-3 text-sm font-semibold text-white shadow-2xl ring-1 ring-white/25 backdrop-blur transition-[bottom] duration-300 hover:bg-black/90 hover:ring-white/50 focus-visible:ring-2 focus-visible:ring-accent focus-visible:outline-none md:end-8 ${
            chrome ? 'bottom-32' : 'bottom-8'
          }`}
        >
          {tr('player.skipIntro')}
          <ChevronsRight className="size-5 text-accent rtl:-scale-x-100" aria-hidden />
        </button>
      )}

      {showSkipCredits && (
        <button
          onClick={(e) => {
            e.stopPropagation()
            stillWatching.input()
            skipCredits()
          }}
          className={`absolute end-4 z-10 flex items-center gap-2 rounded-full bg-black/80 px-5 py-3 text-sm font-semibold text-white shadow-2xl ring-1 ring-white/25 backdrop-blur transition-[bottom] duration-300 hover:bg-black/90 hover:ring-white/50 focus-visible:ring-2 focus-visible:ring-accent focus-visible:outline-none md:end-8 ${
            chrome ? 'bottom-32' : 'bottom-8'
          }`}
        >
          {tr('player.skipCredits')}
          <ChevronsRight className="size-5 text-accent rtl:-scale-x-100" aria-hidden />
        </button>
      )}

      {slept && paused && !error && (
        <div className="pointer-events-none absolute inset-x-0 top-1/2 flex -translate-y-1/2 justify-center p-6">
          <div className="flex items-center gap-2 rounded-full bg-black/70 px-4 py-2 text-sm text-white/80 backdrop-blur">
            <Moon className="size-4 text-accent" />
            {tr('sleep.paused')}
          </div>
        </div>
      )}

      {blocked && session && !error && !away && (
        <div className="absolute inset-0 z-10 grid place-items-center bg-black/50 p-6 text-center">
          <button
            onClick={(e) => {
              e.stopPropagation()
              setBlocked(false)
              video.current?.play().catch(() => {})
            }}
            className="flex items-center gap-2 rounded-full bg-accent px-5 py-3 font-semibold text-on-accent hover:bg-accent-strong"
          >
            <Play className="size-5 fill-current" />
            {tr('common:together.joinIn')}
          </button>
        </div>
      )}

      {notice && (
        <div className="pointer-events-none absolute inset-x-0 top-20 flex justify-center p-4">
          <div className="rounded-full bg-black/70 px-4 py-2 text-sm text-white/80 backdrop-blur">{notice}</div>
        </div>
      )}

      {held && waiting && !error && !away && <HeldFrame id={id} thumbs={thumbs} at={held.at} still={held.still} />}
      {(waiting || !item) && !error && !away && (
        <div className="pointer-events-none absolute inset-0 grid place-items-center">
          <Spinner className="size-12" />
        </div>
      )}
      {away && !error && (
        <div role="status" className="absolute inset-0 z-10 grid place-items-center bg-black/60 p-6 text-center text-white">
          <div className="max-w-sm space-y-3">
            {away !== 'gaveUp' && <Spinner className="mx-auto size-10" />}
            <p className="text-lg font-semibold">{tr(`common:reconnect.${away}`)}</p>
            <p className="text-sm text-white/70">{tr(`common:reconnect.${away}Detail`)}</p>
            <div className="flex justify-center gap-4 pt-2 text-sm">
              {away === 'gaveUp' && (
                <button
                  onClick={() => {
                    setAway(undefined)
                    recover(resumeAt.current, 'retry')
                  }}
                  className="rounded-full bg-accent px-4 py-2 font-semibold text-on-accent hover:bg-accent-strong"
                >
                  {tr('common:reconnect.tryAgain')}
                </button>
              )}
              <button onClick={() => navigate(-1)} className="rounded-full bg-white/15 px-4 py-2 hover:bg-white/25">
                {tr('goBack')}
              </button>
            </div>
          </div>
        </div>
      )}
      {error && (
        <div className="absolute inset-0 grid place-items-center p-6 text-center text-white/70">
          <div>
            <p>{error}</p>
            <button onClick={() => navigate(-1)} className="mt-4 text-accent">
              {tr('goBack')}
            </button>
          </div>
        </div>
      )}

      <div className={`transition-opacity duration-300 ${chrome ? 'opacity-100' : 'pointer-events-none opacity-0'}`}>
        <div className="absolute inset-x-0 top-0 flex items-center gap-3 bg-gradient-to-b from-black/80 to-transparent p-4 pt-[max(1rem,env(safe-area-inset-top))] md:p-6">
          <button onClick={() => navigate(-1)} className="rounded-full p-2 hover:bg-white/10" aria-label={tr('back')}>
            <ArrowLeft className="size-6 rtl:-scale-x-100" />
          </button>
          <div className="min-w-0">
            <div className="truncate text-lg font-semibold">{t.title}</div>
            {t.sub && <div className="truncate text-sm text-white/70">{t.sub}</div>}
          </div>
        </div>

        <div className="absolute inset-x-0 bottom-0 bg-gradient-to-t from-black/90 via-black/50 to-transparent px-4 pt-16 pb-[max(1rem,env(safe-area-inset-bottom))] md:px-8 md:pb-6">
          {/* Time runs left to right in every language, as on a clock. */}
          <div
            ref={bar}
            dir="ltr"
            className="group relative mb-3 flex h-6 items-center md:h-4"
            onPointerEnter={preload}
            onPointerMove={(e) => {
              if (e.pointerType !== 'mouse' && !dragging.current) return
              const r = bar.current?.getBoundingClientRect()
              if (!r?.width || !duration) return
              setHoverAt(Math.min(1, Math.max(0, (e.clientX - r.left) / r.width)) * duration)
            }}
            onPointerLeave={() => !dragging.current && setHoverAt(undefined)}
          >
            {previewAt !== undefined && (
              <SeekPreview id={id} thumbs={thumbs} at={previewAt} duration={duration} chapter={chapterTitleAt(item?.chapters, previewAt)} />
            )}
            <div className="relative h-1 w-full rounded-full bg-white/25 transition-all group-hover:h-1.5">
              <div className="h-full rounded-full bg-accent" style={{ width: `${frac * 100}%` }} />
              {/* A small gap in the bar where each chapter starts. */}
              {marks.map((f) => (
                <div key={f} className="absolute inset-y-0 w-0.5 -translate-x-1/2 bg-black/70" style={{ left: `${f * 100}%` }} data-testid="chapter-mark" />
              ))}
            </div>
            <input
              type="range"
              min={0}
              max={duration || 1}
              step={1}
              value={shown}
              onPointerDown={() => (preload(), (dragging.current = true))}
              onPointerUp={(e) => (e.pointerType !== 'mouse' && setHoverAt(undefined), commitScrub())}
              onPointerCancel={() => (setHoverAt(undefined), commitScrub())}
              onFocus={preload}
              onBlur={() => setHoverAt(undefined)}
              onChange={(e) => scrubTo(Number(e.target.value))}
              aria-label={tr('player.seek')}
              className="absolute inset-0 w-full cursor-pointer opacity-0"
            />
          </div>
          <div className="flex items-center gap-2 md:gap-4">
            <button onClick={toggle} className="rounded-full p-2 hover:bg-white/10" aria-label={paused ? tr('item.play') : tr('player.pause')}>
              {paused ? <Play className="size-7 fill-white" /> : <Pause className="size-7 fill-white" />}
            </button>
            <button onClick={() => seek(position() - 10)} className="rounded-full p-2 hover:bg-white/10" aria-label={tr('player.back10')}>
              <RotateCcw className="size-6" />
            </button>
            <button onClick={() => seek(position() + 10)} className="rounded-full p-2 hover:bg-white/10" aria-label={tr('player.forward10')}>
              <RotateCw className="size-6" />
            </button>
            <button
              onClick={() => video.current && (video.current.muted = !video.current.muted)}
              className="rounded-full p-2 hover:bg-white/10"
              aria-label={muted ? tr('player.unmute') : tr('player.mute')}
            >
              {muted ? <VolumeX className="size-6" /> : <Volume2 className="size-6" />}
            </button>
            <div dir="ltr" className="text-sm text-white/80 tabular-nums">
              {clock(shown)} <span className="text-white/40">/ {clock(duration)}</span>
            </div>
            <div className="ms-auto flex items-center gap-2">
              {mode === 'hls' && !streaming?.adaptive && (
                <span className="hidden rounded bg-white/10 px-2 py-0.5 text-xs text-white/60 sm:inline">{tr('player.converting')}</span>
              )}
              {streaming?.adaptive && quality && (
                <QualityMenu
                  steps={streaming.steps}
                  selected={quality}
                  playing={mode === 'direct' ? tr('common:quality.original') : level}
                  onSelect={selectQuality}
                  onOpenChange={(open) => {
                    menuOpen.current = open
                    poke()
                  }}
                />
              )}
              {cause && (
                <WhyMenu
                  cause={cause}
                  onOpenChange={(open) => {
                    menuOpen.current = open
                    poke()
                  }}
                />
              )}
              {item && (
                <TogetherMenu
                  session={session}
                  onStart={startTogether}
                  onOpenChange={(open) => {
                    menuOpen.current = open
                    poke()
                  }}
                />
              )}
              {item?.videoCodec && (item.mediaInfo?.audio.length ?? 0) > 1 && (
                <AudioMenu
                  tracks={item.mediaInfo!.audio}
                  selected={audioPick ?? fileAudio(item)}
                  onSelect={selectAudio}
                  onOpenChange={(open) => {
                    menuOpen.current = open
                    poke()
                  }}
                />
              )}
              {item?.videoCodec && (
                <AudioDelayMenu
                  delay={delay}
                  onChange={changeDelay}
                  clearVoice={clearVoice}
                  onClearVoice={changeClearVoice}
                  onOpenChange={(open) => {
                    menuOpen.current = open
                    poke()
                  }}
                />
              )}
              {item && (
                <SleepMenu
                  sleep={sleep}
                  left={sleepLeft}
                  kind={item.kind}
                  onOpenChange={(open) => {
                    menuOpen.current = open
                    poke()
                  }}
                />
              )}
              {item && (!!item.subtitles?.length || item.subtitleSearch) && (
                <SubtitleMenu
                  tracks={item.subtitles ?? []}
                  selected={sub?.id}
                  onSelect={selectSub}
                  auto={autoSub}
                  onAuto={() => {
                    selectSub(autoSub, false)
                    rememberAuto()
                  }}
                  timing={sub && !sub.burnIn ? { seconds: subTiming, onChange: changeTiming } : undefined}
                  sync={sub && !sub.burnIn ? { state: sub.sync, onSync: syncSub, onUndo: unsyncSub } : undefined}
                  onOpenChange={(open) => {
                    menuOpen.current = open
                    poke()
                  }}
                  find={item.subtitleSearch ? { itemId: item.id, onAdded: foundSub, onRemove: removeSub } : undefined}
                />
              )}
              <button onClick={toggleFull} className="rounded-full p-2 hover:bg-white/10" aria-label={tr('player.fullscreen')}>
                {full ? <Minimize className="size-6" /> : <Maximize className="size-6" />}
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}

// The audio track a file plays when nobody picks one: its default, else the first.
function fileAudio(item: { mediaInfo?: { audio: { index: number; default?: boolean }[] } }): number {
  return item.mediaInfo?.audio.find((a) => a.default)?.index ?? 0
}
