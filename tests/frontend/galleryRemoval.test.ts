// @vitest-environment happy-dom
import { afterEach, expect, it, vi } from 'vitest'
import type { MediaApi } from '@contracts/media-api'
import type { MediaSummaryDto } from '@contracts/media-v2'
import { prepareRemoval, removalCounts, removalConfirmation, removalLabel, selectedRemovalCounts } from '@renderer/features/gallery/galleryRemoval'
import { setLanguage } from '@renderer/shared/i18n'

afterEach(() => setLanguage('ko'))
const media = (id: number, properties = {}) => ({ id, revision: 4, fileCount: 3, managedFileCount: 2, sourceKind: 'local', ...properties }) as MediaSummaryDto
const counts = (properties = {}) => ({ deleteFiles: 0, unregisterFiles: 0, albumFiles: 0, albums: 0, emptyCollections: 0, ...properties })

it('classifies ownership instead of inferring deletion from the media source', () => {
    expect(removalCounts(media(1))).toEqual(counts({ deleteFiles: 2, unregisterFiles: 1 }))
    expect(removalLabel(removalCounts(media(1)))).toBe('파일 삭제·항목 제거')
    expect(removalLabel(removalCounts(media(1, { managedFileCount: 3 })))).toBe('파일 삭제')
    expect(removalLabel(removalCounts(media(1, { sourceKind: 'download', managedFileCount: 0 })))).toBe('등록 해제')
    expect(removalLabel(removalCounts(media(1, { managedFileCount: undefined })))).toBe('파일 삭제 / 등록 해제')
})
it('keeps album originals even when their files are app managed', () => {
    const album = media(2, { isUserAlbum: true, galleryFile: { id: 21, storageKind: 'app_managed' } })
    expect(removalCounts(album)).toEqual(counts({ albums: 1 }))
    expect(removalCounts(album, [21])).toEqual(counts({ albumFiles: 1 }))
    expect(removalLabel(removalCounts(album, [21]))).toBe('앨범에서 제거')
    expect(removalConfirmation(removalCounts(album, [21])!, 1).message).not.toContain('휴지통')
})
it('deduplicates selections and counts file deletion, unregistering, and album references separately', () => {
    const rows = [media(1), media(2, { isUserAlbum: true }), media(3, { isUserAlbum: true })]
    const selected = selectedRemovalCounts([{ mediaId: 1, revision: 4 }, { mediaId: 1, revision: 4, fileIds: [11] }, { mediaId: 2, revision: 4, fileIds: [21, 21] }, { mediaId: 3, revision: 4 }], rows)
    expect(selected).toEqual(counts({ deleteFiles: 2, unregisterFiles: 1, albumFiles: 1, albums: 1 }))
    const dialog = removalConfirmation(selected!, 4)
    expect(dialog.confirmLabel).toContain('파일 삭제')
    for (const line of ['실제 파일 삭제: 2개', '등록 해제 — 원본 파일 유지: 1개', '앨범에서 제거 — 원본 파일 유지: 1개', '사용자 앨범 삭제 — 원본 파일 유지: 1개', '휴지통을 거치지 않습니다']) expect(dialog.message).toContain(line)
    setLanguage('en')
    const english = removalConfirmation(selected!, 4)
    expect(english.message).toContain('Delete actual files: 2')
    expect(english.message).toContain('Unregister — keep original files: 1')
    expect(english.message).toContain('bypasses the Recycle Bin')
})
it('counts only the selected file cards and preserves unknown ownership as unknown', () => {
    const rows = [media(1, { galleryFile: { id: 11, storageKind: 'app_managed' } }), media(1, { galleryFile: { id: 12, storageKind: 'unverified_legacy' } })]
    expect(selectedRemovalCounts([{ mediaId: 1, revision: 4, fileIds: [11] }, { mediaId: 1, revision: 4, fileIds: [12] }], rows)).toEqual(counts({ deleteFiles: 1, unregisterFiles: 1 }))
    expect(selectedRemovalCounts([{ mediaId: 1, revision: 4, fileIds: [13] }], rows)).toBeNull()
    expect(selectedRemovalCounts([{ mediaId: 1, revision: 4 }, { mediaId: 1, revision: 5 }], rows)).toBeNull()
})
it('loads missing file metadata without assuming that a whole parent will be deleted', async () => {
    const first = { ...media(1), files: { data: [{ id: 11, storageKind: 'external_reference' }], total: 2, hasNextPage: true } }
    const get = vi.fn().mockResolvedValueOnce({ ok: true, data: first }).mockResolvedValueOnce({ ok: true, data: { ...media(1), files: { data: [{ id: 12, storageKind: 'app_managed' }], total: 2, hasNextPage: false } } })
    expect(await prepareRemoval([{ mediaId: 1, revision: 4, fileIds: [12] }], [], { get } as unknown as MediaApi)).toEqual(counts({ deleteFiles: 1 }))
    expect(get).toHaveBeenLastCalledWith({ mediaId: 1, fileLimit: 200, fileOffset: 1 })
    expect(first.files.data).toHaveLength(1)
})
it('refuses stale or unresolved metadata before any confirmation', async () => {
    const target = [{ mediaId: 1, revision: 4, fileIds: [12] }]
    const get = vi.fn().mockResolvedValue({ ok: true, data: { ...media(1, { revision: 5 }), files: { data: [], hasNextPage: false } } })
    await expect(prepareRemoval(target, [], { get } as unknown as MediaApi)).rejects.toThrow()
    get.mockResolvedValue({ ok: true, data: { ...media(1), files: { data: [{ id: 12 }], hasNextPage: false } } })
    await expect(prepareRemoval(target, [], { get } as unknown as MediaApi)).rejects.toThrow('확인하지 못했습니다')
})
