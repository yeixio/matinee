import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Lock, LogIn } from 'lucide-react'
import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, ApiError, type ProfileChoice, type User } from '../api'
import { Logo } from '../components/Shell'
import { Button } from '../components/ui'
import { avatarColor, kidsLevelName, rememberAsked } from '../profiles'

export function Avatar({ name, avatar, className = 'size-9 text-sm' }: { name: string; avatar?: string; className?: string }) {
  return (
    <span
      className={`grid shrink-0 place-items-center rounded-full font-semibold text-white uppercase ${className}`}
      style={{ backgroundColor: avatarColor(avatar) }}
      aria-hidden
    >
      {name.trim()[0] ?? '?'}
    </span>
  )
}

// WhoIsWatching is the profile picker: shown once after signing in when the
// account has profiles, and from the avatar menu to switch.
export function WhoIsWatching({ me, onDone }: { me: User; onDone?: () => void }) {
  const qc = useQueryClient()
  const { data } = useQuery({ queryKey: ['profiles'], queryFn: api.profiles })
  const [locked, setLocked] = useState<ProfileChoice | null>(null)
  const [error, setError] = useState('')

  const pick = async (p: ProfileChoice, pin?: string) => {
    if (p.hasPin && p.id !== me.id && pin === undefined) {
      setLocked(p)
      setError('')
      return
    }
    try {
      await api.selectProfile(p.id, pin)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Something went wrong')
      return
    }
    rememberAsked(me)
    setLocked(null)
    // Everything personal changes: progress, watch list, theme, what kids may see.
    await qc.resetQueries()
    onDone?.()
  }

  return (
    <div className="relative grid min-h-dvh place-items-center overflow-hidden px-4 py-12">
      <div className="pointer-events-none absolute -top-40 left-1/2 h-[480px] w-[760px] -translate-x-1/2 rounded-full bg-accent/10 blur-3xl" />
      <div className="relative w-full max-w-3xl animate-fade text-center">
        <Logo className="mb-10 justify-center text-2xl" />
        {locked ? (
          <PinEntry profile={locked} error={error} onSubmit={(pin) => pick(locked, pin)} onCancel={() => setLocked(null)} />
        ) : (
          <>
            <h1 className="font-display text-3xl font-semibold tracking-tight md:text-4xl">Who's watching?</h1>
            <ul className="mt-10 flex flex-wrap justify-center gap-6 md:gap-10">
              {data?.profiles.map((p) => (
                <li key={p.id}>
                  <button
                    onClick={() => pick(p)}
                    className="group flex w-28 flex-col items-center gap-3 rounded-2xl p-2 focus-visible:outline-2 focus-visible:outline-accent md:w-32"
                  >
                    <span className="relative">
                      <Avatar
                        name={p.name}
                        avatar={p.avatar}
                        className={`size-24 text-4xl ring-offset-4 ring-offset-ink transition group-hover:ring-2 group-hover:ring-text md:size-28 ${
                          p.id === data.current ? 'ring-2 ring-accent' : ''
                        }`}
                      />
                      {p.hasPin && (
                        <span className="absolute right-0 bottom-0 grid size-7 place-items-center rounded-full bg-raised text-muted">
                          <Lock className="size-3.5" />
                        </span>
                      )}
                    </span>
                    <span className="max-w-full truncate text-sm text-muted group-hover:text-text">{p.name}</span>
                    {!!p.kidsLevel && <span className="-mt-2 text-xs text-faint">{kidsLevelName(p.kidsLevel)}</span>}
                  </button>
                </li>
              ))}
            </ul>
            {error && <p className="mt-6 text-sm text-danger">{error}</p>}
            {me.guest && (
              // At home without signing in; an account brings its own profiles.
              <a href="/sign-in" className="mt-12 inline-flex items-center gap-2 text-sm text-muted hover:text-text">
                <LogIn className="size-4" /> Sign in with an account
              </a>
            )}
          </>
        )}
      </div>
    </div>
  )
}

function PinEntry({ profile, error, onSubmit, onCancel }: { profile: ProfileChoice; error: string; onSubmit: (pin: string) => void; onCancel: () => void }) {
  const [pin, setPin] = useState('')
  const type = (next: string) => {
    const digits = next.replace(/\D/g, '').slice(0, 4)
    setPin(digits)
    if (digits.length === 4) {
      onSubmit(digits)
      setPin('')
    }
  }
  return (
    <div className="mx-auto flex max-w-xs flex-col items-center">
      <Avatar name={profile.name} avatar={profile.avatar} className="size-20 text-3xl" />
      <h1 className="mt-4 text-xl font-semibold">Enter {profile.name}'s PIN</h1>
      <input
        autoFocus
        aria-label="PIN"
        type="password"
        inputMode="numeric"
        autoComplete="off"
        value={pin}
        onChange={(e) => type(e.target.value)}
        className="sr-only"
      />
      <div className="mt-6 flex gap-3" aria-hidden>
        {[0, 1, 2, 3].map((i) => (
          <span key={i} className={`size-4 rounded-full border-2 ${i < pin.length ? 'border-accent bg-accent' : 'border-line'}`} />
        ))}
      </div>
      <div className="mt-8 grid grid-cols-3 gap-3">
        {['1', '2', '3', '4', '5', '6', '7', '8', '9', '', '0', '⌫'].map((k, i) =>
          k === '' ? (
            <span key={i} />
          ) : (
            <button
              key={i}
              type="button"
              aria-label={k === '⌫' ? 'Delete' : k}
              onClick={() => type(k === '⌫' ? pin.slice(0, -1) : pin + k)}
              className="grid size-16 place-items-center rounded-full bg-text/5 text-xl font-medium hover:bg-text/10 focus-visible:outline-2 focus-visible:outline-accent"
            >
              {k}
            </button>
          ),
        )}
      </div>
      {error && <p className="mt-6 text-sm text-danger">{error}</p>}
      <Button variant="ghost" className="mt-6" onClick={onCancel}>
        Back
      </Button>
    </div>
  )
}

// SwitchProfile is the /profiles page, reached from the avatar in the header.
export function SwitchProfile({ me }: { me: User }) {
  const navigate = useNavigate()
  return <WhoIsWatching me={me} onDone={() => navigate('/')} />
}
