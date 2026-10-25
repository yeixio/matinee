import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Clapperboard, Home, LogIn, LogOut, Music, Search, Settings, Tv } from 'lucide-react'
import { NavLink, Outlet } from 'react-router-dom'
import { api, type Library, type User } from '../api'
import { useAudio } from '../player/AudioPlayer'

export function LibraryIcon({ kind, className = 'size-5' }: { kind: string; className?: string }) {
  if (kind === 'tv') return <Tv className={className} />
  if (kind === 'music') return <Music className={className} />
  return <Clapperboard className={className} />
}

const ticket = 'M12 14H52a6 6 0 0 1 6 6v6a6 6 0 0 0 0 12v6a6 6 0 0 1-6 6H12a6 6 0 0 1-6-6v-6a6 6 0 0 0 0-12v-6a6 6 0 0 1 6-6Z'

// The mark is a ticket stub: a matinee is the afternoon showing.
export function Logo({ className = '' }: { className?: string }) {
  return (
    <span className={`flex items-center gap-2 font-display text-xl font-semibold tracking-tight ${className}`}>
      <svg viewBox="0 0 64 64" className="size-8" aria-hidden>
        <g transform="rotate(-10 32 32)">
          <path d={ticket} className="fill-accent" />
          <path d="M44 19v26" className="stroke-ink" strokeWidth="2.5" strokeDasharray="3 3.5" />
          <path d="M21 24.5v15l12-7.5Z" className="fill-ink" />
        </g>
      </svg>
      matinee
    </span>
  )
}

const topLink = ({ isActive }: { isActive: boolean }) =>
  `flex items-center gap-2 rounded-full px-4 py-1.5 text-sm transition-colors ${
    isActive ? 'bg-text text-ink font-medium' : 'text-muted hover:bg-white/5 hover:text-text'
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
  // Start loading a library on hover so the switch feels instant.
  const prefetchLibrary = (id: number) =>
    qc.prefetchQuery({ queryKey: ['library', id], queryFn: () => api.libraryItems(id), staleTime: 30_000 })
  const audio = useAudio()
  const logout = async () => {
    await api.logout()
    qc.clear()
    window.location.href = '/'
  }

  return (
    <div className="min-h-dvh">
      <header className="sticky top-0 z-20 border-b border-line/60 bg-ink/80 backdrop-blur">
        <div className="flex h-14 items-center gap-6 px-4 md:h-16 md:px-8">
          <NavLink to="/">
            <Logo />
          </NavLink>
          {/* Desktop: everything in one bar, libraries as pills */}
          <nav className="scrollbar-none hidden min-w-0 flex-1 items-center gap-1 overflow-x-auto md:flex">
            <NavLink to="/" end className={topLink}>
              Home
            </NavLink>
            {libraries.map((l) => (
              <NavLink key={l.id} to={`/library/${l.id}`} className={topLink} onMouseEnter={() => prefetchLibrary(l.id)} onFocus={() => prefetchLibrary(l.id)}>
                <span className="truncate">{l.name}</span>
                {l.scanning && <span className="size-1.5 animate-pulse rounded-full bg-accent" title="Scanning" />}
              </NavLink>
            ))}
          </nav>
          <div className="ml-auto flex items-center gap-1">
            <NavLink to="/search" className="hidden rounded-full p-2 text-muted hover:bg-white/5 hover:text-text md:block" aria-label="Search">
              <Search className="size-5" />
            </NavLink>
            {user.guest ? (
              // Watching at home without an account; signing in brings your own progress and settings.
              <NavLink to="/sign-in" className="flex items-center gap-2 rounded-full px-3 py-1.5 text-sm text-muted hover:bg-white/5 hover:text-text">
                <LogIn className="size-4" /> Sign in
              </NavLink>
            ) : (
              <NavLink to="/settings" className="rounded-full p-2 text-muted hover:bg-white/5 hover:text-text" aria-label="Settings">
                <Settings className="size-5" />
              </NavLink>
            )}
            {!user.guest && (
              <button
                onClick={logout}
                className="hidden items-center gap-2 rounded-full py-1 pr-3 pl-1 text-sm text-muted hover:bg-white/5 hover:text-text md:flex"
                title="Sign out"
              >
                <span className="grid size-7 place-items-center rounded-full bg-raised text-xs font-semibold text-text uppercase">{user.username[0]}</span>
                <LogOut className="size-4" />
              </button>
            )}
          </div>
        </div>
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
          <NavLink key={l.id} to={`/library/${l.id}`} className={tabLink} onTouchStart={() => prefetchLibrary(l.id)}>
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
