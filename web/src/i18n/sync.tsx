import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect } from 'react'
import { api } from '../api'
import i18n, { applyLanguage } from './index'

// Each profile can pick a language, which the server keeps so it follows them
// to every device; '' follows the browser. Titles and descriptions from the
// server come in the same language, so they're fetched again when it changes.

export function useMyLanguage(userId?: number) {
  return useQuery({ queryKey: ['my-language', userId], queryFn: api.myLanguage, enabled: userId !== undefined, staleTime: Infinity })
}

// LanguageSync shows every page in the profile's language.
export function LanguageSync({ userId }: { userId?: number }) {
  const qc = useQueryClient()
  const saved = useMyLanguage(userId).data?.language
  useEffect(() => {
    if (saved === undefined) return
    const before = i18n.language
    void applyLanguage(saved).then(() => {
      // Titles and descriptions follow the language too.
      if (i18n.language !== before) void qc.invalidateQueries({ predicate: (q) => q.queryKey[0] !== 'my-language' })
    })
    if (saved) return
    const onChange = () => void applyLanguage('')
    window.addEventListener('languagechange', onChange)
    return () => window.removeEventListener('languagechange', onChange)
  }, [saved, qc])
  return null
}

export function useSetMyLanguage(userId?: number) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: api.setMyLanguage,
    onMutate: (language) => qc.setQueryData(['my-language', userId], { language }),
    onError: () => qc.invalidateQueries({ queryKey: ['my-language', userId] }),
  })
}
