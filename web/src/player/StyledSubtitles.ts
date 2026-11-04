// ASS/SSA subtitles drawn the way they were made: positions, the fonts
// attached to the file, colors, karaoke and signs. JASSUB (libass built for
// the browser) draws them on a canvas over the video, and is only loaded
// when a styled track plays.
import { useEffect, useRef, useState } from 'react'
import { api, fontUrl } from '../api'

type Instance = { timeOffset: number; destroy(): Promise<void>; ready: Promise<void> }

/**
 * useStyledSubtitles draws track over video while on. clock is the seconds
 * to add to the video's own time to get the file's (an HLS stream that
 * started partway, less any subtitle timing). It says whether drawing
 * failed, so the player can show the words plainly instead.
 */
export function useStyledSubtitles(
  video: HTMLVideoElement | null,
  on: { itemId: number; trackId: string; version?: string } | undefined,
  clock: number,
): { failed: boolean } {
  const [failed, setFailed] = useState(false)
  const instance = useRef<Instance>(undefined)
  const clockNow = useRef(clock)
  clockNow.current = clock

  const key = on && video ? `${on.itemId}/${on.trackId}/${on.version ?? ''}` : ''
  useEffect(() => {
    setFailed(false)
    if (!key || !on || !video) return
    let gone = false
    ;(async () => {
      try {
        const [{ default: JASSUB }, subContent, fonts] = await Promise.all([
          import('jassub'),
          api.styledSubtitles(on.itemId, on.trackId, on.version),
          api.fonts(on.itemId).catch(() => []),
        ])
        if (gone) return
        const j = new JASSUB({
          video,
          subContent,
          fonts: fonts.map((f) => new URL(fontUrl(on.itemId, f.index), location.href).href),
          timeOffset: clockNow.current,
        }) as unknown as Instance
        instance.current = j
        await j.ready
      } catch {
        if (!gone) setFailed(true)
      }
    })()
    return () => {
      gone = true
      instance.current?.destroy().catch(() => {})
      instance.current = undefined
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key])

  useEffect(() => {
    if (instance.current) instance.current.timeOffset = clock
  }, [clock])

  return { failed }
}
