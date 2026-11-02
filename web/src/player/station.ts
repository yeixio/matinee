import { useQuery } from '@tanstack/react-query'
import { api, type Item } from '../api'

// Radio stations play live from the server, which relays them (see
// internal/radio): no seeking, no length, and no queue to move through.
export const isStation = (item?: Item) => item?.kind === 'station'

// What a station is playing, as it said last ("Artist - Song"), checked
// every little while it plays. Stations that don't say leave it empty.
export function useStationNow(item: Item | undefined, playing: boolean) {
  const station = isStation(item) ? item : undefined
  const now = useQuery({
    queryKey: ['stationNow', station?.id],
    queryFn: () => api.stationNow(station!.id),
    enabled: !!station,
    // The first title comes a few seconds after the stream starts.
    refetchInterval: (q) => (!playing ? false : q.state.data?.title ? 15_000 : 4_000),
    retry: false,
  })
  return station ? now.data?.title || undefined : undefined
}
