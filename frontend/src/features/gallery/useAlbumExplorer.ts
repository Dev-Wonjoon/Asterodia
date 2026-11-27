import { readPreference } from '../../../../internal/webserver/web/preferences.js'
import { useEffect, useRef, useState } from 'react'
import { getApi } from '@renderer/shared/api'
import { translate, translateError } from '@renderer/shared/i18n'
import type { AlbumEntry, AlbumExplorePage, AlbumExploreRequest, AlbumExplorerSort, MediaFilter } from '@contracts/media-v2'

export type ExplorerMode = 'icons' | 'list' | 'details'
export type ExplorerColumn = 'kind' | 'size' | 'date' | 'duration'
export interface ExplorerPreferences { mode: ExplorerMode; iconSize: number; sort: AlbumExplorerSort; descending: boolean; columns: ExplorerColumn[]; preview: boolean }
export interface ExplorerSnapshot { scrollTop: number; selected: string[]; count: number; filter: MediaFilter; recursive: boolean; focused?: string }
export const entryKey = (entry: AlbumEntry) => entry.kind === 'album' ? 'album:' + entry.album.id : 'file:' + entry.file.id
export const entryTitle = (entry: AlbumEntry) => entry.kind === 'album' ? entry.album.title : entry.title
const defaults = (): ExplorerPreferences => ({ mode: 'icons', iconSize: 160, sort: 'name', descending: false, columns: ['kind', 'size', 'date', 'duration'], preview: false })
export function readExplorerPreferences(id: number): ExplorerPreferences {
    const value = defaults()
    try {
        const saved = JSON.parse(readPreference('asterodia:album-view:' + id) ?? 'null')
        if (!saved) return value
        if (['icons', 'list', 'details'].includes(saved.mode)) value.mode = saved.mode
        if (typeof saved.iconSize === 'number' && Number.isFinite(saved.iconSize)) value.iconSize = Math.max(80, Math.min(240, saved.iconSize))
        if (['name', 'kind', 'size', 'date', 'duration'].includes(saved.sort)) value.sort = saved.sort
        if (typeof saved.descending === 'boolean') value.descending = saved.descending
        if (typeof saved.preview === 'boolean') value.preview = saved.preview
        if (Array.isArray(saved.columns)) value.columns = [...new Set<ExplorerColumn>(saved.columns.filter((key: ExplorerColumn) => ['kind', 'size', 'date', 'duration'].includes(key)))]
    } catch { /* A damaged or unavailable preference store uses the defaults. */ }
    return value
}
export function useExplorerPreferences(id: number) {
    const [preferences, setPreferences] = useState(() => readExplorerPreferences(id))
    function update(change: Partial<ExplorerPreferences>) {
        setPreferences(previous => {
            const next = { ...previous, ...change }
            try { localStorage.setItem('asterodia:album-view:' + id, JSON.stringify(next)) } catch { /* Viewing remains available without browser storage. */ }
            return next
        })
    }
    return [preferences, update] as const
}

export function useAlbumExplorer(request: AlbumExploreRequest, visibilityKey: string, restoreCount = 0) {
    const [page, setPage] = useState<AlbumExplorePage | null>(null)
    const [loading, setLoading] = useState(true), [loadingMore, setLoadingMore] = useState(false), [error, setError] = useState(''), [moreError, setMoreError] = useState(''), [epoch, setEpoch] = useState(0)
    const serial = useRef(0), pending = useRef(false)
    const key = JSON.stringify({ ...request, visibilityKey })
    const initialKey = useRef(key)
    useEffect(() => {
        const own = ++serial.current
        const wanted = key === initialKey.current && epoch === 0 ? Math.max(100, restoreCount) : 100
        pending.current = true
        setLoading(true); setLoadingMore(false); setError(''); setMoreError(''); setPage(null)
        void (async () => {
            try {
                let current: AlbumExplorePage | null = null
                do {
                    const result = await getApi().media.exploreAlbum({ ...request, offset: current?.data.length ?? 0, limit: 100 })
                    if (own !== serial.current) return
                    if (!result.ok) throw new Error(translateError(result.error))
                    if (current && (current.album.revision !== result.data.album.revision || current.revisionToken !== result.data.revisionToken)) throw new Error(translateError({ code: 'REVISION_CONFLICT', message: '', context: {} }))
                    current = current ? { ...result.data, data: [...current.data, ...result.data.data] } : result.data
                    if (current.hasNextPage && !result.data.data.length) throw new Error(translate('common.couldNotConfirmTheServerResponse'))
                } while (current.hasNextPage && current.data.length < wanted)
                setPage(current)
            } catch (failure) { if (own === serial.current) setError(failure instanceof Error ? failure.message : String(failure)) }
            finally { if (own === serial.current) { pending.current = false; setLoading(false) } }
        })()
        return () => { ++serial.current; pending.current = false }
    }, [key, epoch])
    async function more() {
        if (pending.current || !page?.hasNextPage) return
        pending.current = true; setLoadingMore(true); setMoreError('')
        const own = serial.current
        try {
            const result = await getApi().media.exploreAlbum({ ...request, offset: page.data.length, limit: 100 })
            if (own !== serial.current) return
            if (!result.ok) throw new Error(translateError(result.error))
            if (page.album.revision !== result.data.album.revision || page.revisionToken !== result.data.revisionToken) throw new Error(translateError({ code: 'REVISION_CONFLICT', message: '', context: {} }))
            if (result.data.hasNextPage && !result.data.data.length) throw new Error(translate('common.couldNotConfirmTheServerResponse'))
            setPage({ ...result.data, data: [...page.data, ...result.data.data] })
        } catch (failure) { if (own === serial.current) setMoreError(failure instanceof Error ? failure.message : String(failure)) }
        finally { if (own === serial.current) { pending.current = false; setLoadingMore(false) } }
    }
    return { page, loading, loadingMore, error, moreError, more, refresh: () => setEpoch(value => value + 1), key }
}
