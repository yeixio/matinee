import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Star } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, type Item } from '../api'

// The person's own rating, from half a star to five stars (1 to 10). Each
// star's left half gives a half star. Picking the rating it already has
// takes it back.
export function StarRating({ item }: { item: Item }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [rating, setRating] = useState(item.myRating ?? 0)
  const [hover, setHover] = useState(0)
  useEffect(() => setRating(item.myRating ?? 0), [item.myRating])
  const rate = useMutation({
    mutationFn: (r: number) => api.rate(item.id, r || null),
    onMutate: (r) => {
      const before = rating
      setRating(r)
      return { before }
    },
    onError: (_err, _r, ctx) => ctx && setRating(ctx.before),
    onSettled: () => qc.invalidateQueries({ queryKey: ['item', item.id] }),
  })
  const shown = hover || rating
  return (
    <div
      className="flex items-center gap-0.5"
      role="group"
      aria-label={rating ? t('rating.yours', { stars: rating / 2 }) : t('rating.rate')}
      onMouseLeave={() => setHover(0)}
    >
      {[1, 2, 3, 4, 5].map((n) => {
        const fill = shown >= n * 2 ? 1 : shown === n * 2 - 1 ? 0.5 : 0
        return (
          <span key={n} className="relative size-7">
            <Star className="absolute inset-0 size-7 text-faint" strokeWidth={1.5} />
            {fill > 0 && (
              <span className="absolute inset-0 overflow-hidden" style={{ width: `${fill * 100}%` }}>
                <Star className="size-7 fill-accent text-accent" strokeWidth={1.5} />
              </span>
            )}
            {[n * 2 - 1, n * 2].map((value, half) => (
              <button
                key={value}
                type="button"
                className={`absolute inset-y-0 w-1/2 ${half ? 'end-0' : 'start-0'} rounded-sm focus-visible:outline-2 focus-visible:outline-accent`}
                aria-label={t('rating.stars', { stars: value / 2 })}
                aria-pressed={rating === value}
                onMouseEnter={() => setHover(value)}
                onFocus={() => setHover(value)}
                onBlur={() => setHover(0)}
                onClick={() => rate.mutate(rating === value ? 0 : value)}
              />
            ))}
          </span>
        )
      })}
    </div>
  )
}
