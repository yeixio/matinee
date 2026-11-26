import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, ExternalLink, Monitor, Server, Trash2 } from 'lucide-react'
import { useEffect, useState, type FormEvent, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { api, ApiError, type SteamPCSetup } from '../api'
import { Button, Field, inputClass } from '../components/ui'

const sunshineUrl = 'https://app.lizardbyte.dev/Sunshine/'
const guideUrl = '/docs/server/games/#steam-on-a-pc-you-already-have-preview'

function errorText(e: unknown, fallback: string) {
  return e instanceof ApiError ? e.message : fallback
}

/**
 * Steam setup (preview), for admins on the Games page: the two ways the
 * Steam button can play Steam, side by side, with which one it uses now.
 * A PC someone already plays Steam on is set up here, by pairing with
 * Sunshine on it; a Steam container is set up on the server, so it only
 * says whether there is one and points to the guide. Hidden when the
 * server can't play a PC (no matinee-pcremote) or for someone who isn't an
 * admin. At the top of a Games library (unsetOnly) it shows only while
 * neither way is set up, so an admin finds where to start; once Steam can
 * be played, the Steam button there takes its place.
 */
export function SteamSetup({ unsetOnly = false }: { unsetOnly?: boolean }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const setup = useQuery({
    queryKey: ['steampc'],
    queryFn: api.steamPC,
    retry: false,
    // While a pairing waits for its PIN, look every couple of seconds.
    refetchInterval: (q) => (q.state.data?.pairing?.state === 'waiting' ? 2000 : false),
  })
  const refresh = () => {
    qc.invalidateQueries({ queryKey: ['steampc'] })
    qc.invalidateQueries({ queryKey: ['game-host'] })
  }
  const paired = setup.data?.pairing?.state === 'paired'
  useEffect(() => {
    if (paired) qc.invalidateQueries({ queryKey: ['game-host'] })
  }, [paired, qc])

  const data = setup.data
  if (!data?.available) return null
  if (unsetOnly && (data.pc || data.container || data.pairing?.state === 'paired')) return null
  return (
    <section className="space-y-3">
      <h2 className="text-lg font-semibold">{t('games.steampc.title')}</h2>
      <p className="text-sm text-muted">{t('games.steampc.hint')}</p>
      <div className="space-y-3 rounded-xl border border-line/60 bg-surface/50 p-4">
        <ModeHeading icon={<Monitor className="size-6 shrink-0 text-accent" />} title={t('games.steampc.pcTitle')} active={data.active === 'pc'} />
        <p className="text-sm text-muted">{t('games.steampc.pcHint')}</p>
        {data.pc ? <PairedPC setup={data} onChange={refresh} /> : <Pair setup={data} onChange={refresh} />}
      </div>
      <div className="space-y-2 rounded-xl border border-line/60 bg-surface/50 p-4">
        <ModeHeading icon={<Server className="size-6 shrink-0 text-muted" />} title={t('games.steampc.containerTitle')} active={data.active === 'container'} />
        <p className="text-sm text-muted">{t('games.steampc.containerHint')}</p>
        <p className="text-sm">{data.container ? t('games.steampc.containerOn') : t('games.steampc.containerOff')}</p>
      </div>
      {data.pc && data.container && <p className="text-sm text-muted">{t('games.steampc.bothNote')}</p>}
      <a href={guideUrl} target="_blank" rel="noreferrer" className="inline-flex items-center gap-2 text-sm font-medium text-accent hover:underline">
        <ExternalLink className="size-4" />
        {t('games.steampc.guide')}
      </a>
    </section>
  )
}

function ModeHeading({ icon, title, active }: { icon: ReactNode; title: string; active: boolean }) {
  const { t } = useTranslation()
  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
      {icon}
      <div className="font-medium">{title}</div>
      {active && (
        <span className="inline-flex items-center gap-1 rounded-full bg-accent/15 px-2 py-0.5 text-xs font-medium text-accent">
          <Check className="size-3" />
          {t('games.steampc.active')}
        </span>
      )}
    </div>
  )
}

function PairedPC({ setup, onChange }: { setup: SteamPCSetup; onChange: () => void }) {
  const { t } = useTranslation()
  const pc = setup.pc!
  const remove = useMutation({
    mutationFn: api.removeSteamPC,
    onSuccess: onChange,
  })
  const state = {
    ready: t('games.steampc.stateReady'),
    busy: t('games.steampc.stateBusy'),
    off: t('games.steampc.stateOff'),
    unpaired: t('games.steampc.stateUnpaired'),
  }[pc.state]
  return (
    <div className="flex flex-wrap items-center gap-x-4 gap-y-2 rounded-lg bg-text/5 p-3">
      <div className="min-w-48 flex-1">
        <div className="font-medium">{pc.name}</div>
        <div className="text-sm text-muted" dir="auto">
          {[pc.host, state].join(' · ')}
        </div>
        {pc.state === 'off' && pc.error && <p className="text-sm text-danger">{pc.error}</p>}
      </div>
      <Button
        variant="danger"
        icon={<Trash2 className="size-4" />}
        disabled={remove.isPending}
        onClick={() => {
          if (window.confirm(t('games.steampc.removeConfirm', { name: pc.name }))) remove.mutate()
        }}
      >
        {t('games.steampc.remove')}
      </Button>
    </div>
  )
}

function Pair({ setup, onChange }: { setup: SteamPCSetup; onChange: () => void }) {
  const { t } = useTranslation()
  const [address, setAddress] = useState('')
  const start = useMutation({
    mutationFn: () => api.pairSteamPC(address.trim()),
    onSuccess: onChange,
  })
  const cancel = useMutation({
    mutationFn: api.cancelSteamPCPairing,
    onSuccess: onChange,
  })
  const p = setup.pairing

  if (p?.state === 'waiting') {
    const pinPage = `https://${p.host}:47990/pin`
    return (
      <div className="space-y-3 rounded-lg bg-text/5 p-4" aria-live="polite">
        <div className="font-medium">{t('games.steampc.pinTitle', { name: p.name || p.host })}</div>
        <div className="font-mono text-4xl font-semibold tracking-[0.3em]" dir="ltr">
          {p.pin}
        </div>
        <p className="text-sm text-muted">{t('games.steampc.pinHint')}</p>
        <div className="flex flex-wrap items-center gap-3">
          <a href={pinPage} target="_blank" rel="noreferrer" className="inline-flex items-center gap-2 text-sm font-medium text-accent hover:underline">
            <ExternalLink className="size-4" />
            {t('games.steampc.openPin')}
          </a>
          <Button variant="ghost" onClick={() => cancel.mutate()}>
            {t('cancel')}
          </Button>
        </div>
      </div>
    )
  }

  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (address.trim()) start.mutate()
  }
  return (
    <form onSubmit={submit} className="space-y-3">
      <ol className="list-decimal space-y-1 ps-5 text-sm text-muted">
        <li>
          {t('games.steampc.step1')}{' '}
          <a href={sunshineUrl} target="_blank" rel="noreferrer" className="font-medium text-accent hover:underline">
            {t('games.steampc.getSunshine')}
          </a>
        </li>
        <li>{t('games.steampc.step2')}</li>
        <li>{t('games.steampc.step3')}</li>
      </ol>
      <div className="flex flex-col gap-2 sm:flex-row sm:items-end">
        <div className="flex-1">
          <Field label={t('games.steampc.address')}>
            <input className={inputClass} dir="ltr" value={address} onChange={(e) => setAddress(e.target.value)} placeholder="192.168.1.30" />
          </Field>
        </div>
        <Button type="submit" variant="primary" disabled={!address.trim() || start.isPending}>
          {start.isPending ? t('games.steampc.looking') : t('games.steampc.pair')}
        </Button>
      </div>
      {start.error && <p className="text-sm text-danger">{errorText(start.error, t('somethingWentWrong'))}</p>}
      {p?.state === 'failed' && !start.isPending && <p className="text-sm text-danger">{p.error}</p>}
    </form>
  )
}
