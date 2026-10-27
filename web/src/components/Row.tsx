import { ArrowRight, ChevronLeft, ChevronRight } from 'lucide-react'
import { useRef, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

// A horizontal shelf that scrolls by touch, trackpad, or the arrow buttons.
// With `more`, the title links to a page with everything in the row, and
// with `hasMore` too (the row has more titles than it shows) a See more
// tile ends the row; `wide` shapes it like the row's wide cards.
export function Row({
  title,
  more,
  hasMore = false,
  wide = false,
  children,
}: {
  title: string
  more?: string
  hasMore?: boolean
  wide?: boolean
  children: ReactNode
}) {
  const { t } = useTranslation()
  const ref = useRef<HTMLDivElement>(null)
  // dir is 1 for later titles, which are to the left in right-to-left languages.
  const scroll = (dir: number) => {
    const rtl = document.documentElement.dir === 'rtl'
    ref.current?.scrollBy({ left: (rtl ? -dir : dir) * ref.current.clientWidth * 0.85, behavior: 'smooth' })
  }
  return (
    <section className="group/row relative">
      <div className="mb-3 flex items-center justify-between">
        {more ? (
          <Link to={more} className="group/title flex items-center gap-1 text-lg font-semibold tracking-tight hover:text-accent-strong">
            {title}
            <ChevronRight className="size-5 text-muted transition-transform group-hover/title:translate-x-0.5 rtl:-scale-x-100 rtl:group-hover/title:-translate-x-0.5" />
          </Link>
        ) : (
          <h2 className="text-lg font-semibold tracking-tight">{title}</h2>
        )}
        <div className="hidden gap-1 opacity-0 transition-opacity group-hover/row:opacity-100 md:flex">
          <button onClick={() => scroll(-1)} className="rounded-md p-1.5 text-muted hover:bg-text/10 hover:text-text" aria-label={t('row.back')}>
            <ChevronLeft className="size-5 rtl:-scale-x-100" />
          </button>
          <button onClick={() => scroll(1)} className="rounded-md p-1.5 text-muted hover:bg-text/10 hover:text-text" aria-label={t('row.forward')}>
            <ChevronRight className="size-5 rtl:-scale-x-100" />
          </button>
        </div>
      </div>
      <div ref={ref} className="scrollbar-none -mx-4 flex snap-x scroll-px-4 gap-4 overflow-x-auto px-4 pb-2 md:-mx-8 md:scroll-px-8 md:px-8">
        {children}
        {more && hasMore && (
          <RowItem wide={wide}>
            <SeeMoreTile to={more} title={title} wide={wide} />
          </RowItem>
        )}
      </div>
    </section>
  )
}

// The tile at the end of a row with more titles than it shows, shaped like
// the row's cards, opening the row's own page.
export function SeeMoreTile({ to, title, wide = false }: { to: string; title: string; wide?: boolean }) {
  const { t } = useTranslation()
  return (
    <Link
      to={to}
      aria-label={t('common:rows.seeMoreOf', { title })}
      className={`group grid ${wide ? 'aspect-video' : 'aspect-[2/3]'} animate-fade place-items-center rounded-xl bg-surface ring-1 ring-text/10 transition duration-200 hover:bg-raised hover:ring-accent/70 focus-visible:ring-accent`}
    >
      <span className="flex flex-col items-center gap-3 px-3 text-center">
        <span className="grid size-12 place-items-center rounded-full bg-accent/15 text-accent transition group-hover:bg-accent group-hover:text-on-accent">
          <ArrowRight className="size-5 transition-transform group-hover:translate-x-0.5 rtl:-scale-x-100 rtl:group-hover:-translate-x-0.5" />
        </span>
        <span className="text-sm font-medium">{t('common:rows.seeMore')}</span>
      </span>
    </Link>
  )
}

export function RowItem({ wide = false, children }: { wide?: boolean; children: ReactNode }) {
  return <div className={`shrink-0 snap-start ${wide ? 'w-64 md:w-80' : 'w-32 md:w-40'}`}>{children}</div>
}
