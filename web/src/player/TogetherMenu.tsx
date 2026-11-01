import { Check, Copy, Crown, LogOut, Users, X } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { TogetherState } from '../api'
import { clientId } from '../remote'
import { formatCode, together, togetherLink } from './together'

// The people button in the player's controls: start Watch together, or see
// the code to share, who's watching, and leave or end it.
export function TogetherMenu({
  session,
  onStart,
  onOpenChange,
}: {
  session: TogetherState | undefined
  onStart: () => Promise<unknown>
  onOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [copied, setCopied] = useState(false)
  const ref = useRef<HTMLDivElement>(null)
  const toggle = (v: boolean) => {
    setOpen(v)
    onOpenChange(v)
    setError('')
  }

  useEffect(() => {
    if (!open) return
    const close = (e: PointerEvent) => !ref.current?.contains(e.target as Node) && toggle(false)
    window.addEventListener('pointerdown', close)
    return () => window.removeEventListener('pointerdown', close)
  })

  const start = async () => {
    setBusy(true)
    setError('')
    try {
      await onStart()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }
  const copy = async () => {
    if (!session) return
    const link = togetherLink(session.code)
    try {
      if (navigator.share && matchMedia('(pointer: coarse)').matches) {
        await navigator.share({ title: t('common:together.title'), text: t('common:together.shareText', { code: formatCode(session.code) }), url: link })
        return
      }
      await navigator.clipboard.writeText(link)
      setCopied(true)
      window.setTimeout(() => setCopied(false), 2000)
    } catch {
      // Share sheet closed, or no clipboard: the code is on screen.
    }
  }
  const mine = clientId()

  return (
    <div ref={ref} className="relative">
      <button
        onClick={() => toggle(!open)}
        className={`flex items-center gap-1.5 rounded-full p-2 hover:bg-white/10 ${session ? 'text-accent' : ''}`}
        aria-label={session ? t('common:together.watchingWith', { count: session.members.length }) : t('common:together.title')}
        aria-haspopup="dialog"
        aria-expanded={open}
      >
        <Users className="size-6" />
        {session && <span className="text-sm tabular-nums">{session.members.length}</span>}
      </button>
      {open && (
        <div
          role="dialog"
          aria-label={t('common:together.title')}
          className="absolute end-0 bottom-full mb-2 w-[min(20rem,calc(100vw-2rem))] rounded-xl border border-white/10 bg-black/90 p-4 shadow-2xl backdrop-blur"
        >
          <div className="flex items-center justify-between gap-2">
            <span className="text-xs font-medium tracking-wide text-white/50 uppercase">{t('common:together.title')}</span>
            <button onClick={() => toggle(false)} className="rounded-full p-1 text-white/60 hover:bg-white/10" aria-label={t('close')}>
              <X className="size-4" />
            </button>
          </div>
          {!session ? (
            <div className="mt-2 space-y-3">
              <p className="text-sm text-white/75">{t('common:together.intro')}</p>
              <button
                onClick={start}
                disabled={busy}
                className="w-full rounded-full bg-accent px-4 py-2 text-sm font-semibold text-on-accent hover:bg-accent-strong disabled:opacity-60"
              >
                {t('common:together.start')}
              </button>
            </div>
          ) : (
            <div className="mt-2 space-y-3">
              <p className="text-sm text-white/75">{t('common:together.share')}</p>
              <div className="flex items-center gap-2">
                <span dir="ltr" className="flex-1 rounded-lg bg-white/10 px-3 py-2 text-center font-mono text-2xl font-semibold tracking-[0.2em]">
                  {formatCode(session.code)}
                </span>
                <button
                  onClick={copy}
                  className="flex shrink-0 items-center gap-1.5 rounded-full bg-white/15 px-3 py-2 text-sm hover:bg-white/25"
                >
                  {copied ? <Check className="size-4 text-accent" /> : <Copy className="size-4" />}
                  {copied ? t('common:together.copied') : t('common:together.copyLink')}
                </button>
              </div>
              <ul className="space-y-1" aria-label={t('common:together.watchingWith', { count: session.members.length })}>
                {session.members.map((m) => (
                  <li key={m.clientId} className="flex items-center gap-2 text-sm">
                    <span className="grid size-7 shrink-0 place-items-center rounded-full bg-white/10 text-xs font-semibold uppercase">
                      {m.name[0]}
                    </span>
                    <span className="min-w-0 flex-1 truncate">
                      {m.name}
                      {m.clientId === mine && <span className="text-white/50"> {t('settings.people.you')}</span>}
                      {m.device && <span className="block truncate text-xs text-white/50">{m.device}</span>}
                    </span>
                    {m.host && <Crown className="size-4 text-accent" aria-label={t('common:together.host')} />}
                  </li>
                ))}
              </ul>
              <div className="flex gap-2">
                <button
                  onClick={() => (together.leave(), toggle(false))}
                  className="flex flex-1 items-center justify-center gap-1.5 rounded-full bg-white/15 px-3 py-2 text-sm hover:bg-white/25"
                >
                  <LogOut className="size-4 rtl:-scale-x-100" />
                  {t('common:together.leave')}
                </button>
                {session.members.length > 1 && (
                  <button
                    onClick={() => (together.end(), toggle(false))}
                    className="flex-1 rounded-full bg-white/15 px-3 py-2 text-sm hover:bg-white/25 hover:text-danger"
                  >
                    {t('common:together.end')}
                  </button>
                )}
              </div>
            </div>
          )}
          {error && <p className="mt-2 text-sm text-danger">{error}</p>}
        </div>
      )}
    </div>
  )
}
