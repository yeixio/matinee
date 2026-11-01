import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, Copy, Link2, Share2, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { api, type Account, type Invite, type Library } from '../api'
import { formatLocale } from '../i18n'
import { LibraryIcon, useLibraries } from './Shell'
import { Button, Field, inputClass } from './ui'

// Kinds a friend gets ticked by default; home videos, photos and the like
// stay private until an admin ticks them.
const sharedByDefault = ['movies', 'tv']

// LibraryPicker ticks the libraries a friend sees.
export function LibraryPicker({ selected, onChange }: { selected: number[]; onChange: (ids: number[]) => void }) {
  const { t } = useTranslation()
  const { data: libraries = [] } = useLibraries()
  return (
    <fieldset className="space-y-1.5">
      <legend className="mb-1.5 text-xs font-medium tracking-wide text-muted uppercase">{t('friends.libraries')}</legend>
      <div className="grid gap-1.5 sm:grid-cols-2">
        {libraries.map((l) => (
          <label key={l.id} className="flex items-center gap-2 rounded-lg border border-line/60 px-3 py-2 text-sm hover:bg-text/5">
            <input
              type="checkbox"
              className="accent-accent"
              checked={selected.includes(l.id)}
              onChange={(e) => onChange(e.target.checked ? [...selected, l.id] : selected.filter((id) => id !== l.id))}
            />
            <LibraryIcon kind={l.kind} className="size-4 text-faint" />
            <span className="truncate">{l.name}</span>
          </label>
        ))}
      </div>
    </fieldset>
  )
}

const defaults = (libraries: Library[]) => libraries.filter((l) => sharedByDefault.includes(l.kind)).map((l) => l.id)

// Friends: invite links for people outside the household, who then see
// only the libraries picked for them.
export function Friends() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const { data: libraries = [] } = useLibraries()
  const { data } = useQuery({ queryKey: ['invites'], queryFn: api.invites })
  const [name, setName] = useState('')
  const [picked, setPicked] = useState<number[]>()
  const [days, setDays] = useState(7)
  const [made, setMade] = useState<Invite>()
  const selected = picked ?? defaults(libraries)
  const create = useMutation({
    mutationFn: () => api.createInvite(name.trim(), selected, days),
    onSuccess: (inv) => {
      setMade(inv)
      setName('')
      setPicked(undefined)
      qc.invalidateQueries({ queryKey: ['invites'] })
    },
  })
  const open = data?.invites.filter((i) => i.state === 'open') ?? []
  const past = data?.invites.filter((i) => i.state !== 'open') ?? []
  return (
    <section className="space-y-4">
      <div>
        <h2 className="text-lg font-semibold">{t('friends.title')}</h2>
        <p className="mt-1 text-sm text-muted">{t('friends.hint')}</p>
      </div>
      {data && !data.remoteReady && (
        <p className="rounded-lg border border-line/60 bg-surface/50 p-3 text-sm text-muted">
          {t('friends.homeOnly')}{' '}
          <Link to="/settings/network#remote-access" className="text-accent hover:underline">
            {t('friends.setUpRemote')}
          </Link>
        </p>
      )}
      <form
        onSubmit={(e) => {
          e.preventDefault()
          create.mutate()
        }}
        className="space-y-4 rounded-xl border border-line/60 bg-surface/50 p-4"
      >
        <div className="grid gap-3 sm:grid-cols-[1fr_auto]">
          <Field label={t('friends.forWho')}>
            <input className={inputClass} value={name} onChange={(e) => setName(e.target.value)} maxLength={60} placeholder={t('friends.forWhoHint')} />
          </Field>
          <Field label={t('friends.lasts')}>
            <select className={inputClass} value={days} onChange={(e) => setDays(Number(e.target.value))}>
              {[1, 7, 30].map((d) => (
                <option key={d} value={d}>
                  {t('friends.days', { count: d })}
                </option>
              ))}
            </select>
          </Field>
        </div>
        <LibraryPicker selected={selected} onChange={setPicked} />
        <Button variant="primary" icon={<Link2 className="size-4" />} disabled={create.isPending || selected.length === 0}>
          {t('friends.makeLink')}
        </Button>
        {create.error && <p className="text-sm text-danger">{create.error.message}</p>}
      </form>
      {made && <MadeInvite invite={made} />}
      {open.length > 0 && (
        <div className="divide-y divide-line/60 rounded-xl border border-line/60 bg-surface/50">
          {open.map((i) => (
            <InviteRow key={i.code} invite={i} libraries={libraries} />
          ))}
        </div>
      )}
      {past.length > 0 && (
        <details className="text-sm text-muted">
          <summary className="cursor-pointer">{t('friends.past', { count: past.length })}</summary>
          <ul className="mt-2 space-y-1">
            {past.map((i) => (
              <li key={i.code}>
                {i.state === 'used'
                  ? t('friends.usedBy', { name: i.usedBy, date: formatDate(i.usedAt!) })
                  : t('friends.expired', { name: i.name || formatCode(i.code), date: formatDate(i.expiresAt) })}
              </li>
            ))}
          </ul>
        </details>
      )}
    </section>
  )
}

const formatDate = (unix: number) => new Date(unix * 1000).toLocaleDateString(formatLocale())
const formatCode = (code: string) => code.replace(/(.{4})(?=.)/g, '$1-')

// The link just made, ready to send.
function MadeInvite({ invite }: { invite: Invite }) {
  const { t } = useTranslation()
  const [copied, setCopied] = useState(false)
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(invite.url)
      setCopied(true)
      window.setTimeout(() => setCopied(false), 2000)
    } catch {
      // The link is on screen to copy by hand.
    }
  }
  const share = () => navigator.share?.({ title: t('friends.shareTitle'), text: t('friends.shareText'), url: invite.url }).catch(() => {})
  return (
    <div className="space-y-2 rounded-xl border border-accent/40 bg-accent/5 p-4">
      <p className="text-sm font-medium">{t('friends.made')}</p>
      <div className="flex flex-wrap items-center gap-2">
        <code dir="ltr" className="min-w-0 flex-1 truncate rounded-lg bg-text/10 px-3 py-2 text-sm select-all">
          {invite.url}
        </code>
        <Button onClick={copy} icon={copied ? <Check className="size-4 text-accent" /> : <Copy className="size-4" />}>
          {copied ? t('common:together.copied') : t('invite.copy')}
        </Button>
        {'share' in navigator && (
          <Button onClick={share} icon={<Share2 className="size-4" />}>
            {t('friends.share')}
          </Button>
        )}
      </div>
      <p className="text-xs text-muted">{t('friends.onceOnly', { date: formatDate(invite.expiresAt) })}</p>
    </div>
  )
}

function InviteRow({ invite, libraries }: { invite: Invite; libraries: Library[] }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [copied, setCopied] = useState(false)
  const remove = useMutation({
    mutationFn: () => api.deleteInvite(invite.code),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['invites'] }),
  })
  const names = libraries.filter((l) => invite.libraries.includes(l.id)).map((l) => l.name)
  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-2 p-4">
      <div className="min-w-0 flex-1">
        <div className="truncate">{invite.name || t('friends.unnamed')}</div>
        <div className="truncate text-xs text-muted">
          {t('friends.inviteLine', { libraries: names.join(', '), date: formatDate(invite.expiresAt) })}
        </div>
      </div>
      <Button
        variant="ghost"
        className="h-9 px-3"
        icon={copied ? <Check className="size-4 text-accent" /> : <Copy className="size-4" />}
        onClick={() =>
          navigator.clipboard
            .writeText(invite.url)
            .then(() => (setCopied(true), window.setTimeout(() => setCopied(false), 2000)))
            .catch(() => {})
        }
      >
        {copied ? t('common:together.copied') : t('invite.copy')}
      </Button>
      <Button variant="ghost" className="h-9 px-3 hover:text-danger" icon={<Trash2 className="size-4" />} disabled={remove.isPending} onClick={() => remove.mutate()}>
        {t('friends.takeBack')}
      </Button>
    </div>
  )
}

// FriendLibraries changes what a friend sees, or lets them see everything.
export function FriendLibraries({ user, onDone }: { user: Account; onDone: () => void }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [selected, setSelected] = useState(user.libraries ?? [])
  const save = useMutation({
    mutationFn: (friend: boolean) => api.setFriend(user.id, friend, friend ? selected : undefined),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['users'] })
      onDone()
    },
  })
  return (
    <div className="space-y-3 rounded-lg border border-line/60 p-3">
      <LibraryPicker selected={selected} onChange={setSelected} />
      <div className="flex flex-wrap justify-end gap-2">
        <Button variant="ghost" onClick={onDone}>
          {t('cancel')}
        </Button>
        <Button variant="ghost" disabled={save.isPending} onClick={() => save.mutate(false)}>
          {t('friends.seeEverything')}
        </Button>
        <Button variant="primary" disabled={save.isPending || selected.length === 0} onClick={() => save.mutate(true)}>
          {t('save')}
        </Button>
      </div>
      {save.error && <p className="text-sm text-danger">{save.error.message}</p>}
    </div>
  )
}
