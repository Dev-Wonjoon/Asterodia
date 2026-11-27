import type { MediaFileDto, MediaSummaryDto } from '@contracts/media-v2'

export interface DemoItem {
    summary: MediaSummaryDto
    fileIds: number[]
    tags: { id: number; name: string }[]
    author: string
    platform: string
}

export const sampleNames = ['coast', 'mountains', 'city', 'garden', 'desert', 'orbit'] as const
export const samplePath = (name: string) => `sample:${name}`

export function createSeed() {
    const files: MediaFileDto[] = []
    const items: DemoItem[] = []
    const titles = ['Coastal light · 해안의 빛', 'Quiet peaks · 고요한 산', 'Blue hour · 도시의 저녁', 'Green room · 작은 정원', 'Desert shapes · 사막의 곡선', 'Orbit · 움직이는 순간']
    const now = 1795680000000
    sampleNames.forEach((name, index) => {
        const id = index + 1, video = name === 'orbit'
        files.push({ id, mediaId: id, fileKey: name, fileUuid: `demo-${name}`, filenameToken: `${name}.${video ? 'mp4' : 'svg'}`,
            directoryId: null, storageKind: 'app_managed', sourceUrl: null, title: titles[index],
            filepath: samplePath(video ? 'orbit-video' : name), relativePath: null, kind: video ? 'video' : 'image', position: 0,
            filesize: video ? 250000 : 2400, duration: video ? 8 : null, detectedFormat: video ? 'mp4' : 'svg',
            extension: video ? 'mp4' : 'svg', width: 960, height: 640, orientation: 1, frameCount: null, pageCount: null,
            primaryImageIndex: null, thumbnailPath: samplePath(name), thumbnailOrigin: 'app_generated', previewPath: null,
            createdAt: now - index * 86400000 })
        items.push({ summary: { id, sourceKind: 'download', requestUrl: `https://example.com/demo/${name}`, sourceTitle: titles[index],
            customTitle: null, title: titles[index], revision: 1, coverMode: 'auto', cover: { fileId: id, path: samplePath(name), kind: video ? 'video' : 'image' },
            fileCount: 1, isFavorite: index === 0 || index === 5, createdAt: files[index].createdAt, updatedAt: null },
            fileIds: [id], tags: [{ id: index < 2 ? 1 : 2, name: index < 2 ? 'nature' : 'inspiration' }],
            author: index < 3 ? 'studio.bucket' : 'everyday.archive', platform: index < 3 ? 'Instagram' : 'Sample' })
    })
    // Profile albums reference the original files; appending never duplicates them.
    items.push({ summary: { ...items[0].summary, id: 7, title: 'Instagram · @studio.bucket', sourceTitle: '@studio.bucket',
        requestUrl: 'https://www.instagram.com/studio.bucket/', fileCount: 3, isFavorite: false, createdAt: now + 1 },
        fileIds: [1, 2, 3], tags: [{ id: 3, name: 'travel' }], author: 'studio.bucket', platform: 'Instagram' })
    items.push({ summary: { ...items[3].summary, id: 8, title: 'Weekend mood · 주말의 취향', sourceKind: 'local',
        sourceTitle: null, requestUrl: null, isUserAlbum: true, fileCount: 2, isFavorite: false, createdAt: now + 2 },
        fileIds: [4, 5], tags: [{ id: 4, name: 'weekend' }], author: '', platform: 'Sample' })
    return { items, files }
}
