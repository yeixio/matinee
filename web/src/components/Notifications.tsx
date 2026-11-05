import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Bell, CircleAlert, Download, Play, ShieldAlert, X } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'
import { api, type AppNotification } from '../api'
import { remote } from '../remote'
import { ago } from './ServerHealth'

// Notifications (see internal/api/notifications.go): Box Office telling
// people that what they asked for is on its way or ready to watch, and
// admins about downloads that failed. The bell lists them; one that
// arrives while the page is open also pops up for a few seconds. The same
// one again (a disk that's still nearly full) is counted on the one in the
// list, and pops up again only when it's got more serious.

const key = ['notifications']

function Icon({ n, className = 'size-4' }: { n: AppNotification; className?: string }) {
  if (n.level === 'critical') return <ShieldAlert className={`${className} text-danger`} />
  if (n.level === 'warning') return <CircleAlert className={`${className} text-accent`} />
  if (n.itemId) return <Play className={`${className} text-accent`} />
  return <Download className={`${className} text-muted`} />
}

export function NotificationBell() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const navigate = useNavigate()
  const [open, setOpen] = useState(false)
  const panel = useRef<HTMLDivElement>(null)
  const { data } = useQuery({ queryKey: key, queryFn: api.notifications, refetchInterval: 120_000, retry: false })
  const read = useMutation({ mutationFn: () => api.readNotifications(), onSuccess: () => qc.invalidateQueries({ queryKey: key }) })
  const clear = useMutation({ mutationFn: (ids?: string[]) => api.clearNotifications(ids), onSuccess: () => qc.invalidateQueries({ queryKey: key }) })
  const list = data?.notifications ?? []
  const unread = data?.unread ?? 0

  useEffect(() => remote.onNotify(() => qc.invalidateQueries({ queryKey: key })), [qc])
  useEffect(() => {
    if (!open) return
    const away = (e: MouseEvent) => panel.current && !panel.current.contains(e.target as Node) && setOpen(false)
    const esc = (e: KeyboardEvent) => e.key === 'Escape' && setOpen(false)
    document.addEventListener('mousedown', away)
    document.addEventListener('keydown', esc)
    return () => {
      document.removeEventListener('mousedown', away)
      document.removeEventListener('keydown', esc)
    }
  }, [open])

  // Nobody has any until Box Office is connected, so there's no bell either.
  if (!data || (list.length === 0 && !open)) return null

  const toggle = () => {
    setOpen(!open)
    if (!open && unread > 0) read.mutate()
  }
  return (
    <div className="relative" ref={panel}>
      <button
        onClick={toggle}
        className={`relative rounded-full p-2 transition-colors hover:bg-text/5 hover:text-text ${open ? 'text-text' : 'text-muted'}`}
        aria-label={t('notifications.title')}
        title={t('notifications.title')}
        aria-expanded={open}
      >
        <Bell className="size-5" />
        {unread > 0 && (
          <span className="absolute -top-0.5 -end-0.5 grid h-4 min-w-4 place-items-center rounded-full bg-accent px-1 text-[10px] font-semibold text-on-accent">
            {unread}
          </span>
        )}
      </button>
      {open && (
        <div className="absolute end-0 top-full z-40 mt-2 w-[min(24rem,calc(100vw-2rem))] overflow-hidden rounded-xl border border-line/60 bg-raised shadow-xl">
          <div className="flex items-center justify-between border-b border-line/60 px-4 py-2.5">
            <span className="text-sm font-semibold">{t('notifications.title')}</span>
            {list.length > 0 && (
              <button className="text-xs text-muted hover:text-text" onClick={() => clear.mutate(undefined)}>
                {t('notifications.clear')}
              </button>
            )}
          </div>
          {list.length === 0 ? (
            <p className="px-4 py-6 text-sm text-muted">{t('notifications.empty')}</p>
          ) : (
            <ul className="max-h-[60vh] divide-y divide-line/40 overflow-y-auto">
              {list.map((n) => (
                <li key={n.id} className={`group flex gap-3 px-4 py-3 ${n.read ? '' : 'bg-accent/5'}`}>
                  <Icon n={n} className="mt-0.5 size-4 shrink-0" />
                  <div className="min-w-0 flex-1">
                    <div className="text-sm font-medium">{n.title}</div>
                    {n.body && <div className="mt-0.5 text-xs text-muted">{n.body}</div>}
                    <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted">
                      <span>{ago(n.createdAt * 1000)}</span>
                      {(n.repeats ?? 0) > 1 && (
                        <span className="rounded-full bg-text/10 px-1.5 py-px font-medium tabular-nums">
                          {t('common:notifications.repeated', { times: n.repeats ?? 0, first: ago((n.firstAt ?? n.createdAt) * 1000) })}
                        </span>
                      )}
                      {n.itemId && (
                        <button
                          className="font-medium text-accent hover:underline"
                          onClick={() => {
                            setOpen(false)
                            navigate(`/item/${n.itemId}`)
                          }}
                        >
                          {t('notifications.open')}
                        </button>
                      )}
                    </div>
                  </div>
                  <button
                    className="self-start rounded p-0.5 text-muted opacity-0 group-hover:opacity-100 hover:text-text focus:opacity-100"
                    aria-label={t('close')}
                    onClick={() => clear.mutate([n.id])}
                  >
                    <X className="size-3.5" />
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
    </div>
  )
}

// NotificationToasts pops up notifications as they arrive.
export function NotificationToasts() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [shown, setShown] = useState<AppNotification[]>([])
  useEffect(
    () =>
      remote.onNotify((n) => {
        setShown((s) => [...s.filter((x) => x.id !== n.id), n].slice(-3))
        window.setTimeout(() => setShown((s) => s.filter((x) => x.id !== n.id)), 8000)
      }),
    [],
  )
  if (shown.length === 0) return null
  return (
    <div className="pointer-events-none fixed inset-x-4 bottom-24 z-50 flex flex-col items-end gap-2 md:inset-x-auto md:end-6 md:bottom-6" aria-live="polite">
      {shown.map((n) => (
        <div key={n.id} className="pointer-events-auto flex w-full max-w-sm gap-3 rounded-xl border border-line/60 bg-raised p-3 shadow-xl">
          <Icon n={n} className="mt-0.5 size-4 shrink-0" />
          <div className="min-w-0 flex-1">
            <div className="text-sm font-medium">{n.title}</div>
            {n.body && <div className="mt-0.5 text-xs text-muted">{n.body}</div>}
            {n.itemId && (
              <button className="mt-1 text-xs font-medium text-accent hover:underline" onClick={() => navigate(`/item/${n.itemId}`)}>
                {t('notifications.open')}
              </button>
            )}
          </div>
          <button className="self-start text-muted hover:text-text" aria-label={t('close')} onClick={() => setShown((s) => s.filter((x) => x.id !== n.id))}>
            <X className="size-4" />
          </button>
        </div>
      ))}
    </div>
  )
}
