import { useEffect, useState, useSyncExternalStore } from 'react'
import { getApi } from '@renderer/shared/api'
import type { ContentFilters } from '@contracts/media-v2'

const changed = 'asterodia:content-filters-changed'
const listeners = new Set<() => void>()
let showHidden = false
export function setShowHidden(value: boolean) { showHidden = value; for (const listener of listeners) listener() }
const subscribe = (listener: () => void) => { listeners.add(listener); return () => { listeners.delete(listener) } }
const empty = (): ContentFilters => ({ hiddenPlatforms: [], hiddenTags: [] })
export function contentFiltersChanged(value: ContentFilters) { window.dispatchEvent(new CustomEvent(changed, { detail: value })) }
function normalize(value: ContentFilters): ContentFilters {
    if (!value || !Array.isArray(value.hiddenPlatforms) || !Array.isArray(value.hiddenTags)) throw Error('Invalid content filters')
    return { hiddenPlatforms: value.hiddenPlatforms, hiddenTags: value.hiddenTags }
}
export function useContentVisibility() {
    const visible = useSyncExternalStore(subscribe, () => showHidden, () => false)
    const [filters, setFilters] = useState<ContentFilters>(empty), [failed, setFailed] = useState(false)
    useEffect(() => {
        const read = getApi().getContentFilters
        if (typeof read !== 'function') return
        let live = true, serial = 0
        function apply(value: ContentFilters) {
            const next = normalize(value)
            setFilters(previous => JSON.stringify(previous) === JSON.stringify(next) ? previous : next)
            setFailed(false)
        }
        async function refresh() {
            const own = ++serial
            try { const value = await read(); if (live && own === serial) apply(value) }
            catch { if (live && own === serial) setFailed(true) }
        }
        function saved(event: Event) { ++serial; apply((event as CustomEvent<ContentFilters>).detail) }
        void refresh()
        const timer = setInterval(() => { if (!document.hidden) void refresh() }, 10000)
        const focus = () => { void refresh() }
        window.addEventListener(changed, saved); window.addEventListener('focus', focus)
        return () => { live = false; ++serial; clearInterval(timer); window.removeEventListener(changed, saved); window.removeEventListener('focus', focus) }
    }, [])
    return { ...filters, key: JSON.stringify(filters), showHidden: visible, setShowHidden, failed }
}
