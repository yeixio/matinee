import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Bell, BellOff, Check, ExternalLink, MoreHorizontal, Pause, Play, RefreshCw, RotateCcw, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link, useNavigate } from 'react-router-dom'
import { api, type Item, type ItemDetail, type User } from '../api'
import { EpisodeList, episodeLine } from '../components/Episodes'
import { Button, Spinner } from '../components/ui'
import { duration, shortDate } from '../format'
import { useAudio, useBookTime } from '../player/AudioPlayer'
import * as B from '../player/book'
import { Hero, Overview } from './MusicItem'

const PAGE = 50

// A podcast: who makes it, following it, and its episodes newest first.
export function PodcastPage({ item, episodes }: { item: ItemDetail; episodes?: Item[] }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const navigate = useNavigate()
  const audio = useAudio()
  const me = useQuery<User>({ queryKey: ['me'], queryFn: api.me, staleTime: Infinity })
  const [filter, setFilter] = useState<'all' | 'unplayed'>('all')
  const [shown, setShown] = useState(PAGE)
  const [menu, setMenu] = useState(false)
  const refreshAll = () => {
    qc.invalidateQueries({ queryKey: ['item', item.id] })
    qc.invalidateQueries({ queryKey: ['children', item.id] })
    qc.invalidateQueries({ queryKey: ['podcasts'] })
  }
  const follow = useMutation({ mutationFn: (on: boolean) => api.followPodcast(item.id, on), onSuccess: refreshAll })
  const refresh = useMutation({ mutationFn: () => api.refreshPodcast(item.id), onSuccess: refreshAll })
  const markAll = useMutation({ mutationFn: (played: boolean) => api.markPodcastPlayed(item.id, played), onSuccess: refreshAll })
  const remove = useMutation({
    mutationFn: () => api.deletePodcast(item.id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['podcasts'] })
      navigate(`/library/${item.libraryId}`, { replace: true })
    },
  })
  const list = (episodes ?? []).filter((e) => filter === 'all' || !e.played)
  // Play picks up an episode the person started, else the newest they haven't heard.
  const next = episodes?.find((e) => !e.played && (e.positionMs ?? 0) > 0) ?? episodes?.find((e) => !e.played)
  const playingThis = !!audio.book && audio.book.parentId === item.id && B.isEpisode(audio.book)
  const following = item.feed?.following
  const kids = !!me.data?.kidsLevel

  return (
    <div className="animate-fade space-y-8">
      <Hero item={item} label={t('common:podcasts.podcast')} round={false}>
        {item.author && <span className="font-semibold text-text">{item.author}</span>}
        {episodes && <span>{t('common:count.episodes', { count: episodes.length })}</span>}
        {item.feed?.link && (
          <a href={item.feed.link} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 hover:text-text hover:underline">
            {t('common:podcasts.website')}
            <ExternalLink className="size-3" />
          </a>
        )}
      </Hero>

      <div className="relative flex flex-wrap items-center gap-3">
        <button
          disabled={!next && !playingThis}
          onClick={() => (playingThis ? audio.toggle() : next && audio.playItem(next))}
          className="flex h-14 items-center gap-2 rounded-full bg-accent ps-5 pe-6 font-semibold text-on-accent shadow-lg transition hover:scale-105 hover:bg-accent-strong disabled:opacity-50"
        >
          {playingThis && audio.playing ? <Pause className="size-6 fill-current" /> : <Play className="size-6 fill-current" />}
          {playingThis && audio.playing ? t('player.pause') : (next?.positionMs ?? 0) > 0 ? t('common:books.resume') : t('common:podcasts.playLatest')}
        </button>
        <Button
          className="h-11 rounded-full"
          variant={following ? 'secondary' : 'primary'}
          icon={following ? <BellOff className="size-4" /> : <Bell className="size-4" />}
          disabled={follow.isPending}
          onClick={() => follow.mutate(!following)}
        >
          {following ? t('common:podcasts.unfollow') : t('common:podcasts.follow')}
        </Button>
        <div className="relative">
          <Button variant="ghost" className="h-11 rounded-full" icon={<MoreHorizontal className="size-5" />} aria-label={t('common:podcasts.more')} onClick={() => setMenu(!menu)} />
          {menu && (
            <div className="absolute start-0 top-12 z-20 w-60 overflow-hidden rounded-xl border border-line bg-surface py-1 shadow-xl" onClick={() => setMenu(false)}>
              {!kids && (
                <MenuItem icon={<RefreshCw className="size-4" />} onClick={() => refresh.mutate()}>
                  {t('common:podcasts.checkNow')}
                </MenuItem>
              )}
              <MenuItem icon={<Check className="size-4" />} onClick={() => markAll.mutate(true)}>
                {t('common:podcasts.markAllPlayed')}
              </MenuItem>
              <MenuItem icon={<RotateCcw className="size-4" />} onClick={() => markAll.mutate(false)}>
                {t('common:podcasts.markAllUnplayed')}
              </MenuItem>
              {me.data?.isAdmin && (
                <MenuItem
                  icon={<Trash2 className="size-4" />}
                  danger
                  onClick={() => confirm(t('common:podcasts.confirmRemove', { name: item.title })) && remove.mutate()}
                >
                  {t('common:podcasts.remove')}
                </MenuItem>
              )}
            </div>
          )}
        </div>
        {refresh.isPending && <Spinner className="size-5" />}
        {refresh.data && <span className="text-sm text-muted">{t('common:podcasts.newEpisodesCount', { count: refresh.data.added })}</span>}
        {(refresh.error || remove.error) && <span className="text-sm text-danger">{(refresh.error ?? remove.error)?.message}</span>}
      </div>
      {me.data?.isAdmin && item.feed?.error && <p className="relative text-sm text-danger">{t('common:podcasts.feedProblem', { error: item.feed.error })}</p>}

      {item.overview && <Overview text={item.overview} />}

      <section className="relative space-y-2">
        <div className="flex items-center gap-2">
          <h2 className="text-lg font-semibold">{t('common:podcasts.episodesTitle')}</h2>
          <div className="ms-auto flex gap-1" role="tablist">
            {(['all', 'unplayed'] as const).map((f) => (
              <button
                key={f}
                role="tab"
                aria-selected={filter === f}
                onClick={() => setFilter(f)}
                className={`rounded-full px-3 py-1 text-sm ${filter === f ? 'bg-text font-medium text-ink' : 'bg-text/5 text-muted hover:text-text'}`}
              >
                {f === 'all' ? t('common:podcasts.all') : t('common:podcasts.unplayedTab')}
              </button>
            ))}
          </div>
        </div>
        {!episodes ? (
          <Spinner />
        ) : list.length === 0 ? (
          <p className="py-6 text-sm text-muted">{t('common:podcasts.allPlayed')}</p>
        ) : (
          <>
            <EpisodeList episodes={list.slice(0, shown)} />
            {list.length > shown && (
              <Button variant="ghost" onClick={() => setShown(shown + PAGE)}>
                {t('common:podcasts.showMore')}
              </Button>
            )}
          </>
        )}
      </section>
    </div>
  )
}

function MenuItem({ icon, danger, onClick, children }: { icon: React.ReactNode; danger?: boolean; onClick: () => void; children: React.ReactNode }) {
  return (
    <button onClick={onClick} className={`flex w-full items-center gap-3 px-4 py-2.5 text-start text-sm hover:bg-text/5 ${danger ? 'text-danger' : ''}`}>
      {icon}
      {children}
    </button>
  )
}

// One episode: its show, when it came out, a button that picks up where the
// person left off, and its show notes.
export function EpisodePage({ item }: { item: ItemDetail }) {
  const { t, i18n } = useTranslation()
  const qc = useQueryClient()
  const audio = useAudio()
  const playingThis = audio.book?.id === item.id
  const { time } = useBookTime()
  const length = item.durationMs ?? 0
  const at = playingThis ? time * 1000 : item.played ? 0 : (item.positionMs ?? 0)
  const mark = useMutation({
    mutationFn: (played: boolean) => api.setProgress(item.id, 0, played),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['item', item.id] })
      qc.invalidateQueries({ queryKey: ['children', item.parentId] })
    },
  })
  return (
    <div className="animate-fade space-y-8">
      <Hero item={item} label={item.episode?.episodeType === 'trailer' ? t('common:podcasts.trailer') : item.episode?.episodeType === 'bonus' ? t('common:podcasts.bonus') : t('common:podcasts.episode')} round={false}>
        {item.parentId && (
          <Link to={`/item/${item.parentId}`} className="font-semibold text-text hover:underline">
            {item.podcast}
          </Link>
        )}
        {item.released && <span>{shortDate(item.released, i18n.language)}</span>}
        {!!length && <span>{duration(length)}</span>}
      </Hero>
      <div className="relative flex flex-wrap items-center gap-3">
        <button
          onClick={() => (playingThis ? audio.toggle() : audio.playItem(item))}
          className="flex h-14 items-center gap-2 rounded-full bg-accent ps-5 pe-6 font-semibold text-on-accent shadow-lg transition hover:scale-105 hover:bg-accent-strong"
        >
          {playingThis && audio.playing ? <Pause className="size-6 fill-current" /> : <Play className="size-6 fill-current" />}
          {playingThis && audio.playing ? t('player.pause') : at > 0 ? t('common:books.resume') : t('item.play')}
        </button>
        {at > 0 && length > 0 && (
          <div className="min-w-40 space-y-1">
            <div className="h-1.5 overflow-hidden rounded-full bg-text/10">
              <div className="h-full bg-accent" style={{ width: `${Math.min(100, (at / length) * 100)}%` }} />
            </div>
            <div className="text-xs text-muted">{t('common:books.left', { time: duration(B.timeLeft(at / 1000, length / 1000, audio.speed) * 1000) })}</div>
          </div>
        )}
        <Button className="h-11 rounded-full" icon={item.played ? <RotateCcw className="size-4" /> : <Check className="size-4" />} onClick={() => mark.mutate(!item.played)}>
          {item.played ? t('common:podcasts.markUnplayed') : t('common:podcasts.markPlayed')}
        </Button>
        {item.episode?.link && (
          <a href={item.episode.link} target="_blank" rel="noreferrer" className="inline-flex h-11 items-center gap-2 rounded-full px-4 text-sm text-muted hover:bg-text/5 hover:text-text">
            <ExternalLink className="size-4" />
            {t('common:podcasts.episodePage')}
          </a>
        )}
      </div>
      <section className="relative max-w-3xl space-y-2">
        <h2 className="text-lg font-semibold">{t('common:podcasts.showNotes')}</h2>
        <p className="leading-relaxed whitespace-pre-line text-text/85">{item.overview || t('common:podcasts.noShowNotes')}</p>
      </section>
      <p className="sr-only">{episodeLine(item, t, i18n.language)}</p>
    </div>
  )
}
