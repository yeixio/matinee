import { useQueryClient } from '@tanstack/react-query'
import { useState, type FormEvent } from 'react'
import { api } from '../api'
import { Logo } from '../components/Shell'
import { Button, Field, inputClass } from '../components/ui'

function AuthCard({ title, subtitle, children }: { title: string; subtitle: string; children: React.ReactNode }) {
  return (
    <div className="relative grid min-h-dvh place-items-center overflow-hidden px-4">
      <div className="pointer-events-none absolute -top-40 left-1/2 h-[480px] w-[760px] -translate-x-1/2 rounded-full bg-accent/10 blur-3xl" />
      <div className="relative w-full max-w-sm animate-fade">
        <Logo className="mb-10 justify-center text-2xl" />
        <div className="rounded-2xl border border-line/70 bg-surface/80 p-6 shadow-2xl shadow-black/40 backdrop-blur md:p-8">
          <h1 className="text-xl font-semibold">{title}</h1>
          <p className="mt-1 mb-6 text-sm text-muted">{subtitle}</p>
          {children}
        </div>
      </div>
    </div>
  )
}

function useCredentialsForm(submit: (u: string, p: string) => Promise<unknown>) {
  const qc = useQueryClient()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const onSubmit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      await submit(username, password)
      await qc.invalidateQueries()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Something went wrong')
    } finally {
      setBusy(false)
    }
  }
  return { username, setUsername, password, setPassword, error, busy, onSubmit }
}

function CredentialsForm({ form, cta, newPassword }: { form: ReturnType<typeof useCredentialsForm>; cta: string; newPassword?: boolean }) {
  return (
    <form onSubmit={form.onSubmit} className="space-y-4">
      <Field label="Username">
        <input className={inputClass} value={form.username} onChange={(e) => form.setUsername(e.target.value)} autoComplete="username" autoFocus required />
      </Field>
      <Field label="Password">
        <input
          className={inputClass}
          type="password"
          value={form.password}
          onChange={(e) => form.setPassword(e.target.value)}
          autoComplete={newPassword ? 'new-password' : 'current-password'}
          minLength={newPassword ? 8 : undefined}
          required
        />
      </Field>
      {form.error && <p className="text-sm text-red-400">{form.error}</p>}
      <Button variant="primary" className="w-full" disabled={form.busy}>
        {form.busy ? 'One moment…' : cta}
      </Button>
    </form>
  )
}

export function Login() {
  const form = useCredentialsForm(api.login)
  return (
    <AuthCard title="Sign in" subtitle="Welcome back.">
      <CredentialsForm form={form} cta="Sign in" />
    </AuthCard>
  )
}

export function Setup() {
  const form = useCredentialsForm(api.createAdmin)
  return (
    <AuthCard title="Set up Matinee" subtitle="Create the admin account. You can add family and friends later.">
      <CredentialsForm form={form} cta="Create account" newPassword />
    </AuthCard>
  )
}
