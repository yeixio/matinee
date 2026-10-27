import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Lock, Pencil, Plus, Trash2 } from 'lucide-react'
import { useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { api, type LibraryMode, type Profile, type ProfileChanges, type User } from '../api'
import { Avatar } from '../pages/Profiles'
import { avatarColor, kidsLevelName, kidsLevels } from '../profiles'
import { LibraryIcon, useLibraries } from './Shell'
import { Button, Field, inputClass } from './ui'

// Profiles is the admin's Settings section for everyone's profiles, the
// home network's shared account first.
export function Profiles() {
  const { t } = useTranslation()
  const { data } = useQuery({ queryKey: ['manageProfiles'], queryFn: api.manageProfiles })
  return (
    <section className="space-y-4">
      <div>
        <h2 className="text-lg font-semibold">{t('profileSettings.title')}</h2>
        <p className="mt-1 text-sm text-muted">
          {t('profileSettings.intro')}
        </p>
      </div>
      {data?.accounts.map(({ account, profiles }) => (
        <AccountProfiles key={account.id} account={account} profiles={profiles} avatars={data.avatars} />
      ))}
    </section>
  )
}

function AccountProfiles({ account, profiles, avatars }: { account: User; profiles: Profile[]; avatars: string[] }) {
  const { t } = useTranslation()
  const [editing, setEditing] = useState<Profile | 'new' | null>(null)
  const qc = useQueryClient()
  const done = () => {
    setEditing(null)
    qc.invalidateQueries({ queryKey: ['manageProfiles'] })
    qc.invalidateQueries({ queryKey: ['profiles'] })
  }
  const remove = useMutation({ mutationFn: api.deleteProfile, onSuccess: done })
  const accountPin = useMutation({
    mutationFn: (pin: string) => api.updateProfile(account.id, { pin }),
    onSuccess: done,
  })
  const setAccountPin = () => {
    if (account.hasPin) {
      accountPin.mutate('')
      return
    }
    const pin = prompt(t('profileSettings.pinFor', { name: account.username }))
    if (pin) accountPin.mutate(pin)
  }

  return (
    <div className="rounded-xl border border-line/60 bg-surface/50">
      <div className="flex items-center gap-3 p-4">
        <Avatar name={account.username} avatar={account.avatar} />
        <div className="min-w-0 flex-1">
          <div className="font-medium">{account.username}</div>
          <div className="text-xs text-muted">{account.guest ? t('profileSettings.homeDevices') : t('profileSettings.account')}</div>
        </div>
        {profiles.length > 0 && (
          <Button variant="ghost" icon={<Lock className="size-4" />} onClick={setAccountPin} disabled={accountPin.isPending}>
            <span className="hidden sm:inline">{account.hasPin ? t('profileSettings.removePin') : t('profileSettings.addPin')}</span>
          </Button>
        )}
      </div>
      {accountPin.error && <p className="px-4 pb-3 text-sm text-danger">{accountPin.error.message}</p>}
      <div className="divide-y divide-line/60 border-t border-line/60">
        {profiles.map((p) =>
          editing !== 'new' && editing?.id === p.id ? (
            <ProfileForm key={p.id} accountId={account.id} profile={p} avatars={avatars} onDone={done} onCancel={() => setEditing(null)} />
          ) : (
            <div key={p.id} className="flex items-center gap-3 py-3 ps-8 pe-4">
              <Avatar name={p.username} avatar={p.avatar} className="size-8 text-xs" />
              <span className="min-w-0 flex-1 truncate">{p.username}</span>
              {p.hasPin && <Lock className="size-3.5 text-muted" aria-label={t('profileSettings.hasPin')} />}
              {!!p.kidsLevel && <span className="rounded bg-accent/15 px-2 py-0.5 text-xs text-accent">{kidsLevelName(p.kidsLevel)}</span>}
              <Button variant="ghost" icon={<Pencil className="size-4" />} aria-label={t('profileSettings.edit', { name: p.username })} onClick={() => setEditing(p)} />
              <Button
                variant="ghost"
                icon={<Trash2 className="size-4" />}
                aria-label={t('profileSettings.remove', { name: p.username })}
                onClick={() => confirm(t('profileSettings.confirmRemove', { name: p.username })) && remove.mutate(p.id)}
              />
            </div>
          ),
        )}
        {editing === 'new' ? (
          <ProfileForm accountId={account.id} avatars={avatars} onDone={done} onCancel={() => setEditing(null)} />
        ) : (
          <div className="p-3 ps-7">
            <Button variant="ghost" icon={<Plus className="size-4" />} onClick={() => setEditing('new')}>
              {t('profileSettings.add')}
            </Button>
          </div>
        )}
      </div>
    </div>
  )
}

const modes: { value: LibraryMode | ''; key: string }[] = [
  { value: '', key: 'hidden' },
  { value: 'rated', key: 'rated' },
  { value: 'all', key: 'all' },
]

function ProfileForm({
  accountId,
  profile,
  avatars,
  onDone,
  onCancel,
}: {
  accountId: number
  profile?: Profile
  avatars: string[]
  onDone: () => void
  onCancel: () => void
}) {
  const { t } = useTranslation()
  const { data: libraries = [] } = useLibraries()
  const [name, setName] = useState(profile?.username ?? '')
  const [avatar, setAvatar] = useState(profile?.avatar ?? '')
  const [kidsLevel, setKidsLevel] = useState(profile?.kidsLevel ?? 0)
  const [pin, setPin] = useState('')
  const [removePin, setRemovePin] = useState(false)
  // New kids profiles see movies and TV by rating.
  const [libs, setLibs] = useState<Record<string, LibraryMode>>(
    () =>
      profile?.libraries ??
      Object.fromEntries(libraries.filter((l) => l.kind === 'movies' || l.kind === 'tv').map((l) => [String(l.id), 'rated' as const])),
  )
  const save = useMutation({
    mutationFn: () => {
      const c: ProfileChanges = { name, kidsLevel, libraries: kidsLevel ? libs : undefined }
      if (avatar) c.avatar = avatar
      if (pin) c.pin = pin
      else if (removePin) c.pin = ''
      return profile ? api.updateProfile(profile.id, c) : api.createProfile({ ...c, accountId })
    },
    onSuccess: onDone,
  })
  const submit = (e: FormEvent) => {
    e.preventDefault()
    save.mutate()
  }

  return (
    <form onSubmit={submit} className="space-y-4 bg-text/[0.03] p-4 sm:ps-8">
      <div className="grid gap-3 sm:grid-cols-2">
        <Field label={t('profileSettings.name')}>
          <input className={inputClass} value={name} onChange={(e) => setName(e.target.value)} maxLength={30} autoFocus required />
        </Field>
        <Field label={profile?.hasPin ? t('profileSettings.newPin') : t('profileSettings.pinOptional')}>
          <input
            className={inputClass}
            value={pin}
            onChange={(e) => setPin(e.target.value.replace(/\D/g, '').slice(0, 4))}
            inputMode="numeric"
            pattern="\d{4}"
            placeholder={profile?.hasPin ? t('profileSettings.unchanged') : t('profileSettings.fourDigits')}
            autoComplete="off"
          />
        </Field>
      </div>
      {profile?.hasPin && !pin && (
        <label className="flex items-center gap-2 text-sm text-muted">
          <input type="checkbox" checked={removePin} onChange={(e) => setRemovePin(e.target.checked)} className="accent-accent" /> {t('profileSettings.removeThePin')}
        </label>
      )}
      <div className="flex flex-wrap items-center gap-2" role="radiogroup" aria-label={t('profileSettings.colour')}>
        {avatars.map((a) => (
          <button
            type="button"
            key={a}
            role="radio"
            aria-checked={avatar === a}
            aria-label={t(`common:avatars.${a}`, { defaultValue: a })}
            onClick={() => setAvatar(a)}
            className={`size-7 rounded-full ring-offset-2 ring-offset-surface ${avatar === a ? 'ring-2 ring-text' : ''}`}
            style={{ backgroundColor: avatarColor(a) }}
          />
        ))}
      </div>
      <Field label={t('profileSettings.kidsMode')}>
        <select className={inputClass} value={kidsLevel} onChange={(e) => setKidsLevel(Number(e.target.value))}>
          <option value={0}>{t('profileSettings.kidsOff')}</option>
          {kidsLevels.map((l) => (
            <option key={l.level} value={l.level}>
              {t('profileSettings.kidsLevel', { name: l.name, ratings: l.ratings })}
            </option>
          ))}
        </select>
      </Field>
      {kidsLevel > 0 && (
        <div className="space-y-2">
          <p className="text-sm text-muted">
            {t('profileSettings.kidsLibraries', { level: kidsLevelName(kidsLevel) })}
          </p>
          {libraries.map((l) => (
            <label key={l.id} className="flex items-center gap-3">
              <LibraryIcon kind={l.kind} className="size-4 text-muted" />
              <span className="min-w-0 flex-1 truncate text-sm">{l.name}</span>
              <select
                className={`${inputClass.replace('h-11 w-full', 'h-9 w-36 shrink-0')}`}
                value={libs[String(l.id)] ?? ''}
                onChange={(e) => {
                  const next = { ...libs }
                  if (e.target.value) next[String(l.id)] = e.target.value as LibraryMode
                  else delete next[String(l.id)]
                  setLibs(next)
                }}
              >
                {modes.map((m) => (
                  <option key={m.value} value={m.value}>
                    {t(`profileSettings.libraryMode.${m.key}`)}
                  </option>
                ))}
              </select>
            </label>
          ))}
        </div>
      )}
      {save.error && <p className="text-sm text-danger">{save.error.message}</p>}
      <div className="flex gap-2">
        <Button variant="primary" disabled={save.isPending}>
          {profile ? t('save') : t('profileSettings.add')}
        </Button>
        <Button type="button" variant="ghost" onClick={onCancel}>
          {t('cancel')}
        </Button>
      </div>
    </form>
  )
}
