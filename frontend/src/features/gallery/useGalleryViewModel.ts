import { translateError, translate } from '@renderer/shared/i18n'
import { useCallback, useEffect, useMemo, useSyncExternalStore } from 'react'
import type { GalleryView, GallerySort, MediaFilter } from '@contracts/media-v2'
import { getApi } from '@renderer/shared/api'
import { useToast } from '@renderer/shared/hooks/useToast'
import { useDownloadViewModel } from '@renderer/features/download/useDownloadViewModel'
import { createGalleryStore } from './galleryStore'
import { useContentVisibility } from '@renderer/shared/hooks/useContentVisibility'

export function useGalleryViewModel(favoritesOnly = false, filter?: MediaFilter, galleryView: GalleryView = 'all', showAlbumFiles = false, presentation?: { sort: GallerySort; descending: boolean; details: boolean }) {
    const { showToast } = useToast()
    const contentVisibility = useContentVisibility()

    const store = useMemo(
        () => createGalleryStore(getApi().media, { favoritesOnly, expandAlbums: true, albumCards: true, galleryView, showAlbumFiles, ...presentation }),
        [favoritesOnly, galleryView, showAlbumFiles, contentVisibility.key, contentVisibility.showHidden, presentation?.sort, presentation?.descending, presentation?.details]
    )

    const snapshot = useSyncExternalStore(store.subscribe, store.getSnapshot, store.getSnapshot)

    const downloads = useDownloadViewModel()
    const { mediaRevision } = downloads
    const filterKey = JSON.stringify({ ...filter, ...(contentVisibility.showHidden ? { showHidden: true } : {}) })

    useEffect(() => () => store.cancelReads(), [store])
    useEffect(() => {
        if (!presentation?.details && presentation?.sort !== 'fileCreated') return
        let timer: ReturnType<typeof setTimeout> | undefined
        const refresh = () => {
            if (document.querySelector('[data-gallery-header-mode=selection],[role=menu]')) { timer = setTimeout(refresh, 1000); return }
            timer = undefined; void store.refresh({ background: true })
        }
        const stop = getApi().onMediaMetadataUpdated?.(() => {
            if (timer) return
            timer = setTimeout(refresh, 500)
        })
        return () => { stop?.(); clearTimeout(timer) }
    }, [store, presentation?.details, presentation?.sort])

    useEffect(() => {
        store.setFilter(JSON.parse(filterKey) as MediaFilter)
        void store.refresh()
    }, [store, mediaRevision, filterKey])

    const openInFolder = useCallback(
        async (mediaId: number): Promise<void> => {
            try {
                const result = await getApi().media.get({
                    mediaId,
                    fileLimit: 1,
                    fileOffset: 0
                })

                if (!result.ok) {
                    showToast(translateError(result.error), 'error')
                    return
                }

                const filepath = result.data.files.data[0]?.filepath

                if (!filepath) {
                    showToast(translate("gallery.couldNotFindTheRegisteredFilePath"), 'error')
                    return
                }

                await getApi().showItemInFolder(filepath)
            } catch {
                showToast(translate("gallery.couldNotOpenTheFileLocation"), 'error')
            }
        },
        [showToast]
    )

    return {
        contentVisibility,
        store,
        snapshot,
        downloads,
        openInFolder
    }
}
