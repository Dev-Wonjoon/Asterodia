import { useState } from 'react'
import type { GalleryLayout, GallerySort } from '@contracts/media-v2'

const key = 'asterodia:gallery-presentation:v1'
const defaults = { layout: 'cards' as GalleryLayout, sort: 'registered' as GallerySort, descending: true }
export function useGalleryPreferences() {
    const [value, setValue] = useState(() => {
        try {
            const stored = JSON.parse(localStorage.getItem(key) ?? 'null')
            return { layout: stored?.layout === 'details' ? 'details' as const : 'cards' as const,
                sort: (['name','registered','fileCreated','platform','tag'].includes(stored?.sort) ? stored.sort : 'registered') as GallerySort,
                descending: typeof stored?.descending === 'boolean' ? stored.descending : true }
        } catch { return defaults }
    })
    const update = (next: Partial<typeof value>) => setValue(current => {
        const result = { ...current, ...next }
        try { localStorage.setItem(key, JSON.stringify(result)) } catch { /* preferences are optional */ }
        return result
    })
    return { ...value, update }
}
