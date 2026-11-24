import type { MediaApi } from '@contracts/media-api'
import type { MediaDetailDto, MediaSummaryDto } from '@contracts/media-v2'
import type { MoveSelection } from '@contracts/file-move-api'
import { translate, translateError } from '@renderer/shared/i18n'

export type RemovalCounts = { deleteFiles: number; unregisterFiles: number; albumFiles: number; albums: number; emptyCollections: number }
const emptyCounts = (): RemovalCounts => ({ deleteFiles: 0, unregisterFiles: 0, albumFiles: 0, albums: 0, emptyCollections: 0 })

function groupTargets(targets: MoveSelection[]) {
    const grouped = new Map<number, MoveSelection>()
    for (const target of targets) {
        const previous = grouped.get(target.mediaId)
        if (previous) {
            if (previous.revision !== target.revision) throw Error(translate('gallery.thisPostChangedAfterSelectionRefreshAndSelectIt'))
            if (previous.fileIds && target.fileIds) previous.fileIds = [...new Set([...previous.fileIds, ...target.fileIds])]
            else delete previous.fileIds
        } else grouped.set(target.mediaId, { ...target, ...(target.fileIds ? { fileIds: [...new Set(target.fileIds)] } : {}) })
    }
    return [...grouped.values()]
}

export function removalCounts(media: MediaSummaryDto, fileIds?: number[]): RemovalCounts | null {
    const counts = emptyCounts()
    if (media.isUserAlbum) {
        if (fileIds) counts.albumFiles = new Set(fileIds).size
        else counts.albums = 1
        return counts
    }
    const detail = media as Partial<MediaDetailDto>
    const files = [...(detail.files?.data ?? []), ...(media.galleryFile ? [media.galleryFile] : [])]
    if (fileIds) {
        for (const id of new Set(fileIds)) {
            const file = files.find(item => item.id === id)
            if (!file || !file.storageKind) return null
            if (file.storageKind === 'app_managed') counts.deleteFiles++
            else counts.unregisterFiles++
        }
    } else if (Number.isInteger(media.managedFileCount) && media.managedFileCount! >= 0 && media.managedFileCount! <= media.fileCount) {
        counts.deleteFiles = media.managedFileCount!
        counts.unregisterFiles = media.fileCount - counts.deleteFiles
    } else if (detail.files && !detail.files.hasNextPage && detail.files.data.length === media.fileCount) {
        return media.fileCount ? removalCounts(media, detail.files.data.map(file => file.id)) : { ...counts, emptyCollections: 1 }
    } else if (media.fileCount) return null
    if (!fileIds && media.fileCount === 0) counts.emptyCollections = 1
    return counts
}

export function selectedRemovalCounts(targets: MoveSelection[], rows: MediaSummaryDto[]): RemovalCounts | null {
    const result = emptyCounts()
    let targetsByParent: MoveSelection[]
    try { targetsByParent = groupTargets(targets) } catch { return null }
    const byParent = new Map<number, MediaSummaryDto[]>()
    for (const row of rows) {
        const matches = byParent.get(row.id)
        if (matches) matches.push(row)
        else byParent.set(row.id, [row])
    }
    for (const target of targetsByParent) {
        const matches = (byParent.get(target.mediaId) ?? []).filter(item => item.revision === target.revision)
        const media = matches[0]
        if (!media) return null
        const existing = (media as Partial<MediaDetailDto>).files
        const counts = removalCounts({ ...media, files: { ...existing, data: [...(existing?.data ?? []), ...matches.flatMap(item => item.galleryFile ? [item.galleryFile] : [])], hasNextPage: existing?.hasNextPage ?? true } } as MediaDetailDto, target.fileIds)
        if (!counts) return null
        for (const key of Object.keys(result) as (keyof RemovalCounts)[]) result[key] += counts[key]
    }
    return result
}

export async function prepareRemoval(targets: MoveSelection[], rows: MediaSummaryDto[], api: MediaApi): Promise<RemovalCounts> {
    const known = selectedRemovalCounts(targets, rows)
    if (known) return known
    const combined = emptyCounts()
    for (const target of groupTargets(targets)) {
        const result = await api.get({ mediaId: target.mediaId, fileLimit: 200, fileOffset: 0 })
        if (!result.ok) throw Error(translateError(result.error))
        const media = { ...result.data, files: { ...result.data.files, data: [...result.data.files.data] } }
        if (media.revision !== target.revision) throw Error(translate('gallery.thisPostChangedAfterSelectionRefreshAndSelectIt'))
        let counts = removalCounts(media, target.fileIds)
        let offset = media.files.data.length, more = media.files.hasNextPage
        while (!counts && more) {
            const next = await api.get({ mediaId: target.mediaId, fileLimit: 200, fileOffset: offset })
            if (!next.ok) throw Error(translateError(next.error))
            if (next.data.revision !== target.revision || !next.data.files.data.length) throw Error(translate('gallery.thisPostChangedAfterSelectionRefreshAndSelectIt'))
            media.files = { ...next.data.files, data: [...media.files.data, ...next.data.files.data] }
            offset = media.files.data.length; more = media.files.hasNextPage
            counts = removalCounts(media, target.fileIds)
        }
        if (!counts) throw Error(translate('gallery.cannotDetermineRemoval'))
        for (const key of Object.keys(combined) as (keyof RemovalCounts)[]) combined[key] += counts[key]
    }
    return combined
}

export function removalLabel(counts: RemovalCounts | null, collection = false): string {
    if (!counts) return translate('gallery.deleteOrUnregister')
    const retained = counts.unregisterFiles + counts.albumFiles + counts.albums + counts.emptyCollections
    if (counts.deleteFiles) return translate(retained ? 'gallery.deleteAndRemove' : 'gallery.deleteFiles')
    if (counts.albums && !counts.albumFiles && !counts.unregisterFiles && !counts.emptyCollections) return translate('gallery.deleteAlbum')
    if (counts.albumFiles && !counts.albums && !counts.unregisterFiles && !counts.emptyCollections) return translate('gallery.album.removeFile')
    if (counts.albums || counts.albumFiles) return translate('gallery.removeSelectedItems')
    return translate(collection ? 'gallery.unregisterCollection' : 'gallery.unregister')
}

export function removalConfirmation(counts: RemovalCounts, selectedCount: number, collection = false) {
    const lines = [translate('gallery.removal.confirm', { count: selectedCount })]
    const keys: Record<keyof RemovalCounts, string> = { deleteFiles: 'gallery.removal.deleteFiles', unregisterFiles: 'gallery.removal.unregisterFiles', albumFiles: 'gallery.removal.albumFiles', albums: 'gallery.removal.albums', emptyCollections: 'gallery.removal.emptyCollections' }
    for (const key of Object.keys(keys) as (keyof RemovalCounts)[]) if (counts[key]) lines.push(translate(keys[key], { count: counts[key] }))
    if (counts.deleteFiles) lines.push(translate('gallery.removal.permanent'))
    if (counts.deleteFiles && (counts.albumFiles || counts.albums)) lines.push(translate('gallery.removal.albumOverlap'))
    return { title: removalLabel(counts, collection), message: lines.join('\n\n'), confirmLabel: removalLabel(counts), danger: true }
}
