import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Cpu, Gamepad2, Trash2, Upload } from 'lucide-react'
import { useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { api, ApiError, type GameEmulator } from '../api'
import { Button } from './ui'

/** Settings → Streaming → Games: whether this server can play games, a way in, and its emulators. */
export function GameStreamingSettings() {
  const { t } = useTranslation()
  const { data } = useQuery({ queryKey: ['game-streaming'], queryFn: api.gameStreaming })
  return (
    <section className="space-y-4">
      <h2 className="text-lg font-semibold">{t('settings.games.title')}</h2>
      <p className="text-sm text-muted">{t('settings.games.intro')}</p>
      {data && (
        <div className="space-y-4 rounded-xl border border-line/60 bg-surface/50 p-4">
          <p className="text-sm text-muted">
            {data.available ? t('settings.games.ready', { systems: data.systems.map((s) => s.name).join(', ') }) : t('settings.games.notSetUp')}
          </p>
          {data.available && (
            <Link to="/games">
              <Button variant="primary" icon={<Gamepad2 className="size-4" />}>
                {t('settings.games.open')}
              </Button>
            </Link>
          )}
        </div>
      )}
      {data?.available && <Emulators />}
    </section>
  )
}

/** The emulators on the server, and adding one of your own. */
function Emulators() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const input = useRef<HTMLInputElement>(null)
  const { data } = useQuery({ queryKey: ['game-emulators'], queryFn: api.gameEmulators })
  const done = () => {
    qc.invalidateQueries({ queryKey: ['game-emulators'] })
    qc.invalidateQueries({ queryKey: ['game-streaming'] })
  }
  const add = useMutation({ mutationFn: (f: File) => api.addGameEmulator(f), onSuccess: done })
  const remove = useMutation({ mutationFn: (file: string) => api.removeGameEmulator(file), onSuccess: done })
  const error = add.error ?? remove.error

  return (
    <div className="space-y-3">
      <h3 className="font-semibold">{t('settings.games.emulators')}</h3>
      <p className="text-sm text-muted">{t('settings.games.emulatorsIntro', { platform: data?.platform ?? 'Linux', ext: data?.ext ?? '.so' })}</p>
      <p className="text-sm text-muted">{t('settings.games.emulatorsTrust')}</p>
      <div className="flex flex-wrap items-center gap-3">
        <input
          ref={input}
          type="file"
          accept=".so,.dll,.dylib"
          className="hidden"
          onChange={(e) => {
            const f = e.target.files?.[0]
            e.target.value = ''
            if (f) add.mutate(f)
          }}
        />
        <Button icon={<Upload className="size-4" />} disabled={add.isPending} onClick={() => input.current?.click()}>
          {add.isPending ? t('settings.games.adding') : t('settings.games.addEmulator')}
        </Button>
        {add.isSuccess && <span className="text-sm text-muted">{t('settings.games.added', { name: add.data.name })}</span>}
      </div>
      {error && <p className="text-sm text-danger">{error instanceof ApiError ? error.message : t('settings.games.couldntAdd')}</p>}
      <ul className="divide-y divide-line/60 rounded-xl border border-line/60 bg-surface/50">
        {data?.emulators.map((e) => (
          <EmulatorRow key={e.file} emulator={e} onRemove={() => confirm(t('settings.games.removeConfirm', { name: e.name })) && remove.mutate(e.file)} />
        ))}
      </ul>
      <p className="text-sm text-muted">{t('settings.games.bios')}</p>
    </div>
  )
}

function EmulatorRow({ emulator: e, onRemove }: { emulator: GameEmulator; onRemove: () => void }) {
  const { t } = useTranslation()
  const plays = e.systems?.length ? e.systems.join(', ') : e.extensions.join(' ')
  return (
    <li className="flex items-center gap-3 px-4 py-3">
      <Cpu className="size-4 shrink-0 text-muted" />
      <div className="min-w-0 flex-1">
        <div className="truncate font-medium">
          {e.name}
          {e.version && <span className="ms-2 text-sm font-normal text-muted">{e.version}</span>}
        </div>
        {e.error ? (
          <div className="text-sm text-danger">{t('settings.games.cantUse', { reason: e.error })}</div>
        ) : (
          <div className="truncate text-sm text-muted">{t('settings.games.plays', { what: plays })}</div>
        )}
      </div>
      {e.builtIn ? (
        <span className="shrink-0 text-xs text-muted">{t('settings.games.builtIn')}</span>
      ) : (
        <button type="button" className="rounded-lg p-2 text-muted hover:bg-text/10 hover:text-danger" aria-label={t('settings.games.remove')} onClick={onRemove}>
          <Trash2 className="size-4" />
        </button>
      )}
    </li>
  )
}
