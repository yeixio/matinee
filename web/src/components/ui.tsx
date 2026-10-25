import type { ButtonHTMLAttributes, ReactNode } from 'react'

export function Spinner({ className = '' }: { className?: string }) {
  return (
    <div
      role="status"
      aria-label="Loading"
      className={`size-8 animate-spin rounded-full border-2 border-line border-t-accent ${className}`}
    />
  )
}

type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: 'primary' | 'secondary' | 'ghost' | 'danger'
  icon?: ReactNode
}

export function Button({ variant = 'secondary', icon, children, className = '', ...rest }: ButtonProps) {
  const styles = {
    primary: 'bg-accent text-ink hover:bg-accent-strong font-semibold',
    secondary: 'bg-white/10 text-text hover:bg-white/15',
    ghost: 'text-muted hover:text-text hover:bg-white/5',
    danger: 'bg-red-500/15 text-red-300 hover:bg-red-500/25',
  }[variant]
  return (
    <button
      className={`inline-flex h-10 items-center justify-center gap-2 rounded-lg px-4 text-sm transition-colors disabled:opacity-50 ${styles} ${className}`}
      {...rest}
    >
      {icon}
      {children}
    </button>
  )
}

export function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className="block space-y-1.5">
      <span className="text-xs font-medium tracking-wide text-muted uppercase">{label}</span>
      {children}
    </label>
  )
}

export const inputClass =
  'h-11 w-full rounded-lg border border-line bg-surface px-3 text-sm text-text placeholder:text-faint focus:border-accent focus:outline-none'

export function EmptyState({ title, children }: { title: string; children?: ReactNode }) {
  return (
    <div className="mx-auto max-w-md py-24 text-center">
      <h2 className="text-lg font-semibold">{title}</h2>
      {children && <div className="mt-2 text-sm text-muted">{children}</div>}
    </div>
  )
}
