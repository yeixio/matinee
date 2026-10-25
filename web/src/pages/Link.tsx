import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { CheckCircle2, Smartphone, Tv, XCircle } from 'lucide-react'
import { useState, type FormEvent } from 'react'
import { useSearchParams } from 'react-router-dom'
import { api, type User } from '../api'
import { Button, inputClass } from '../components/ui'

// Codes are 8 letters and digits; show them as ABCD-EFGH however they were typed.
const clean = (code: string) => code.toUpperCase().replace(/[^A-Z0-9]/g, '').slice(0, 8)
const pretty = (code: string) => (code.length > 4 ? `${code.slice(0, 4)}-${code.slice(4)}` : code)

export function DeviceIcon({ platform, className = 'size-5' }: { platform: string; className?: string }) {
  return /tv/.test(platform) ? <Tv className={className} /> : <Smartphone className={className} />
}

// /link: sign in a phone or TV that is showing a code, the way plex.tv/link works.
export default function Link({ user }: { user: User }) {
  const [params] = useSearchParams()
  const [typed, setTyped] = useState(() => pretty(clean(params.get('code') ?? '')))
  const [code, setCode] = useState(() => (clean(params.get('code') ?? '').length === 8 ? clean(params.get('code')!) : ''))
  const [done, setDone] = useState<'approved' | 'declined' | null>(null)
  const qc = useQueryClient()

  const pending = useQuery({ queryKey: ['device-code', code], queryFn: () => api.pendingDevice(code), enabled: code !== '', retry: false })
  const decide = useMutation({
    mutationFn: (approve: boolean) => api.decideDevice(code, approve),
    onSuccess: (_, approve) => {
      setDone(approve ? 'approved' : 'declined')
      qc.invalidateQueries({ queryKey: ['devices'] })
    },
  })

  const submit = (e: FormEvent) => {
    e.preventDefault()
    setDone(null)
    decide.reset()
    setCode(clean(typed))
  }

  return (
    <div className="mx-auto max-w-md py-10 md:py-16">
      <h1 className="text-2xl font-bold tracking-tight">Sign in a device</h1>
      <p className="mt-2 text-sm text-muted">Enter the code your phone or TV is showing.</p>

      {done ? (
        <div className="mt-8 animate-fade rounded-2xl border border-line/60 bg-surface/60 p-6 text-center">
          {done === 'approved' ? (
            <>
              <CheckCircle2 className="mx-auto size-10 text-accent" />
              <p className="mt-3 font-semibold">{pending.data?.deviceName} is signed in</p>
              <p className="mt-1 text-sm text-muted">It will be ready in a few seconds.</p>
            </>
          ) : (
            <>
              <XCircle className="mx-auto size-10 text-faint" />
              <p className="mt-3 font-semibold">Declined</p>
              <p className="mt-1 text-sm text-muted">{pending.data?.deviceName} was not signed in.</p>
            </>
          )}
          <Button
            className="mt-6"
            onClick={() => {
              setDone(null)
              setTyped('')
              setCode('')
            }}
          >
            Sign in another device
          </Button>
        </div>
      ) : (
        <>
          <form onSubmit={submit} className="mt-8 flex gap-2">
            <input
              className={`${inputClass} h-14 text-center font-mono text-2xl tracking-[0.2em] uppercase`}
              value={typed}
              onChange={(e) => setTyped(pretty(clean(e.target.value)))}
              placeholder="ABCD-EFGH"
              aria-label="Code"
              autoComplete="one-time-code"
              autoCapitalize="characters"
              spellCheck={false}
              autoFocus
            />
            <Button variant="primary" className="h-14 px-5" disabled={clean(typed).length !== 8}>
              Next
            </Button>
          </form>

          {pending.isError && <p className="mt-4 text-sm text-red-400">{pending.error.message}</p>}

          {pending.data && (
            <div className="mt-6 animate-fade rounded-2xl border border-accent/30 bg-surface p-5">
              <div className="flex items-center gap-4">
                <span className="grid size-12 place-items-center rounded-xl bg-accent/10 text-accent">
                  <DeviceIcon platform={pending.data.platform} className="size-6" />
                </span>
                <div className="min-w-0">
                  <div className="truncate font-semibold">{pending.data.deviceName}</div>
                  <div className="text-sm text-muted">
                    wants to sign in as <span className="text-text">{user.username}</span>
                  </div>
                </div>
              </div>
              <p className="mt-4 text-xs text-faint">Only approve a device you're holding or can see. It gets the same access you have.</p>
              {decide.isError && <p className="mt-3 text-sm text-red-400">{decide.error.message}</p>}
              <div className="mt-5 flex justify-end gap-2">
                <Button variant="ghost" disabled={decide.isPending} onClick={() => decide.mutate(false)}>
                  Decline
                </Button>
                <Button variant="primary" disabled={decide.isPending} onClick={() => decide.mutate(true)}>
                  Sign in
                </Button>
              </div>
            </div>
          )}
        </>
      )}
    </div>
  )
}
