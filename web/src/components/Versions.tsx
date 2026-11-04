import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { ItemDetail, Version } from '../api'
import { canDirectPlay } from '../player/direct'
import { chooseVersion, chosenVersion, pickVersion, screenHeight, versionDetail, versionLabel } from '../player/versions'
import { inputClass } from './ui'

let probe: HTMLVideoElement | undefined
const playsHere = (v: Version) => {
  if (typeof document === 'undefined') return false
  probe ??= document.createElement('video')
  return canDirectPlay(v, probe)
}

/** defaultVersion is what plays of a title when nobody picks. */
export function defaultVersion(item: ItemDetail): Version | undefined {
  const vs = item.versions
  if (!vs?.length) return undefined
  const title = item.versionOf ?? item.id
  return chosenVersion(title, vs) ?? pickVersion(vs, { maxHeight: screenHeight(), plays: playsHere })
}

/** useVersion is the version of a title that Play plays, and a way to pick another. */
export function useVersion(item: ItemDetail): [Version | undefined, (id: number) => void] {
  const [picked, setPicked] = useState<number>()
  const vs = item.versions
  const chosen = vs?.find((v) => v.id === picked) ?? defaultVersion(item)
  const pick = (id: number) => {
    setPicked(id)
    chooseVersion(item.versionOf ?? item.id, id)
  }
  return [chosen, pick]
}

/** VersionPicker picks which file of a title plays: 4K or 1080p, theatrical or extended. */
export function VersionPicker({ item, value, onChange }: { item: ItemDetail; value?: Version; onChange: (id: number) => void }) {
  const { t } = useTranslation()
  const vs = item.versions
  if (!vs || vs.length < 2 || !value) return null
  return (
    <label className="flex min-w-0 items-center gap-2 text-sm text-muted">
      <span className="shrink-0">{t('common:versions.title')}</span>
      <select
        className={`${inputClass} w-auto max-w-xs`}
        value={value.id}
        onChange={(e) => onChange(Number(e.target.value))}
        aria-label={t('common:versions.choose')}
      >
        {vs.map((v, i) => {
          const label = versionLabel(v) || t('common:versions.numbered', { n: i + 1 })
          const detail = versionDetail(v)
          return (
            <option key={v.id} value={v.id}>
              {detail ? `${label} (${detail})` : label}
            </option>
          )
        })}
      </select>
    </label>
  )
}
