import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ChevronDown, Eye, EyeOff, Plus, RefreshCw, Trash2, X } from 'lucide-react'
import { useMemo, useState, type FormEvent } from 'react'
import { Trans, useTranslation } from 'react-i18next'
import { api, ApiError, type LiveChoice, type LiveFilter, type LiveSource } from '../api'
import { ChannelLogo } from './ChannelLogo'
import { Button, Field, inputClass, Spinner } from './ui'

const errorText = (e: unknown, fallback: string) => (e instanceof ApiError ? e.message : fallback)

// Settings → Live TV: the playlists channels come from, each with its
// guide, how its channels' checks went, and its channels to hide.
export function LiveTVSettings() {
  const { t } = useTranslation()
  const sources = useQuery({
    queryKey: ['live', 'sources'],
    queryFn: api.liveSources,
    // While a new source's guide and checks run in the background.
    refetchInterval: (q) => (q.state.data?.some((s) => !s.guideAt && (s.guideUrl || s.playlistUrl)) ? 5000 : false),
  })
  const [adding, setAdding] = useState(false)
  return (
    <section className="space-y-4">
      <h2 className="text-lg font-semibold">{t('common:live.title')}</h2>
      <p className="text-sm text-muted">{t('live.settings.intro')}</p>
      {sources.isPending ? (
        <Spinner />
      ) : (
        <ul className="space-y-3">
          {sources.data?.map((s) => (
            <SourceRow key={s.id} source={s} />
          ))}
        </ul>
      )}
      {adding || !sources.data?.length ? (
        <AddSource onDone={() => setAdding(false)} />
      ) : (
        <Button icon={<Plus className="size-4" />} onClick={() => setAdding(true)}>
          {t('live.settings.add')}
        </Button>
      )}
    </section>
  )
}

function SourceRow({ source }: { source: LiveSource }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [open, setOpen] = useState(false)
  const done = () => qc.invalidateQueries({ queryKey: ['live'] })
  const refresh = useMutation({ mutationFn: () => api.refreshLiveSource(source.id), onSettled: done })
  const remove = useMutation({ mutationFn: () => api.deleteLiveSource(source.id), onSuccess: done })
  const playing = source.channels - source.broken - source.hidden
  const picked = source.iptvOrg
    ? [...(source.iptvOrg.countries ?? []), ...(source.iptvOrg.categories ?? []), ...(source.iptvOrg.languages ?? [])].join(', ') || t('live.settings.everything')
    : source.playlistUrl
  return (
    <li className="rounded-xl border border-line/60 bg-surface/50 p-4">
      <div className="flex flex-wrap items-start gap-3">
        <div className="min-w-0 flex-1">
          <p className="font-medium">{source.name}</p>
          <p className="truncate text-xs text-muted">{source.iptvOrg ? `iptv-org · ${picked}` : picked}</p>
          <p className="mt-1 text-sm">
            {t('live.settings.counts', { count: source.channels, playing, broken: source.broken, hidden: source.hidden })}
          </p>
          <p className="text-sm text-muted">
            {source.programs
              ? t('live.settings.guideShows', { count: source.programs })
              : source.guideError
                ? t('live.settings.guideFailed', { reason: source.guideError })
                : source.guideAt || (!source.guideUrl && source.refreshedAt)
                  ? t('live.settings.noGuide')
                  : t('live.settings.guideLoading')}
          </p>
          {source.error && <p className="mt-1 text-sm text-danger">{t('live.settings.refreshFailed', { reason: source.error })}</p>}
          {refresh.error && <p className="mt-1 text-sm text-danger">{errorText(refresh.error, t('somethingWentWrong'))}</p>}
        </div>
        <div className="flex gap-1">
          <Button variant="ghost" className="h-9 px-2.5" onClick={() => refresh.mutate()} disabled={refresh.isPending} title={t('live.settings.refresh')} aria-label={t('live.settings.refresh')}>
            <RefreshCw className={`size-4 ${refresh.isPending ? 'animate-spin' : ''}`} />
          </Button>
          <Button
            variant="ghost"
            className="h-9 px-2.5"
            onClick={() => confirm(t('live.settings.removeConfirm', { name: source.name })) && remove.mutate()}
            title={t('live.settings.remove')}
            aria-label={t('live.settings.remove')}
          >
            <Trash2 className="size-4" />
          </Button>
        </div>
      </div>
      <button onClick={() => setOpen(!open)} className="mt-3 flex items-center gap-1 text-sm text-accent" aria-expanded={open}>
        <ChevronDown className={`size-4 transition-transform ${open ? 'rotate-180' : ''}`} />
        {t('common:live.channels')}
      </button>
      {open && <SourceChannels id={source.id} />}
    </li>
  )
}

// A source's channels, broken and hidden ones too: an admin hides ones
// nobody wants, or shows them again.
function SourceChannels({ id }: { id: string }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const { data } = useQuery({ queryKey: ['live', 'sources', id, 'channels'], queryFn: () => api.liveSourceChannels(id) })
  const [q, setQ] = useState('')
  const hide = useMutation({
    mutationFn: ({ channel, hidden }: { channel: string; hidden: boolean }) => api.hideLiveChannel(channel, hidden),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['live'] }),
  })
  const shown = (data ?? []).filter((c) => !q || c.name.toLowerCase().includes(q.toLowerCase()))
  if (!data) return <Spinner className="mt-3" />
  return (
    <div className="mt-3 space-y-2">
      <input value={q} onChange={(e) => setQ(e.target.value)} placeholder={t('common:live.find')} className={inputClass} />
      <ul className="max-h-96 divide-y divide-line/40 overflow-y-auto rounded-lg border border-line/60">
        {shown.slice(0, 300).map((c) => (
          <li key={c.id} className={`flex items-center gap-3 px-3 py-2 ${c.hidden || c.broken ? 'opacity-60' : ''}`}>
            <ChannelLogo channel={c} className="size-8" />
            <div className="min-w-0 flex-1">
              <p className="truncate text-sm">{c.name}</p>
              <p className="text-xs text-muted">
                {c.broken ? t('live.settings.broken') : c.checkedAt ? t('live.settings.works') : t('live.settings.notChecked')}
                {c.hidden && ` · ${t('live.settings.hidden')}`}
              </p>
            </div>
            <Button
              variant="ghost"
              className="h-8 px-2"
              onClick={() => hide.mutate({ channel: c.id, hidden: !c.hidden })}
              aria-label={c.hidden ? t('live.settings.show') : t('live.settings.hide')}
              title={c.hidden ? t('live.settings.show') : t('live.settings.hide')}
            >
              {c.hidden ? <Eye className="size-4" /> : <EyeOff className="size-4" />}
            </Button>
          </li>
        ))}
      </ul>
      {shown.length > 300 && <p className="text-xs text-muted">{t('live.settings.more')}</p>}
    </div>
  )
}

// Adding a source: iptv-org's lists, picked by country, category and
// language, or any M3U playlist with an XMLTV guide.
function AddSource({ onDone }: { onDone: () => void }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [kind, setKind] = useState<'iptv-org' | 'playlist'>('iptv-org')
  const [name, setName] = useState('')
  const [playlist, setPlaylist] = useState('')
  const [guide, setGuide] = useState('')
  const [filter, setFilter] = useState<Required<LiveFilter>>({ countries: [], categories: [], languages: [] })
  const choices = useQuery({ queryKey: ['live', 'iptv-org'], queryFn: api.iptvOrgChoices, enabled: kind === 'iptv-org', staleTime: Infinity, retry: 1 })
  const add = useMutation({
    mutationFn: () =>
      api.addLiveSource(
        kind === 'iptv-org'
          ? { name: name.trim() || undefined, iptvOrg: filter, guideUrl: guide.trim() || undefined }
          : { name: name.trim() || undefined, playlistUrl: playlist.trim(), guideUrl: guide.trim() || undefined },
      ),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['live'] })
      onDone()
    },
  })
  const submit = (e: FormEvent) => {
    e.preventDefault()
    add.mutate()
  }
  const picks = filter.countries.length + filter.categories.length + filter.languages.length
  return (
    <form onSubmit={submit} className="space-y-4 rounded-xl border border-line/60 bg-surface/50 p-4">
      <div className="flex rounded-lg bg-text/5 p-1" role="tablist">
        {(['iptv-org', 'playlist'] as const).map((k) => (
          <button
            key={k}
            type="button"
            role="tab"
            aria-selected={kind === k}
            onClick={() => setKind(k)}
            className={`flex-1 rounded-md px-3 py-1.5 text-sm ${kind === k ? 'bg-surface text-text shadow' : 'text-muted hover:text-text'}`}
          >
            {k === 'iptv-org' ? t('live.settings.suggested') : t('live.settings.ownPlaylist')}
          </button>
        ))}
      </div>
      {kind === 'iptv-org' ? (
        <div className="space-y-3">
          <p className="text-sm text-muted">
            <Trans
              i18nKey="live.settings.iptvOrgIntro"
              components={{ site: <a href="https://github.com/iptv-org/iptv" target="_blank" rel="noreferrer" className="text-accent underline" /> }}
            />
          </p>
          {choices.isPending ? (
            <Spinner />
          ) : choices.error ? (
            <p className="text-sm text-danger">{errorText(choices.error, t('somethingWentWrong'))}</p>
          ) : (
            <>
              <MultiPick label={t('live.settings.countries')} options={choices.data.countries} value={filter.countries} onChange={(countries) => setFilter({ ...filter, countries })} />
              <MultiPick label={t('live.settings.categories')} options={choices.data.categories} value={filter.categories} onChange={(categories) => setFilter({ ...filter, categories })} />
              <MultiPick label={t('live.settings.languages')} options={choices.data.languages} value={filter.languages} onChange={(languages) => setFilter({ ...filter, languages })} />
              <p className="text-xs text-muted">{picks ? t('live.settings.pickHint') : t('live.settings.pickNothing')}</p>
            </>
          )}
          <p className="text-xs text-muted">{t('live.settings.yourCall')}</p>
        </div>
      ) : (
        <Field label={t('live.settings.playlist')}>
          <input required type="url" inputMode="url" spellCheck={false} placeholder="https://…/channels.m3u" className={inputClass} value={playlist} onChange={(e) => setPlaylist(e.target.value)} />
        </Field>
      )}
      <Field label={t('live.settings.guide')}>
        <input type="url" inputMode="url" spellCheck={false} placeholder="https://…/guide.xml.gz" className={inputClass} value={guide} onChange={(e) => setGuide(e.target.value)} />
      </Field>
      <p className="-mt-2 text-xs text-muted">{t('live.settings.guideHint')}</p>
      <Field label={t('live.settings.name')}>
        <input className={inputClass} value={name} onChange={(e) => setName(e.target.value)} placeholder={kind === 'iptv-org' ? 'iptv-org' : ''} />
      </Field>
      {add.error && <p className="text-sm text-danger">{errorText(add.error, t('somethingWentWrong'))}</p>}
      <div className="flex items-center gap-2">
        <Button variant="primary" type="submit" disabled={add.isPending || (kind === 'iptv-org' && !choices.data)}>
          {add.isPending ? t('live.settings.reading') : t('live.settings.addThis')}
        </Button>
        <Button type="button" variant="ghost" onClick={onDone}>
          {t('cancel')}
        </Button>
      </div>
    </form>
  )
}

// Picks any number of choices, found by typing.
function MultiPick({ label, options, value, onChange }: { label: string; options: LiveChoice[]; value: string[]; onChange: (v: string[]) => void }) {
  const { t } = useTranslation()
  const [q, setQ] = useState('')
  const byCode = useMemo(() => new Map(options.map((o) => [o.code, o])), [options])
  const found = q.trim()
    ? options.filter((o) => !value.includes(o.code) && (o.name.toLowerCase().includes(q.trim().toLowerCase()) || o.code.toLowerCase() === q.trim().toLowerCase())).slice(0, 8)
    : []
  return (
    <div className="space-y-1.5">
      <span className="text-xs font-medium tracking-wide text-muted uppercase">{label}</span>
      <div className="flex flex-wrap gap-1.5">
        {value.map((c) => (
          <span key={c} className="flex items-center gap-1 rounded-full bg-accent/15 py-1 ps-3 pe-1 text-sm">
            {byCode.get(c)?.flag} {byCode.get(c)?.name ?? c}
            <button type="button" onClick={() => onChange(value.filter((v) => v !== c))} className="rounded-full p-0.5 hover:bg-text/10" aria-label={t('live.settings.unpick', { name: byCode.get(c)?.name ?? c })}>
              <X className="size-3.5" />
            </button>
          </span>
        ))}
      </div>
      <div className="relative">
        <input value={q} onChange={(e) => setQ(e.target.value)} placeholder={t('live.settings.anyPick')} aria-label={label} className={inputClass} />
        {found.length > 0 && (
          <ul className="absolute inset-x-0 top-full z-20 mt-1 overflow-hidden rounded-lg border border-line bg-surface shadow-lg">
            {found.map((o) => (
              <li key={o.code}>
                <button
                  type="button"
                  onClick={() => {
                    onChange([...value, o.code])
                    setQ('')
                  }}
                  className="w-full px-3 py-2 text-start text-sm hover:bg-text/5"
                >
                  {o.flag} {o.name}
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  )
}
