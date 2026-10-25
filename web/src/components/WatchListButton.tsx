import { useMutation, useQueryClient } from '@tanstack/react-query'
import { BookmarkCheck, BookmarkPlus } from 'lucide-react'
import { api, canWatchList, type ItemDetail } from '../api'
import { Button } from './ui'

// Adds a movie or show to the watch list, or takes it off. The button flips
// right away and flips back if the server says no.
export function WatchListButton({ item }: { item: ItemDetail }) {
  const qc = useQueryClient()
  const key = ['item', item.id]
  const toggle = useMutation({
    mutationFn: (add: boolean) => (add ? api.addToWatchList(item.id) : api.removeFromWatchList(item.id)),
    onMutate: async (add) => {
      await qc.cancelQueries({ queryKey: key })
      const before = qc.getQueryData<ItemDetail>(key)
      if (before) qc.setQueryData<ItemDetail>(key, { ...before, onWatchList: add })
      return { before }
    },
    onError: (_err, _add, ctx) => {
      if (ctx?.before) qc.setQueryData(key, ctx.before)
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: key })
      qc.invalidateQueries({ queryKey: ['home'] })
      qc.invalidateQueries({ queryKey: ['watchlist'] })
    },
  })

  if (!canWatchList(item)) return null
  const on = !!item.onWatchList
  return (
    <Button
      onClick={() => toggle.mutate(!on)}
      aria-pressed={on}
      icon={on ? <BookmarkCheck className="size-4 text-accent" /> : <BookmarkPlus className="size-4" />}
    >
      {on ? 'On your watch list' : 'Add to watch list'}
    </Button>
  )
}
