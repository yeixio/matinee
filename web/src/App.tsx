import { useQuery } from '@tanstack/react-query'
import { lazy, Suspense, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Navigate, Route, Routes, useLocation, useNavigate } from 'react-router-dom'
import { api, ApiError } from './api'
import Shell from './components/Shell'
import { AudioProvider } from './player/AudioPlayer'
import Home from './pages/Home'
import Library from './pages/Library'
import Libraries from './pages/Libraries'
import ItemPage from './pages/Item'
import PersonPage from './pages/Person'
import Search from './pages/Search'
import WatchList from './pages/WatchList'
import PlaylistPage, { PlaylistsPage } from './pages/Playlist'
import Collections from './pages/Collections'
import CollectionPage from './pages/Collection'
import BoxSetPage from './pages/BoxSet'
import RowPage from './pages/RowPage'
import { Login, Setup } from './pages/Auth'
import { SwitchProfile, WhoIsWatching } from './pages/Profiles'
import { askedFor, isKids, rememberAsked, shouldAsk } from './profiles'
import { Spinner } from './components/ui'
import { ThemeSync } from './theme'
import { LanguageSync } from './i18n/sync'
import { remote } from './remote'

const Watch = lazy(() => import('./pages/Watch'))
const Settings = lazy(() => import('./pages/Settings'))
const Link = lazy(() => import('./pages/Link'))
const Requests = lazy(() => import('./pages/Requests'))
const Upcoming = lazy(() => import('./pages/Upcoming'))
const Discover = lazy(() => import('./pages/Discover'))
const DiscoverRowPage = lazy(() => import('./pages/Discover').then((m) => ({ default: m.DiscoverRowPage })))

export default function App() {
  const me = useQuery({ queryKey: ['me'], queryFn: api.me, staleTime: Infinity })
  return (
    <>
      <ThemeSync userId={me.data?.id} />
      <LanguageSync userId={me.data?.id} />
      <Screens />
    </>
  )
}

function Screens() {
  const { t } = useTranslation()
  const me = useQuery({ queryKey: ['me'], queryFn: api.me, staleTime: Infinity })
  const unauthenticated = me.error instanceof ApiError && me.error.status === 401
  const setup = useQuery({ queryKey: ['setup'], queryFn: api.setup, enabled: unauthenticated })
  const profiles = useQuery({ queryKey: ['profiles'], queryFn: api.profiles, enabled: me.isSuccess, retry: false })
  const [asked, setAsked] = useState(askedFor)
  const { pathname } = useLocation()
  // Signed in without profiles: profiles added later wait for the next sign-in
  // to ask, rather than pulling the admin who made them into the picker.
  const single = profiles.data && profiles.data.profiles.length <= 1
  useEffect(() => {
    if (single && me.data) {
      rememberAsked(me.data)
      setAsked(askedFor())
    }
  }, [single, me.data])

  if (me.isPending || (unauthenticated && setup.isPending) || (me.isSuccess && profiles.isPending)) {
    return (
      <div className="grid h-dvh place-items-center">
        <Spinner />
      </div>
    )
  }
  if (unauthenticated) return setup.data?.needsSetup ? <Setup /> : <Login />
  if (me.isError) {
    return <div className="grid h-dvh place-items-center text-muted">{t('cantReachServer')}</div>
  }
  if (pathname !== '/sign-in' && shouldAsk(me.data, profiles.data?.profiles, asked)) {
    return <WhoIsWatching me={me.data} onDone={() => setAsked(askedFor())} />
  }
  // Kids profiles can't open settings, make requests or link devices.
  const kids = isKids(me.data)
  const grownUps = (page: React.ReactNode) => (kids ? <Navigate to="/" replace /> : page)

  return (
    <AudioProvider user={me.data.id}>
      <RemoteSync />
      <Suspense fallback={null}>
      <Routes>
        <Route path="/watch/:id" element={<Watch />} />
        <Route path="/profiles" element={<SwitchProfile me={me.data} />} />
        <Route element={<Shell user={me.data} />}>
          <Route index element={<Home />} />
          <Route path="/libraries" element={<Libraries />} />
          <Route path="/library/:id" element={<Library />} />
          <Route path="/item/:id" element={<ItemPage />} />
          <Route path="/person/:id" element={<PersonPage />} />
          <Route path="/search" element={<Search />} />
          <Route path="/watchlist" element={<WatchList />} />
          <Route path="/playlists" element={<PlaylistsPage />} />
          <Route path="/playlist/:id" element={<PlaylistPage />} />
          <Route path="/collections" element={<Collections />} />
          <Route path="/collection/:id" element={<CollectionPage />} />
          <Route path="/box-set/:id" element={<BoxSetPage />} />
          <Route path="/row/:id" element={<RowPage />} />
          <Route path="/settings" element={grownUps(<Settings />)} />
          <Route path="/upcoming" element={grownUps(<Upcoming />)} />
          <Route path="/discover" element={grownUps(<Discover />)} />
          <Route path="/discover/row/:id" element={grownUps(<DiscoverRowPage />)} />
          <Route path="/requests" element={grownUps(me.data.guest ? <Login /> : <Requests />)} />
          {/* At home people can watch without signing in; these need a real account. */}
          <Route path="/link" element={grownUps(me.data.guest ? <Login /> : <Link user={me.data} />)} />
          <Route path="/sign-in" element={me.data.guest ? <Login /> : <Navigate to="/" replace />} />
        </Route>
      </Routes>
      </Suspense>
    </AudioProvider>
  )
}

// Lets Home Assistant and people's other devices see and control this
// browser (see remote.ts).
function RemoteSync() {
  const navigate = useNavigate()
  useEffect(() => remote.start((path) => navigate(path)), [navigate])
  return null
}
