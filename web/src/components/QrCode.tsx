import qrcode from 'qrcode-generator'
import { useMemo } from 'react'

// A QR code of text, dark on white with the quiet margin scanners need.
export function QrCode({ value, size = 176, label }: { value: string; size?: number; label: string }) {
  const { count, path } = useMemo(() => {
    const q = qrcode(0, 'M')
    q.addData(value)
    q.make()
    const n = q.getModuleCount()
    let d = ''
    for (let r = 0; r < n; r++) {
      for (let c = 0; c < n; c++) {
        // Runs of dark modules in a row are one rectangle.
        if (!q.isDark(r, c) || (c > 0 && q.isDark(r, c - 1))) continue
        let w = 1
        while (c + w < n && q.isDark(r, c + w)) w++
        d += `M${c + 2} ${r + 2}h${w}v1h-${w}z`
      }
    }
    return { count: n + 4, path: d }
  }, [value])
  return (
    <svg role="img" aria-label={label} width={size} height={size} viewBox={`0 0 ${count} ${count}`} className="shrink-0 rounded-lg" shapeRendering="crispEdges">
      <rect width={count} height={count} fill="#ffffff" />
      <path d={path} fill="#000000" />
    </svg>
  )
}
