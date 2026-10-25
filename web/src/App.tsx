import { useQuery } from '@tanstack/react-query'
import { lazy, Suspense } from 'react'
import { Navigate, Route, Routes } from 'react-router-dom'
import { api, ApiError } from './api'
import Shell from './components/Shell'
import { AudioProvider } from './player/AudioPlayer'
import Home from './pages/Home'
import Library from './pages/Library'
import ItemPage from './pages/Item'
import Search from './pages/Search'
import WatchList from './pages/WatchList'
import { Login, Setup } from './pages/Auth'
import { Spinner } from './components/ui'

const Watch = lazy(() => import('./pages/Watch'))
const Settings = lazy(() => import('./pages/Settings'))
const Link = lazy(() => import('./pages/Link'))

export default function App() {
  const me = useQuery({ queryKey: ['me'], queryFn: api.me, staleTime: Infinity })
  const unauthenticated = me.error instanceof ApiError && me.error.status === 401
  const setup = useQuery({ queryKey: ['setup'], queryFn: api.setup, enabled: unauthenticated })

  if (me.isPending || (unauthenticated && setup.isPending)) {
    return (
      <div className="grid h-dvh place-items-center">
        <Spinner />
      </div>
    )
  }
  if (unauthenticated) return setup.data?.needsSetup ? <Setup /> : <Login />
  if (me.isError) {
    return <div className="grid h-dvh place-items-center text-muted">Can't reach the server. Retrying…</div>
  }

  return (
    <AudioProvider>
      <Suspense fallback={null}>
      <Routes>
        <Route path="/watch/:id" element={<Watch />} />
        <Route element={<Shell user={me.data} />}>
          <Route index element={<Home />} />
          <Route path="/library/:id" element={<Library />} />
          <Route path="/item/:id" element={<ItemPage />} />
          <Route path="/search" element={<Search />} />
          <Route path="/watchlist" element={<WatchList />} />
          <Route path="/settings" element={<Settings />} />
          {/* At home people can watch without signing in; these need a real account. */}
          <Route path="/link" element={me.data.guest ? <Login /> : <Link user={me.data} />} />
          <Route path="/sign-in" element={me.data.guest ? <Login /> : <Navigate to="/" replace />} />
        </Route>
      </Routes>
      </Suspense>
    </AudioProvider>
  )
}
