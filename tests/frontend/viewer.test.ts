// @vitest-environment happy-dom
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { createViewerQueue } from '../../internal/webserver/web/viewer-queue.js'
import { mountMediaViewer } from '../../internal/webserver/web/viewer.js'
import { deferred } from './reactHarness'

let viewer: ReturnType<typeof mountMediaViewer> | undefined
beforeEach(() => {
    vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => {})
    vi.spyOn(HTMLMediaElement.prototype, 'load').mockImplementation(() => {})
    vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue(undefined)
})
afterEach(() => { viewer?.destroy(); viewer = undefined; document.body.replaceChildren(); document.body.style.overflow = ''; vi.restoreAllMocks() })
const flush = async () => { for (let i = 0; i < 8; i++) await Promise.resolve() }
const button = (text: string) => [...document.querySelectorAll<HTMLButtonElement>('.sb-viewer button')].find(node => node.textContent === text)!
const option = (text: string) => [...document.querySelectorAll<HTMLLabelElement>('.sbv-options label')].find(node => node.textContent === text)!.querySelector('input')!
function setup(ids = [1, 2, 3]) {
    const getFile = vi.fn(async (id: number) => ({ id, title: `파일 ${id}`, kind: id === 1 ? 'video' : id === 2 ? 'audio' : 'image' }))
    viewer = mountMediaViewer({ mediaId: 9, getSequence: async () => ({ ids, title: '앨범' }), getFile,
        contentURL: file => `/media/${file.id}`, previewURL: file => `/preview/${file.id}`, thumbnailURL: () => null })
    return getFile
}
it.each([false, true])('wraps previous from the first file to the last with repeat=%s', repeat => {
    const queue = createViewerQueue([1, 2, 3], 1)
    queue.setRepeat(repeat)
    expect(queue.hasPrevious).toBe(true)
    expect(queue.previous()).toBe(3)
    expect(queue.index).toBe(2)
    expect(queue.repeat).toBe(repeat)
    expect(queue.previous()).toBe(2)
    expect(queue.previous()).toBe(1)
    expect(queue.previous()).toBe(3)
})

it.each([{ ids: [] }, { ids: [7] }, { ids: [7, 7] }])('keeps previous unavailable for fewer than two unique files: $ids', ({ ids }) => {
    const queue = createViewerQueue(ids, 7)
    for (const repeat of [false, true]) {
        queue.setRepeat(repeat)
        expect(queue.hasPrevious).toBe(false)
        expect(queue.previous()).toBe(ids[0])
        queue.next()
        expect(queue.hasPrevious).toBe(false)
        expect(queue.previous()).toBe(ids[0])
    }
})

it('keeps shuffled cycles unique and follows the same history backwards', () => {
    const queue = createViewerQueue([1, 2, 3, 4, 4], 2, () => 0.25)
    queue.setShuffle(true)
    expect(queue.current).toBe(2)
    const order = [queue.current]
    while (queue.hasNext) order.push(queue.next())
    expect(new Set(order).size).toBe(4)
    expect(queue.hasNext).toBe(false)
    for (const id of order.slice(0, -1).reverse()) expect(queue.previous()).toBe(id)
    expect(queue.previous()).toBe(order[3])
    expect(queue.previous()).toBe(order[2]); expect(queue.next()).toBe(order[3])
    queue.setRepeat(true)
    const nextCycle = [queue.next(), queue.next(), queue.next(), queue.next()]
    expect(new Set(nextCycle).size).toBe(4); expect(nextCycle[0]).not.toBe(order[3])
    expect(queue.previous()).toBe(nextCycle[2])
    for (const id of [...order, ...nextCycle.slice(0, 2)].reverse()) expect(queue.previous()).toBe(id)
    expect(queue.index).toBe(0)
    expect(queue.previous()).toBe(order[3])
    expect(queue.previous()).toBe(order[2])
    expect(queue.next()).toBe(order[3])
})

it('jumps within the displayed shuffle cycle and preserves previous-cycle history', () => {
    const queue = createViewerQueue([1, 2, 3, 4], 2, () => .25)
    queue.setShuffle(true); queue.setRepeat(true)
    const first = [queue.current, queue.next(), queue.next(), queue.next()]
    const second = [queue.next(), queue.next(), queue.next(), queue.next()]
    expect(queue.jump(1)).toBe(true); expect(queue.current).toBe(second[1]); expect(queue.index).toBe(1)
    expect(queue.previous()).toBe(second[0]); expect(queue.previous()).toBe(first[3])
    for (const value of [-1, 4, 1.5, NaN]) expect(queue.jump(value)).toBe(false)
    expect(queue.current).toBe(first[3])
    expect(createViewerQueue([], 0).jump(0)).toBe(false)
})

it.each(['button', 'keyboard'])('wraps from the first video to the last image using the previous %s', async input => {
    const getFile = setup(); await flush()
    expect(option('앨범 반복').checked).toBe(false)
    expect(button('← 이전').disabled).toBe(false)
    if (input === 'button') button('← 이전').click()
    else document.querySelector('video')!.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowLeft', bubbles: true, cancelable: true }))
    await flush()
    expect(getFile).toHaveBeenLastCalledWith(3)
    expect(document.querySelector('.sbv-image')).not.toBeNull()
    expect(document.querySelector('video')).toBeNull()
    expect(document.querySelector('.sbv-count')?.textContent).toBe('3 / 3')
    expect(button('다음 →').disabled).toBe(true)
    button('← 이전').click(); await flush()
    expect(getFile).toHaveBeenLastCalledWith(2)
    expect(document.querySelector('audio')).not.toBeNull()
})

it.each([{ ids: [] }, { ids: [1] }])('disables previous in the viewer for fewer than two files: $ids', async ({ ids }) => {
    const getFile = setup(ids); await flush()
    expect(button('← 이전').disabled).toBe(true)
    if (ids.length) {
        option('앨범 반복').click()
        button('다음 →').click(); await flush()
        expect(button('← 이전').disabled).toBe(true)
    }
    const calls = getFile.mock.calls.length
    button('← 이전').click()
    viewer!.dialog.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowLeft', bubbles: true, cancelable: true }))
    await flush()
    expect(getFile).toHaveBeenCalledTimes(calls)
})

it('seeks video with J and L, retains arrow file navigation, and protects controls and composition', async () => {
    const getFile = setup(); await flush()
    const video = document.querySelector('video')!
    Object.defineProperty(video, 'duration', { value: 100, configurable: true }); video.currentTime = 20
    const send = (target: Element, key: string, options: KeyboardEventInit = {}) => target.dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true, ...options }))
    send(video, 'l'); expect(video.currentTime).toBe(25)
    send(video, 'j'); expect(video.currentTime).toBe(20)
    send(video, 'l', { isComposing: true }); expect(video.currentTime).toBe(20)
    const slider = document.querySelector<HTMLInputElement>('.sbp-timeline')!
    send(slider, 'ArrowRight'); await flush(); expect(getFile).toHaveBeenCalledTimes(1)
    send(video, 'ArrowRight'); await flush(); expect(getFile).toHaveBeenLastCalledWith(2)
    const audio = document.querySelector('audio')!; Object.defineProperty(audio, 'duration', { value: 100, configurable: true }); audio.currentTime = 15
    send(audio, 'j'); expect(audio.currentTime).toBe(10)
})
it('opens only the current file, navigates video and audio with arrows, and stops on close', async () => {
    const trigger = document.createElement('button'); document.body.append(trigger); trigger.focus()
    const getFile = setup(); await flush()
    expect(getFile).toHaveBeenCalledExactlyOnceWith(1)
    expect(document.querySelectorAll('video')).toHaveLength(1); expect(button('← 이전').disabled).toBe(false)
    document.querySelector('video')!.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true, cancelable: true })); await flush()
    expect(getFile).toHaveBeenLastCalledWith(2); expect(document.querySelector('video')).toBeNull(); expect(document.querySelector('audio')).not.toBeNull()
    expect(document.querySelector('.sbv-count')?.textContent).toBe('2 / 3')
    document.querySelector('audio')!.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true })); await flush()
    expect(document.querySelector('.sbv-image')).not.toBeNull(); expect(button('다음 →').disabled).toBe(true)
    viewer!.destroy(); expect(document.querySelector('.sb-viewer')).toBeNull(); expect(document.activeElement).toBe(trigger); expect(document.body.style.overflow).toBe('')
    expect(HTMLMediaElement.prototype.pause).toHaveBeenCalled()
})
it('advances ended media only when enabled, starts the next audio, and leaves images manual', async () => {
    const getFile = setup(); await flush()
    document.querySelector('video')!.dispatchEvent(new Event('ended')); await flush(); expect(getFile).toHaveBeenCalledTimes(1)
    option('자동 다음').click(); document.querySelector('video')!.dispatchEvent(new Event('ended')); await flush()
    expect(getFile).toHaveBeenLastCalledWith(2); expect(HTMLMediaElement.prototype.play).toHaveBeenCalled()
    document.querySelector('audio')!.dispatchEvent(new Event('ended')); await flush(); expect(getFile).toHaveBeenLastCalledWith(3)
    await flush(); expect(getFile).toHaveBeenCalledTimes(3)
    option('앨범 반복').click(); expect(button('다음 →').disabled).toBe(false)
    button('다음 →').click(); await flush(); expect(getFile).toHaveBeenLastCalledWith(1)
})
it('ignores late file responses after closing and offers retry for a read failure', async () => {
    const pending = deferred<unknown>(), getFile = vi.fn(() => pending.promise)
    viewer = mountMediaViewer({ mediaId: 9, getSequence: async () => ({ ids: [1], title: '앨범' }), getFile, contentURL: () => '/file', previewURL: () => null, thumbnailURL: () => null })
    await flush(); viewer.destroy(); pending.resolve({ id: 1, kind: 'video' }); await flush(); expect(document.querySelector('video')).toBeNull()
    viewer = mountMediaViewer({ mediaId: 9, getSequence: async () => ({ ids: [1], title: '앨범' }), getFile: vi.fn().mockRejectedValueOnce(new Error('시험 오류')).mockResolvedValue({ id: 1, kind: 'image' }), contentURL: () => '/file', previewURL: () => null, thumbnailURL: () => null })
    await flush(); expect(document.querySelector('.sbv-status')?.textContent).toBe('시험 오류'); button('다시 시도').click(); await flush(); expect(document.querySelector('.sbv-image')).not.toBeNull()
})

it.each(['video', 'audio'])('starts the first %s immediately on opening, using the custom player', async kind => {
    viewer = mountMediaViewer({ mediaId: 9, getSequence: async () => ({ ids: [7], title: '앨범' }),
        getFile: async () => ({ id: 7, title: '노래', kind }),
        contentURL: () => '/file', previewURL: () => null, thumbnailURL: () => '/cover.jpg' })
    await flush()
    const media = document.querySelector<HTMLMediaElement>(kind)!
    expect(media.play).toHaveBeenCalledOnce()
    expect(media.controls).toBe(false)
    expect(document.querySelector('.sb-player .sbp-controls')).not.toBeNull()
    if (kind === 'audio') {
        expect(document.querySelector('.sbp-artwork img')?.getAttribute('src')).toBe('/cover.jpg')
        expect(document.querySelector('video')).toBeNull()
    }
})

it('leaves controls available when initial playback is rejected and ignores a late rejection from an old file', async () => {
    const rejected = deferred<void>()
    vi.mocked(HTMLMediaElement.prototype.play).mockReturnValueOnce(rejected.promise)
    setup(); await flush()
    button('다음 →').click(); await flush()
    const status = document.querySelector('.sbv-status')!.textContent
    rejected.reject(new Error('blocked')); await flush()
    expect(document.querySelector('.sbv-status')!.textContent).toBe(status)
    expect(document.querySelector('.sbp-controls button[aria-label="재생"]')).not.toBeNull()
})

it('offers the custom play control if the browser rejects autoplay', async () => {
    vi.mocked(HTMLMediaElement.prototype.play).mockRejectedValueOnce(new Error('NotAllowedError'))
    setup(); await flush()
    expect(document.querySelector('.sbv-status')!.textContent).toBe('재생 버튼을 눌러 시작해 주세요.')
    expect(document.querySelector('.sbp-controls button[aria-label="재생"]')).not.toBeNull()
})
