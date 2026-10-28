import { ArrowLeft } from 'lucide-react'
import { useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { api, pluginFileUrl, type ItemDetail, type PluginPlayer as Player } from '../api'
import i18n from '../i18n'
import { answer, playerItem, type FromPlayer, type ToPlayer } from '../plugins'

// PluginPlayer plays an item with its plugin's page, in a frame that can't
// reach the session (see plugins.ts). The frame takes the mouse, so the
// back button stays, faded until it's pointed at.
export function PluginPlayer({ item, player, onExit }: { item: ItemDetail; player: Player; onExit: () => void }) {
  const { t } = useTranslation()
  const frame = useRef<HTMLIFrameElement>(null)

  useEffect(() => {
    const send = (msg: ToPlayer, transfer: Transferable[] = []) =>
      // The frame has no origin to name, so '*'; only that window gets it.
      frame.current?.contentWindow?.postMessage(msg, '*', transfer)
    const bridge = {
      plugin: player.plugin,
      item,
      onExit,
      onProgress: (pos: number, finished: boolean) => void api.setProgress(item.id, pos, finished).catch(() => {}),
    }
    const onMessage = (e: MessageEvent) => {
      if (!frame.current || e.source !== frame.current.contentWindow) return
      const msg = e.data as FromPlayer
      if (msg?.matinee !== 1) return
      if (msg.type === 'ready') {
        send({ matinee: 1, type: 'start', item: playerItem(item), language: i18n.language })
      } else if (msg.type === 'request' && typeof msg.id === 'number') {
        answer(bridge, msg.method, msg.params).then(
          ([result, transfer]) => send({ matinee: 1, type: 'response', id: msg.id, result }, transfer),
          (err: unknown) => send({ matinee: 1, type: 'response', id: msg.id, error: err instanceof Error ? err.message : String(err) }),
        )
      }
    }
    window.addEventListener('message', onMessage)
    return () => window.removeEventListener('message', onMessage)
  }, [item, player, onExit])

  // The player gets the keyboard straight away.
  useEffect(() => frame.current?.focus(), [])

  return (
    <div className="fixed inset-0 z-50 bg-black">
      <iframe
        ref={frame}
        title={item.title}
        src={pluginFileUrl(player.plugin, player.entry)}
        // No allow-same-origin: the page can't read the session or call the API.
        sandbox="allow-scripts allow-pointer-lock allow-downloads"
        allow="gamepad; fullscreen; autoplay"
        allowFullScreen
        className="size-full border-0"
      />
      <button
        onClick={onExit}
        className="absolute start-4 top-4 flex items-center gap-2 rounded-full bg-black/60 px-4 py-2 text-sm text-white opacity-40 backdrop-blur transition-opacity hover:opacity-100 focus-visible:opacity-100"
      >
        <ArrowLeft className="size-4 rtl:-scale-x-100" /> {t('back')}
      </button>
    </div>
  )
}
