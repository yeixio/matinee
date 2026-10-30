// The pills that switch a music or audiobook library between its views. On
// phones they share the width so every view shows without scrolling; a
// language with long names still scrolls.
export function ViewTabs<V extends string>({ views, view, label, onPick }: { views: V[]; view: V; label: (v: V) => string; onPick: (v: V) => void }) {
  return (
    <nav className="scrollbar-none -mx-4 flex gap-1 overflow-x-auto px-4 md:mx-0 md:gap-2 md:px-0" role="tablist">
      {views.map((v) => (
        <button
          key={v}
          role="tab"
          aria-selected={view === v}
          onClick={() => onPick(v)}
          className={`min-w-fit flex-1 shrink-0 rounded-full px-2.5 py-1.5 text-[13px] transition-colors md:flex-none md:px-4 md:text-sm ${
            view === v ? 'bg-text font-medium text-ink' : 'bg-text/5 text-muted hover:bg-text/10 hover:text-text'
          }`}
        >
          {label(v)}
        </button>
      ))}
    </nav>
  )
}
