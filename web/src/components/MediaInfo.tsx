import { useMutation, useQueryClient } from '@tanstack/react-query'
import { FileVideo, RefreshCw, X } from 'lucide-react'
import { useEffect, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { api, type ItemDetail, type User } from '../api'
import { duration } from '../format'
import { formatLocale } from '../i18n'
import {
  audioLanguages,
  audioTitle,
  bitrateLabel,
  codecName,
  containerName,
  hdrLabel,
  languageName,
  qualityBadges,
  sizeLabel,
  subtitleLabel,
} from '../media'
import { Button } from './ui'

export function QualityBadges({ item }: { item: ItemDetail }) {
  const badges = qualityBadges(item)
  if (badges.length === 0) return null
  return (
    <div className="flex flex-wrap gap-2">
      {badges.map((b) => (
        <span
          key={b.label}
          title={b.title}
          className="rounded-md border border-text/20 px-2 py-0.5 text-xs font-semibold tracking-wide text-text/90"
        >
          {b.label}
        </span>
      ))}
    </div>
  )
}

// Audio and subtitle languages, the other thing people check before pressing Play.
export function Languages({ item }: { item: ItemDetail }) {
  const { t } = useTranslation()
  const audio = item.mediaInfo ? audioLanguages(item.mediaInfo) : []
  const subs: string[] = []
  for (const s of item.subtitles ?? []) {
    const n = languageName(s.language) || s.label
    if (n && !subs.includes(n)) subs.push(n)
  }
  if (audio.length === 0 && subs.length === 0) return null
  const list = (names: string[]) => (names.length > 4 ? `${names.slice(0, 3).join(', ')} +${names.length - 3}` : names.join(', '))
  return (
    <dl className="flex flex-wrap gap-x-6 gap-y-1 text-sm">
      {audio.length > 0 && (
        <div className="flex gap-2">
          <dt className="text-muted">{t('file.audio')}</dt>
          <dd title={audio.join(', ')}>{list(audio)}</dd>
        </div>
      )}
      {subs.length > 0 && (
        <div className="flex gap-2">
          <dt className="text-muted">{t('file.subtitles')}</dt>
          <dd title={subs.join(', ')}>{list(subs)}</dd>
        </div>
      )}
    </dl>
  )
}

export function FileDetailsButton({ item, user }: { item: ItemDetail; user?: User }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  if (!item.playable) return null
  return (
    <>
      <Button variant="ghost" icon={<FileVideo className="size-4" />} onClick={() => setOpen(true)}>
        {t('file.title')}
      </Button>
      {open && <FileDetails item={item} user={user} onClose={() => setOpen(false)} />}
    </>
  )
}

function FileDetails({ item, user, onClose }: { item: ItemDetail; user?: User; onClose: () => void }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const again = useMutation({
    mutationFn: () => api.reanalyze(item.id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['item', item.id] }),
  })
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])
  const mi = item.mediaInfo
  const fileName = item.filePath?.split(/[\\/]/).pop()

  return (
    <div className="fixed inset-0 z-50 flex items-end justify-center bg-black/60 backdrop-blur-sm sm:items-center sm:p-6" onClick={onClose}>
      <div
        role="dialog"
        aria-modal="true"
        aria-label={t('file.title')}
        onClick={(e) => e.stopPropagation()}
        className="max-h-[90vh] w-full max-w-2xl overflow-y-auto rounded-t-2xl border border-line/60 bg-surface p-5 shadow-2xl sm:rounded-2xl md:p-6"
      >
        <div className="mb-5 flex items-start justify-between gap-4">
          <div className="min-w-0">
            <h2 className="text-lg font-semibold">{t('file.title')}</h2>
            <p className="truncate text-sm text-muted" title={item.filePath}>
              {fileName ?? item.title}
            </p>
          </div>
          <button onClick={onClose} aria-label={t('close')} className="text-muted hover:text-text">
            <X className="size-5" />
          </button>
        </div>

        {!mi ? (
          <p className="text-sm text-muted">{t('file.notYet')}</p>
        ) : (
          <div className="space-y-6">
            {mi.error && <p className="text-sm text-danger">{t('file.unreadable', { error: mi.error })}</p>}
            {item.mediaInfoStale && (
              <p className="text-sm text-muted">{t('file.stale')}</p>
            )}
            <Section title={t('file.file')}>
              <Facts
                rows={[
                  [t('file.location'), user?.isAdmin ? item.filePath : undefined],
                  [t('file.size'), sizeLabel(mi.size || item.fileSize)],
                  [t('file.format'), containerName(mi.container)],
                  [t('file.length'), duration(mi.durationMs)],
                  [t('file.bitrate'), bitrateLabel(mi.bitrate)],
                  [t('file.chapters'), mi.chapters ? String(mi.chapters) : ''],
                ]}
              />
            </Section>

            {mi.video.map((v) => (
              <Section key={`v${v.index}`} title={mi.video.length > 1 ? t('file.videoN', { n: v.index + 1 }) : t('file.video')}>
                <Facts
                  rows={[
                    [t('file.codec'), [codecName(v.codec), v.profile].filter(Boolean).join(' · ')],
                    [t('file.resolution'), `${v.resolution} · ${v.width}×${v.height}`],
                    [t('file.frameRate'), v.frameRate ? t('file.fps', { rate: v.frameRate }) : ''],
                    [t('file.bitDepth'), v.bitDepth ? t('common:media.bitDepth', { bits: v.bitDepth }) : ''],
                    [
                      t('file.dynamicRange'),
                      v.hdr
                        ? [
                            hdrLabel[v.hdr],
                            v.dvProfile ? (v.dvLevel ? t('file.dvProfileLevel', { profile: v.dvProfile, level: v.dvLevel }) : t('file.dvProfile', { profile: v.dvProfile })) : '',
                            v.hdrBase ? t('file.fallback', { format: hdrLabel[v.hdrBase] }) : '',
                          ]
                            .filter(Boolean)
                            .join(' · ')
                        : 'SDR',
                    ],
                    [t('file.color'), [v.colorPrimaries, v.colorTransfer, v.pixFmt].filter(Boolean).join(' · ')],
                    [t('file.bitrate'), bitrateLabel(v.bitrate)],
                  ]}
                />
              </Section>
            ))}

            {mi.audio.length > 0 && (
              <Section title={`${t('file.audio')} · ${mi.audio.length}`}>
                <Tracks
                  rows={mi.audio.map((a) => ({
                    key: a.index,
                    name: [languageName(a.language) || t('file.trackN', { n: a.index + 1 }), a.title && a.title !== languageName(a.language) ? a.title : '']
                      .filter(Boolean)
                      .join(' · '),
                    detail: [audioTitle(a), bitrateLabel(a.bitrate)].filter(Boolean).join(' · '),
                    flags: [
                      a.default && t('file.default'),
                      a.forced && t('file.forced'),
                      a.commentary && t('file.commentary'),
                      a.descriptive && t('file.audioDescription'),
                    ],
                  }))}
                />
              </Section>
            )}

            {(item.subtitles?.length ?? 0) > 0 && (
              <Section title={`${t('file.subtitles')} · ${item.subtitles!.length}`}>
                <Tracks
                  rows={item.subtitles!.map((s, i) => ({
                    key: i,
                    name: subtitleLabel(s),
                    detail: [codecName(s.codec), s.external ? t('file.separateFile') : t('file.inFile')].join(' · '),
                    flags: [s.default && t('file.default'), s.forced && t('file.forced'), s.sdh && t('file.sdh')],
                  }))}
                />
              </Section>
            )}

            <p className="text-xs text-faint">
              {t('file.recorded', { when: new Date(mi.analyzedAt * 1000).toLocaleString(formatLocale()) })}
            </p>
          </div>
        )}

        {user?.isAdmin && (
          <div className="mt-5 flex items-center justify-end gap-3">
            {again.isSuccess && <span className="text-sm text-muted">{t('file.queued')}</span>}
            {again.error && <span className="text-sm text-danger">{again.error.message}</span>}
            <Button variant="ghost" icon={<RefreshCw className="size-4" />} disabled={again.isPending || again.isSuccess} onClick={() => again.mutate()}>
              {t('file.analyzeAgain')}
            </Button>
          </div>
        )}
      </div>
    </div>
  )
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="space-y-2">
      <h3 className="text-xs font-medium tracking-wide text-muted uppercase">{title}</h3>
      {children}
    </section>
  )
}

function Facts({ rows }: { rows: [string, string | undefined][] }) {
  return (
    <dl className="grid grid-cols-[7rem_1fr] gap-x-4 gap-y-1.5 text-sm">
      {rows
        .filter(([, v]) => !!v)
        .map(([k, v]) => (
          <div key={k} className="contents">
            <dt className="text-muted">{k}</dt>
            <dd className="min-w-0 break-words">{v}</dd>
          </div>
        ))}
    </dl>
  )
}

function Tracks({ rows }: { rows: { key: number; name: string; detail: string; flags: (string | false | undefined)[] }[] }) {
  return (
    <ul className="divide-y divide-line/50 rounded-xl border border-line/50 text-sm">
      {rows.map((r) => (
        <li key={r.key} className="flex flex-wrap items-baseline gap-x-3 gap-y-1 px-3 py-2">
          <span className="font-medium">{r.name}</span>
          <span className="text-muted">{r.detail}</span>
          {r.flags.filter(Boolean).map((f) => (
            <span key={f as string} className="rounded bg-text/10 px-1.5 text-xs text-muted">
              {f}
            </span>
          ))}
        </li>
      ))}
    </ul>
  )
}
