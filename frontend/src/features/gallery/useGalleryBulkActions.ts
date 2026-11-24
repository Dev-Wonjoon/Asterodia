import { translate } from '@renderer/shared/i18n'
import { useEffect, useRef, useState } from 'react'
import type { MoveSelection } from '@contracts/file-move-api'
import { getApi } from '@renderer/shared/api'
import { useConfirm } from '@renderer/shared/hooks/useConfirm'
import type { GalleryBulkAction, GalleryBulkResult, GalleryStore } from './galleryStore'

export type BulkCommand = 'favorite' | 'unfavorite' | 'addTags' | 'removeTags' | 'openFolders' | 'unregister'

export function useGalleryBulkActions(store: GalleryStore, selection: MoveSelection[], onSuccess: (ids: number[]) => void) {
    const { confirm } = useConfirm()
    const [busy, setBusy] = useState(false)
    const [progress, setProgress] = useState<{ done: number; total: number } | null>(null)
    const [result, setResult] = useState<GalleryBulkResult | null>(null)
    const lock = useRef(false)
    const generation = useRef(0)
    const latest = useRef({ selection, onSuccess }); latest.current = { selection, onSuccess }
    useEffect(() => {
        ++generation.current; lock.current = false
        setBusy(false); setProgress(null); setResult(null)
        return () => { ++generation.current }
    }, [store])

    async function run(command: BulkCommand, names: string[] = []) {
        if (lock.current || !selection.length || store.getSnapshot().busy || store.getSnapshot().needsRefresh) return
        lock.current = true
        const own = generation.current
        const targets = selection.map(item => ({ ...item }))
        setBusy(true)
        try {
            if (command === 'unregister' || command === 'openFolders') {
                const accepted = await confirm(command === 'unregister'
                    ? { title: translate("gallery.unregisterSelectedCollections"), message: translate("gallery.unregisterSelectedPostsExternalOriginalsAndFilesWithUnknown", { count: targets.length }), confirmLabel: translate("gallery.unregister"), danger: true }
                    : { title: translate("gallery.openSelectedFileLocations"), message: translate("gallery.openFileLocationsForSelectedPostsEachFolderOpens", { count: targets.length }), confirmLabel: translate("common.open") })
                if (!accepted || own !== generation.current || JSON.stringify(latest.current.selection) !== JSON.stringify(targets)) return
            }
            const action: GalleryBulkAction = command === 'unregister' ? { kind: 'unregister' }
                : command === 'openFolders' ? { kind: 'openFolders', open: path => getApi().showItemInFolder(path) }
                : command === 'favorite' || command === 'unfavorite' ? { kind: 'favorite', value: command === 'favorite' }
                : { kind: 'tags', names, remove: command === 'removeTags' }
            if (own !== generation.current) return
            setResult(null); setProgress({ done: 0, total: targets.length })
            const outcome = await store.bulk(targets, action, (done, total) => {
                if (own === generation.current) setProgress({ done, total })
            })
            if (own !== generation.current) return
            if (outcome) { setResult(outcome); setProgress(null); latest.current.onSuccess(outcome.succeeded) }
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
