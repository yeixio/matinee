// Plugins' players (docs/plugins.md). A player is a page from the plugin,
// shown in a sandboxed frame with no origin of its own, so it can't use the
// session. It asks the web player for what it needs with postMessage, and
// answer() does only these few things for it, as whoever is watching.

import { useQuery } from '@tanstack/react-query'
import { api, ApiError, fileUrl, response, type Item, type PluginPlayer } from './api'

export const usePlayers = () =>
  useQuery({ queryKey: ['players'], queryFn: api.players, staleTime: 60_000, retry: false })

// playerFor finds the plugin player for an item's kind, if one is running.
export const playerFor = (players: PluginPlayer[] | undefined, kind: string) =>
  players?.find((p) => p.itemKinds.includes(kind))

// Messages carry matinee: 1 so a player can tell them from others.
export type FromPlayer =
  | { matinee: 1; type: 'ready' }
  | { matinee: 1; type: 'request'; id: number; method: string; params?: Record<string, unknown> }
export type ToPlayer =
  | { matinee: 1; type: 'start'; item: PlayerItem; language: string }
  | { matinee: 1; type: 'response'; id: number; result?: unknown; error?: string }

// What a player learns about the item it plays.
export type PlayerItem = Pick<Item, 'id' | 'libraryId' | 'kind' | 'title' | 'year' | 'facts' | 'pluginData'>

export const playerItem = (it: Item): PlayerItem => ({
  id: it.id,
  libraryId: it.libraryId,
  kind: it.kind,
  title: it.title,
  year: it.year,
  facts: it.facts,
  pluginData: it.pluginData,
})

const storageUrl = (plugin: string, key: string) =>
  `/api/v1/plugins/${encodeURIComponent(plugin)}/storage/${key.split('/').map(encodeURIComponent).join('/')}`

async function bytes(url: string, init?: RequestInit) {
  const res = await fetch(url, { credentials: 'same-origin', ...init })
  if (!res.ok) await response(res)
  return res.arrayBuffer()
}

// Things a player may ask for. Anything else is refused.
export type Bridge = {
  plugin: string
  item: Item
  onExit: () => void
  onProgress?: (positionMs: number, finished: boolean) => void
}

// answer does what a player asked and returns the result, plus any buffers
// to hand over rather than copy.
export async function answer(b: Bridge, method: string, params: Record<string, unknown> = {}): Promise<[unknown, Transferable[]]> {
  const str = (k: string) => {
    const v = params[k]
    if (typeof v !== 'string' || v === '') throw new Error(`${method} needs ${k}`)
    return v
  }
  switch (method) {
    case 'file': {
      const buf = await bytes(fileUrl(b.item.id))
      return [buf, [buf]]
    }
    case 'libraryFile': {
      const q = new URLSearchParams({ path: str('path') })
      const buf = await bytes(`/api/v1/plugins/${encodeURIComponent(b.plugin)}/libraries/${b.item.libraryId}/file?${q}`)
      return [buf, [buf]]
    }
    case 'storage.list': {
      const prefix = typeof params.prefix === 'string' ? params.prefix : ''
      const res = await fetch(`/api/v1/plugins/${encodeURIComponent(b.plugin)}/storage?prefix=${encodeURIComponent(prefix)}`, {
        credentials: 'same-origin',
      })
      return [await response(res), []]
    }
    case 'storage.get': {
      try {
        const buf = await bytes(storageUrl(b.plugin, str('key')))
        return [buf, [buf]]
      } catch (e) {
        if (e instanceof ApiError && e.status === 404) return [null, []]
        throw e
      }
    }
    case 'storage.put': {
      const v = params.value
      if (!(v instanceof ArrayBuffer) && typeof v !== 'string') throw new Error('storage.put needs a value')
      const res = await fetch(storageUrl(b.plugin, str('key')), {
        method: 'PUT',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/octet-stream' },
        body: v,
      })
      await response(res)
      return [null, []]
    }
    case 'storage.delete': {
      await response(await fetch(storageUrl(b.plugin, str('key')), { method: 'DELETE', credentials: 'same-origin' }))
      return [null, []]
    }
    case 'progress': {
      const pos = Number(params.positionMs) || 0
      b.onProgress?.(Math.max(0, Math.round(pos)), params.finished === true)
      return [null, []]
    }
    case 'exit':
      b.onExit()
      return [null, []]
  }
  throw new Error(`unknown method ${method}`)
}
