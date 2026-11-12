import { useQuery } from '@tanstack/react-query'
import { Gamepad2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { api } from '../api'
import { Button } from './ui'

/** Settings → Streaming → Games: whether this server can play games, and a way in. */
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
    </section>
  )
}
