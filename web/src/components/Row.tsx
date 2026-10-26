import { ChevronLeft, ChevronRight } from 'lucide-react'
import { useRef, type ReactNode } from 'react'
import { Link } from 'react-router-dom'

// A horizontal shelf that scrolls by touch, trackpad, or the arrow buttons.
// With `more`, the title links to a page with everything in the row.
export function Row({ title, more, children }: { title: string; more?: string; children: ReactNode }) {
  const ref = useRef<HTMLDivElement>(null)
  const scroll = (dir: number) => ref.current?.scrollBy({ left: dir * ref.current.clientWidth * 0.85, behavior: 'smooth' })
  return (
    <section className="group/row relative">
      <div className="mb-3 flex items-center justify-between">
        {more ? (
          <Link to={more} className="group/title flex items-center gap-1 text-lg font-semibold tracking-tight hover:text-accent-strong">
            {title}
            <ChevronRight className="size-5 text-muted transition-transform group-hover/title:translate-x-0.5" />
          </Link>
        ) : (
          <h2 className="text-lg font-semibold tracking-tight">{title}</h2>
        )}
        <div className="hidden gap-1 opacity-0 transition-opacity group-hover/row:opacity-100 md:flex">
          <button onClick={() => scroll(-1)} className="rounded-md p-1.5 text-muted hover:bg-text/10 hover:text-text" aria-label="Scroll left">
            <ChevronLeft className="size-5" />
          </button>
          <button onClick={() => scroll(1)} className="rounded-md p-1.5 text-muted hover:bg-text/10 hover:text-text" aria-label="Scroll right">
            <ChevronRight className="size-5" />
          </button>
        </div>
      </div>
      <div ref={ref} className="scrollbar-none -mx-4 flex snap-x scroll-px-4 gap-4 overflow-x-auto px-4 pb-2 md:-mx-8 md:scroll-px-8 md:px-8">
        {children}
      </div>
    </section>
  )
}

export function RowItem({ wide = false, children }: { wide?: boolean; children: ReactNode }) {
  return <div className={`shrink-0 snap-start ${wide ? 'w-64 md:w-80' : 'w-32 md:w-40'}`}>{children}</div>
}
