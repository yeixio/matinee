import { useQuery } from '@tanstack/react-query'
import { Tv } from 'lucide-react'
import { useState } from 'react'
import { api, type LiveChannel, type User } from '../api'

// A live TV channel's logo, or a TV when it has none.
export function ChannelLogo({ channel, className = 'size-12' }: { channel: LiveChannel; className?: string }) {
  const [failed, setFailed] = useState(false)
  return (
    <div className={`grid shrink-0 place-items-center overflow-hidden rounded-lg bg-text/5 ${className}`}>
      {channel.logo && !failed ? (
        <img src={channel.logo} alt="" loading="lazy" referrerPolicy="no-referrer" className="size-full object-contain p-1" onError={() => setFailed(true)} />
      ) : (
        <Tv className="size-1/2 text-muted" />
      )}
    </div>
  )
}

/** Whether this person has live TV channels to watch: grown-ups at home only. */
export function useHasLiveTV(user?: User) {
  const on = !!user && !user.kidsLevel && !user.friend
  const { data } = useQuery({ queryKey: ['live', 'channels'], queryFn: api.liveChannels, enabled: on, staleTime: 5 * 60_000, retry: false })
  return on && !!data?.channels.length
}
