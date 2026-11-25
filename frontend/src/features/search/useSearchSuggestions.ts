import { translateError, translate } from '@renderer/shared/i18n'
import { useCallback, useEffect, useRef, useState } from 'react'
import type { MediaFilter, SearchSuggestion, SearchSuggestionPage } from '@contracts/media-v2'
import { getApi } from '@renderer/shared/api'
import { useContentVisibility } from '@renderer/shared/hooks/useContentVisibility'

export const SEARCH_SUGGEST_DELAY = 50
const PAGE_SIZE = 10

export function useSearchSuggestions(text: string, filter: MediaFilter, enabled: boolean, composing: boolean) {
    const visibility = useContentVisibility()
    const [page, setPage] = useState<SearchSuggestionPage>({ data: [], offset: 0, hasNextPage: false, revision: 0, indexed: false })
    const [loading, setLoading] = useState(false)
    const [error, setError] = useState<string | null>(null)
    const serial = useRef(0)
    const busy = useRef(false)
    const wasComposing = useRef(false)
    const cache = useRef(new Map<string, { page: SearchSuggestionPage; at: number }>())
    const filterKey = JSON.stringify({ ...filter, keyword: undefined })
    const key = JSON.stringify([text, filterKey, visibility.key])
    const current = useRef(key)
    current.current = key
    const pageRef = useRef(page)
    pageRef.current = page

    const read = useCallback(async (offset: number, own: number): Promise<void> => {
        if (busy.current) return
        busy.current = true; setLoading(true); setError(null)
        try {
            const result = await getApi().media.searchSuggest({ text, filter: JSON.parse(filterKey), limit: PAGE_SIZE, offset })
            if (serial.current !== own || current.current !== key) return
            if (!result.ok) throw new Error(translateError(result.error))
            const next = result.data
            if (offset > 0 && next.revision !== pageRef.current.revision) {
                // Do not splice pages from different catalog revisions.
                busy.current = false
                await read(0, own)
                return
            }
            const data = offset === 0 ? next.data : [...pageRef.current.data, ...next.data]
            const merged = { ...next, data }
            setPage(merged); pageRef.current = merged
            if (offset === 0) {
                cache.current.delete(key)
                cache.current.set(key, { page: next, at: Date.now() })
                if (cache.current.size > 32) cache.current.delete(cache.current.keys().next().value!)
            }
        } catch (failure) {
            if (serial.current === own && current.current === key) setError(failure instanceof Error ? failure.message : translate("search.couldNotLoadSuggestions"))
        } finally {
            if (serial.current === own) { busy.current = false; setLoading(false) }
        }
    }, [text, filterKey, key])

    useEffect(() => {
        const compositionEnded = wasComposing.current && !composing
        wasComposing.current = composing
        const own = ++serial.current
        busy.current = false; setLoading(false); setError(null)
        const empty = { data: [] as SearchSuggestion[], offset: 0, hasNextPage: false, revision: 0, indexed: false }
        const cached = cache.current.get(key)
        const initial = enabled && cached && Date.now() - cached.at < 30000 ? cached.page : empty
        setPage(initial); pageRef.current = initial
        if (!enabled || composing || !text.trim()) return
        const timer = setTimeout(() => { void read(0, own) }, compositionEnded ? 0 : SEARCH_SUGGEST_DELAY)
        return () => { clearTimeout(timer); ++serial.current; busy.current = false }
    }, [read, key, enabled, composing, text])

    const more = useCallback(() => {
        if (!enabled || composing || busy.current || !pageRef.current.hasNextPage || error) return
        void read(pageRef.current.data.length, serial.current)
    }, [read, enabled, composing, error])
    const retry = useCallback(() => { if (enabled && !composing) void read(0, serial.current) }, [read, enabled, composing])
    return { ...page, loading, error, more, retry }
}
