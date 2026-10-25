import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Clapperboard, Home, LogOut, Music, Search, Settings, Tv } from 'lucide-react'
import { NavLink, Outlet } from 'react-router-dom'
import { api, type Library, type User } from '../api'
import { useAudio } from '../player/AudioPlayer'

export function LibraryIcon({ kind, className = 'size-5' }: { kind: string; className?: string }) {
  if (kind === 'tv') return <Tv className={className} />
  if (kind === 'music') return <Music className={className} />
  return <Clapperboard className={className} />
}

export function Logo({ className = '' }: { className?: string }) {
  return (
    <span className={`flex items-center gap-2 text-lg font-bold tracking-tight ${className}`}>
      <svg viewBox="0 0 64 64" className="size-7" aria-hidden>
        <rect width="64" height="64" rx="14" fill="#1b1e26" />
        <path d="M18 46V18l14 16 14-16v28" fill="none" stroke="#f5b544" strokeWidth="6" strokeLinecap="round" strokeLinejoin="round" />
      </svg>
      Matinee
    </span>
  )
}

const sideLink = ({ isActive }: { isActive: boolean }) =>
  `flex items-center gap-3 rounded-lg px-3 py-2 text-sm transition-colors ${
    isActive ? 'bg-white/10 text-text font-medium' : 'text-muted hover:bg-white/5 hover:text-text'
  }`

const tabLink = ({ isActive }: { isActive: boolean }) =>
  `flex flex-1 flex-col items-center gap-1 py-2 text-[11px] ${isActive ? 'text-accent' : 'text-muted'}`

export function useLibraries() {
  return useQuery({
    queryKey: ['libraries'],
    queryFn: api.libraries,
    // Poll while a scan runs so new items show up on their own.
    refetchInterval: (q) => (q.state.data?.some((l: Library) => l.scanning) ? 2000 : false),
  })
}

export default function Shell({ user }: { user: User }) {
  const { data: libraries = [] } = useLibraries()
  const qc = useQueryClient()
  const audio = useAudio()
  const logout = async () => {
    await api.logout()
    qc.clear()
    window.location.href = '/'
  }

  return (
    <div className="min-h-dvh md:pl-60">
      {/* Desktop sidebar */}
      <aside className="fixed inset-y-0 left-0 hidden w-60 flex-col border-r border-line/60 bg-surface/60 px-3 py-5 backdrop-blur md:flex">
        <NavLink to="/" className="mb-6 px-3">
          <Logo />
        </NavLink>
        <nav className="space-y-1">
          <NavLink to="/" end className={sideLink}>
            <Home className="size-5" /> Home
          </NavLink>
          <NavLink to="/search" className={sideLink}>
            <Search className="size-5" /> Search
          </NavLink>
        </nav>
        {libraries.length > 0 && (
          <>
            <div className="mt-6 mb-2 px-3 text-xs font-medium tracking-wide text-faint uppercase">Libraries</div>
            <nav className="space-y-1">
              {libraries.map((l) => (
                <NavLink key={l.id} to={`/library/${l.id}`} className={sideLink}>
                  <LibraryIcon kind={l.kind} /> <span className="truncate">{l.name}</span>
                  {l.scanning && <span className="ml-auto size-1.5 animate-pulse rounded-full bg-accent" title="Scanning" />}
                </NavLink>
              ))}
            </nav>
          </>
        )}
        <div className="mt-auto space-y-1">
          {user.isAdmin && (
            <NavLink to="/settings" className={sideLink}>
              <Settings className="size-5" /> Settings
            </NavLink>
          )}
          <button onClick={logout} className={sideLink({ isActive: false }) + ' w-full'}>
            <LogOut className="size-5" /> Sign out <span className="ml-auto truncate text-xs text-faint">{user.username}</span>
          </button>
        </div>
      </aside>

      {/* Mobile top bar */}
      <header className="sticky top-0 z-20 flex h-14 items-center justify-between border-b border-line/60 bg-ink/80 px-4 backdrop-blur md:hidden">
        <NavLink to="/">
          <Logo />
        </NavLink>
        {user.isAdmin && (
          <NavLink to="/settings" className="rounded-lg p-2 text-muted" aria-label="Settings">
            <Settings className="size-5" />
          </NavLink>
        )}
      </header>

      <main className={`px-4 pt-4 md:px-8 md:pt-8 ${audio.current ? 'pb-44 md:pb-28' : 'pb-24 md:pb-12'}`}>
        <Outlet />
      </main>

      {/* Mobile tab bar */}
      <nav
        className="fixed inset-x-0 bottom-0 z-30 flex border-t border-line/60 bg-ink/90 backdrop-blur md:hidden"
        style={{ paddingBottom: 'env(safe-area-inset-bottom)' }}
      >
        <NavLink to="/" end className={tabLink}>
          <Home className="size-5" /> Home
        </NavLink>
        {libraries.slice(0, 3).map((l) => (
          <NavLink key={l.id} to={`/library/${l.id}`} className={tabLink}>
            <LibraryIcon kind={l.kind} /> <span className="max-w-16 truncate">{l.name}</span>
          </NavLink>
        ))}
        <NavLink to="/search" className={tabLink}>
          <Search className="size-5" /> Search
        </NavLink>
      </nav>
    </div>
  )
}
