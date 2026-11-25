// @vitest-environment happy-dom
import { createElement as h } from 'react'
import { afterEach, expect, it, vi } from 'vitest'
import { GalleryCard } from '@renderer/features/gallery/GalleryCard'
import { MediaPlayer } from '@renderer/shared/components/media/MediaPlayer'
import { cleanupReact, mount } from './reactHarness'

afterEach(async () => { await cleanupReact(); vi.restoreAllMocks() })

it.each([
    { extension: 'MP4', cover: { path: '/thumbnail.jpg', kind: 'video' } },
    { extension: 'MP3', cover: null },
    { extension: 'MP4', galleryFile: { extension: 'JPG', filepath: '/api/files/2/content' } },
])('shows the actual file extension instead of the thumbnail or HTTP route', async value => {
    const media = { id: 1, title: '파일', sourceKind: 'download', fileCount: 1, createdAt: 0, ...value } as any
    await mount(h(GalleryCard, { media, previewPath: null, onView: vi.fn(), onDetails: vi.fn(), children: null }))
    const metadata = document.querySelector('article p:last-child')!
    expect(metadata.firstElementChild!.textContent).toBe(value.galleryFile?.extension ?? value.extension)
    expect(metadata.lastElementChild!.textContent).toBe(new Date(0).toLocaleDateString('ko-KR'))
    if (value.extension === 'MP3') expect(document.querySelector('[aria-label="오디오"]')).not.toBeNull()
})

it('uses the custom audio player with artwork in the detail preview', async () => {
    vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => {})
    vi.spyOn(HTMLMediaElement.prototype, 'load').mockImplementation(() => {})
    await mount(h(MediaPlayer, { path: 'C:/song.mp3', kind: 'audio', title: '노래', poster: 'C:/cover.jpg' }))
    expect(document.querySelector('.sb-player .sbp-controls')).not.toBeNull()
    expect(document.querySelector<HTMLAudioElement>('audio')!.controls).toBe(false)
    expect(document.querySelector('.sbp-artwork img')?.getAttribute('src')).toBe('/media?path=C%3A%2Fcover.jpg')
})
