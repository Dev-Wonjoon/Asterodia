import { readPreference } from '../../../../internal/webserver/web/preferences.js'
import { useSyncExternalStore } from 'react'

type ActionDisplay = 'icons' | 'details'
const key = 'asterodia:action-display:v1'
const listeners = new Set<() => void>()
let fallback: ActionDisplay = 'icons'
let volatile = false
function snapshot(): ActionDisplay {
    if (volatile) return fallback
    try { return readPreference(key) === 'details' ? 'details' : 'icons' }
    catch { return fallback }
}
function notify() { for (const listener of listeners) listener() }
function storage(event: StorageEvent) {
    if (event.key === key || event.key === null) { volatile = false; notify() }
}
function subscribe(listener: () => void) {
    if (!listeners.size) window.addEventListener('storage', storage)
    listeners.add(listener)
    return () => {
        listeners.delete(listener)
        if (!listeners.size) window.removeEventListener('storage', storage)
    }
}
export function useActionDisplay() {
    const mode = useSyncExternalStore(subscribe, snapshot, () => 'icons' as const)
    return { mode, toggle: () => {
        fallback = snapshot() === 'icons' ? 'details' : 'icons'
        try { localStorage.setItem(key, fallback); volatile = false }
        catch { volatile = true }
        notify()
    } }
}
