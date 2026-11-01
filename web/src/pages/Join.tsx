import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, Copy, Smartphone, Users } from 'lucide-react'
import { useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api, imageUrl } from '../api'
import { Button, EmptyState, Field, inputClass, Spinner } from '../components/ui'
import { formatCode, together } from '../player/together'
import { AuthCard } from './Auth'

// The page an invite link opens (see Friends in Settings): who invited you
// and what they share, and a username and password to make your account.
export function InvitePage({ code }: { code: string }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const navigate = useNavigate()
  const info = useQuery({ queryKey: ['invite', code], queryFn: () => api.inviteInfo(code), retry: false })
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  if (info.isPending) {
    return (
      <div className="grid h-dvh place-items-center">
        <Spinner />
      </div>
    )
  }
  if (info.isError) {
    return (
      <AuthCard title={t('invite.notWorking')} subtitle={info.error.message}>
        <Button className="w-full" onClick={() => navigate('/', { replace: true })}>
          {t('auth.signIn')}
        </Button>
      </AuthCard>
    )
  }
  const { server, invitedBy, libraries } = info.data
  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      await api.acceptInvite(code, username, password)
      navigate(`/join/${code}`, { replace: true })
      await qc.invalidateQueries()
    } catch (err) {
      setError(err instanceof Error ? err.message : t('somethingWentWrong'))
    } finally {
      setBusy(false)
    }
  }
  return (
    <AuthCard
      title={t('invite.title', { server })}
      subtitle={invitedBy ? t('invite.invitedBy', { name: invitedBy }) : t('invite.invited')}
    >
      <ul className="-mt-2 mb-5 flex flex-wrap gap-1.5" aria-label={t('invite.shared')}>
        {libraries.map((l) => (
          <li key={l.id} className="rounded-full bg-text/10 px-3 py-1 text-xs">
            {l.name}
          </li>
        ))}
      </ul>
      <form onSubmit={submit} className="space-y-4">
        <Field label={t('invite.username')}>
          <input
            className={inputClass}
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            autoComplete="username"
            maxLength={40}
            autoFocus
            required
          />
        </Field>
        <Field label={t('auth.password')}>
          <input
            className={inputClass}
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="new-password"
            minLength={8}
            required
          />
        </Field>
        {error && <p className="text-sm text-danger">{error}</p>}
        <Button variant="primary" className="w-full" disabled={busy}>
          {busy ? t('oneMoment') : t('invite.join')}
        </Button>
      </form>
    </AuthCard>
  )
}

// After joining (or opening an invite while signed in): you're in, and how
// to watch on a phone or TV.
export function JoinedPage() {
  const { t } = useTranslation()
  const { data: server } = useQuery({ queryKey: ['server'], queryFn: api.server })
  const address = server?.remoteURL || location.origin
  const [copied, setCopied] = useState(false)
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(address)
      setCopied(true)
      window.setTimeout(() => setCopied(false), 2000)
    } catch {
      // The address is on screen.
    }
  }
  return (
    <div className="mx-auto max-w-xl space-y-6 px-4 py-10 md:px-0">
      <div>
        <h1 className="font-display text-3xl font-semibold">{t('invite.welcome', { server: server?.name || 'Matinee' })}</h1>
        <p className="mt-2 text-muted">{t('invite.welcomeHint')}</p>
      </div>
      <div className="space-y-3 rounded-xl border border-line/60 bg-surface/50 p-5">
        <h2 className="flex items-center gap-2 font-semibold">
          <Smartphone className="size-5 text-accent" /> {t('invite.apps')}
        </h2>
        <p className="text-sm text-muted">{t('invite.appsHint')}</p>
        <div className="flex items-center gap-2">
          <code dir="ltr" className="min-w-0 flex-1 truncate rounded-lg bg-text/10 px-3 py-2 text-sm">
            {address}
          </code>
          <Button onClick={copy} icon={copied ? <Check className="size-4 text-accent" /> : <Copy className="size-4" />}>
            {copied ? t('common:together.copied') : t('invite.copy')}
          </Button>
        </div>
        <a href="https://matinee.yeix.io/docs/apps/phone-and-tv/" target="_blank" rel="noreferrer" className="inline-block text-sm text-accent hover:underline">
          {t('invite.getApps')}
        </a>
      </div>
      <Link to="/" className="inline-flex h-10 items-center rounded-lg bg-accent px-4 text-sm font-semibold text-on-accent hover:bg-accent-strong">
        {t('invite.start')}
      </Link>
    </div>
  )
}

// A Watch together link: what's playing and who's in, then join. Without a
// code, a box to type one.
export function TogetherPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const param = useParams().code
  const [typed, setTyped] = useState('')
  const code = param?.toUpperCase().replace(/[^A-Z0-9]/g, '')
  const peek = useQuery({ queryKey: ['together', code], queryFn: () => api.peekTogether(code!), enabled: !!code, retry: false })
  const item = useQuery({
    queryKey: ['item', peek.data?.itemId],
    queryFn: () => api.item(peek.data!.itemId),
    enabled: !!peek.data?.canWatch,
  })
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  if (!code) {
    return (
      <div className="mx-auto max-w-sm px-4 py-16">
        <h1 className="flex items-center gap-2 text-2xl font-semibold">
          <Users className="size-6 text-accent" /> {t('common:together.title')}
        </h1>
        <p className="mt-2 text-sm text-muted">{t('common:together.typeCode')}</p>
        <form
          className="mt-6 flex gap-2"
          onSubmit={(e) => {
            e.preventDefault()
            navigate(`/together/${typed.toUpperCase().replace(/[^A-Z0-9]/g, '')}`)
          }}
        >
          <input
            className={`${inputClass} font-mono tracking-[0.2em] uppercase`}
            value={typed}
            onChange={(e) => setTyped(e.target.value)}
            placeholder={formatCode('ABCDEF')}
            aria-label={t('common:together.code')}
            autoFocus
            required
          />
          <Button variant="primary">{t('common:together.next')}</Button>
        </form>
      </div>
    )
  }
  if (peek.isPending) {
    return (
      <div className="grid place-items-center py-24">
        <Spinner />
      </div>
    )
  }
  if (peek.isError) {
    return (
      <EmptyState title={t('common:together.notFound')}>
        <Link to="/together" className="text-accent hover:underline">
          {t('common:together.tryAnother')}
        </Link>
      </EmptyState>
    )
  }
  const host = peek.data.members.find((m) => m.host)
  if (!peek.data.canWatch) return <EmptyState title={t('common:together.notShared')}>{t('common:together.notSharedHint')}</EmptyState>
  const join = async () => {
    setBusy(true)
    setError('')
    try {
      const s = await together.join(code)
      navigate(`/watch/${s.itemId}`)
    } catch (e) {
      setError(e instanceof Error ? e.message : t('somethingWentWrong'))
      setBusy(false)
    }
  }
  return (
    <div className="mx-auto flex max-w-2xl flex-col items-center gap-6 px-4 py-12 text-center sm:flex-row sm:items-start sm:text-start">
      {item.data && (
        <img src={imageUrl(item.data.parents.find((p) => p.kind === 'show')?.id ?? item.data.id, 'poster')} alt="" className="w-40 rounded-xl shadow-2xl" />
      )}
      <div className="space-y-4">
        <p className="text-sm font-medium tracking-wide text-accent uppercase">{t('common:together.title')}</p>
        <h1 className="text-2xl font-semibold">{item.data?.title ?? '…'}</h1>
        <p className="text-muted">
          {host ? t('common:together.hostedBy', { name: host.name, count: peek.data.members.length }) : t('common:together.waiting')}
        </p>
        <p dir="ltr" className="font-mono text-sm text-faint">
          {formatCode(code)}
        </p>
        {error && <p className="text-sm text-danger">{error}</p>}
        <Button variant="primary" onClick={join} disabled={busy} icon={<Users className="size-4" />}>
          {t('common:together.join')}
        </Button>
      </div>
    </div>
  )
}
