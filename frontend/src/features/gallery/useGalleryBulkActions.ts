import { translate } from '@renderer/shared/i18n'
import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import type { MoveSelection } from '@contracts/file-move-api'
import { getApi } from '@renderer/shared/api'
import { useConfirm } from '@renderer/shared/hooks/useConfirm'
import type { GalleryBulkAction, GalleryBulkResult, GalleryStore } from './galleryStore'
import { prepareRemoval, removalConfirmation } from './galleryRemoval'

export type BulkCommand = 'favorite' | 'unfavorite' | 'addTags' | 'removeTags' | 'addPlatforms' | 'removePlatforms' | 'openFolders' | 'unregister'

export function useGalleryBulkActions(store: GalleryStore, selection: MoveSelection[], onSuccess: (ids: number[], command: BulkCommand) => void) {
    const { confirm } = useConfirm()
    const [busy, setBusy] = useState(false)
    const [progress, setProgress] = useState<{ done: number; total: number } | null>(null)
    const [result, setResult] = useState<GalleryBulkResult | null>(null)
    const lock = useRef(false)
    const returnFocus = useRef<HTMLElement | null>(null)
    const generation = useRef(0)
    const latest = useRef({ selection, onSuccess }); latest.current = { selection, onSuccess }
    useEffect(() => {
        ++generation.current; lock.current = false
        setBusy(false); setProgress(null); setResult(null)
        return () => { ++generation.current }
    }, [store])
    useLayoutEffect(() => {
        if (busy) return
        const origin = returnFocus.current
        returnFocus.current = null
        // The confirmation closes before busy cards are enabled again.
        if (origin?.isConnected && document.activeElement === document.body && !document.querySelector('dialog[open],[aria-modal="true"]')) origin.focus({ preventScroll: true })
    }, [busy])

    function commandSelection(command: BulkCommand, values: MoveSelection[]) {
        return command === 'unregister' ? values : values.filter(item => !item.fileIds)
    }
    async function run(command: BulkCommand, names: string[] = [], fallback: MoveSelection[] = []) {
        const selected = commandSelection(command, selection)
        const values = selected.length ? selected : command === 'unregister' ? fallback : []
        const initial = store.getSnapshot()
        const initialPage = initial.list?.page
        if (lock.current || !values.length || initial.busy || initial.needsRefresh) return
        lock.current = true
        const own = generation.current
        const selectionSnapshot = JSON.stringify(selected)
        const targets = values.map(item => ({ ...item, ...(item.fileIds ? { fileIds: [...item.fileIds] } : {}) }))
        setBusy(true)
        try {
            if (command === 'unregister' || command === 'openFolders') {
                returnFocus.current = document.activeElement instanceof HTMLElement ? document.activeElement : null
                const counts = command === 'unregister' ? await prepareRemoval(targets, initial.list?.page.data ?? [], getApi().media) : null
                if (own !== generation.current || store.getSnapshot().list?.page !== initialPage
                    || JSON.stringify(commandSelection(command, latest.current.selection)) !== selectionSnapshot) return
                const accepted = await confirm(command === 'unregister'
                    ? removalConfirmation(counts!, targets.reduce((count, item) => count + (item.fileIds?.length ?? 1), 0))
                    : { title: translate("gallery.openSelectedFileLocations"), message: translate("gallery.openFileLocationsForSelectedPostsEachFolderOpens", { count: targets.length }), confirmLabel: translate("common.open") })
                const current = store.getSnapshot()
                if (!accepted || own !== generation.current || current.busy || current.needsRefresh || current.list?.page !== initialPage
                    || JSON.stringify(commandSelection(command, latest.current.selection)) !== selectionSnapshot) return
            }
            const action: GalleryBulkAction = command === 'unregister' ? { kind: 'unregister' }
                : command === 'openFolders' ? { kind: 'openFolders', open: path => getApi().showItemInFolder(path) }
                : command === 'favorite' || command === 'unfavorite' ? { kind: 'favorite', value: command === 'favorite' }
                : { kind: command === 'addPlatforms' || command === 'removePlatforms' ? 'platforms' : 'tags', names, remove: command === 'removeTags' || command === 'removePlatforms' }
            if (own !== generation.current) return
            setResult(null); setProgress({ done: 0, total: targets.length })
            const outcome = await store.bulk(targets, action, (done, total) => {
                if (own === generation.current) setProgress({ done, total })
            })
            if (own !== generation.current) return
            if (outcome) { setResult(outcome); setProgress(null); latest.current.onSuccess(outcome.succeeded, command) }
            else setProgress(null)
        } catch {
            if (own === generation.current) {
                setProgress(null)
                setResult({ succeeded: [], failed: targets.map(item => ({ mediaId: item.mediaId, message: translate("gallery.couldNotConfirmTheResultRefreshAndCheckAgain") })) })
            }
        } finally { if (own === generation.current) { lock.current = false; setBusy(false) } }
    }
    return { busy, progress, result, run }
}
