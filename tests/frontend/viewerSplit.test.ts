// @vitest-environment happy-dom
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { mountMediaViewer } from '../../internal/webserver/web/viewer.js'
import { resetShortcuts, setShortcut } from '../../internal/webserver/web/shortcuts.js'
import { setLanguage } from '@renderer/shared/i18n'
import { deferred } from './reactHarness'

let viewer: ReturnType<typeof mountMediaViewer> | undefined
let playing: WeakSet<HTMLMediaElement>
const flush = async () => { for (let i = 0; i < 20; i++) await Promise.resolve() }
const panes = () => [...document.querySelectorAll<HTMLElement>('.sbv-pane')]
const surface = (index = 0) => panes()[index].querySelector<HTMLElement>('.sbv-viewport')!
const click = (element: HTMLElement) => { element.focus(); element.click() }
const button = (text: string, parent: ParentNode = document) => [...parent.querySelectorAll<HTMLButtonElement>('button')].find(el => el.textContent === text)!
function key(value: string, options: KeyboardEventInit = {}) {
    const event = new KeyboardEvent('keydown', { key: value, bubbles: true, cancelable: true, ...options })
    document.activeElement!.dispatchEvent(event); return event
}
function select(index: number) { surface(index).dispatchEvent(new PointerEvent('pointerdown', { bubbles: true, button: 0, pointerId: 1 })); expect(document.activeElement).toBe(surface(index)) }
function scale(index = 0) { return panes()[index].querySelector('.sbv-scale')!.textContent }
beforeEach(() => {
    playing = new WeakSet(); localStorage.clear(); resetShortcuts(); setLanguage('ko')
    vi.spyOn(HTMLMediaElement.prototype, 'play').mockImplementation(function () { playing.add(this); this.dispatchEvent(new Event('play')); return Promise.resolve() })
    vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(function () { playing.delete(this); this.dispatchEvent(new Event('pause')) })
    vi.spyOn(HTMLMediaElement.prototype, 'paused', 'get').mockImplementation(function () { return !playing.has(this) })
    vi.spyOn(HTMLMediaElement.prototype, 'load').mockImplementation(() => {})
    vi.spyOn(HTMLMediaElement.prototype, 'duration', 'get').mockReturnValue(120)
    vi.spyOn(HTMLMediaElement.prototype, 'readyState', 'get').mockReturnValue(2)
    vi.spyOn(HTMLImageElement.prototype, 'naturalWidth', 'get').mockReturnValue(2000)
    vi.spyOn(HTMLImageElement.prototype, 'naturalHeight', 'get').mockReturnValue(1000)
    vi.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockReturnValue(800)
    vi.spyOn(HTMLElement.prototype, 'clientHeight', 'get').mockReturnValue(450)
    vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue({ setTransform() {}, clearRect() {}, drawImage() {} } as any)
})
afterEach(() => { viewer?.destroy(); viewer = undefined; document.body.replaceChildren(); document.body.style.overflow = ''; vi.useRealTimers(); vi.restoreAllMocks(); resetShortcuts(); setLanguage('ko') })
function setup(startId = 1, extra = {}) {
    const files = [1, 2, 3, 4].map(id => ({ id, mediaId: 9, kind: id === 2 ? 'video' : 'image', title: '파일 ' + id, width: 2000, height: 1000 }))
    const getFile = vi.fn(async (id: number) => files.find(file => file.id === id)!)
    const getSequence = vi.fn(async (id: number) => ({ ids: [1, 2, 3, 4], title: '앨범 ' + id, revision: 1 }))
    const listMedia = vi.fn(async () => ({ data: [{ id: 9, title: '앨범 9', fileCount: 4 }], hasNextPage: false }))
    const getAlbum = vi.fn(async () => ({ title: '앨범 9', revision: 1, files: { data: files, hasNextPage: false } }))
    viewer = mountMediaViewer({ mediaId: 9, fileId: startId, getSequence, getFile, listMedia, getAlbum,
        contentURL: file => '/media/' + file.id, previewURL: file => '/preview/' + file.id, thumbnailURL: () => null, ...extra })
    return { getFile, getSequence, getAlbum, listMedia }
}
async function choose(index: number, fileId: number) {
    click(button('미디어 선택/교체', panes()[index])); await flush()
    const row = document.querySelector<HTMLButtonElement>('.sbv-picker [data-file-id="' + fileId + '"]')!
    click(row); await flush(); expect(document.activeElement).toBe(surface(index))
}

it('zooms from the actual focused close button, keeps focus for repeated navigation, and clamps zoom', async () => {
    setup(); await flush(); expect(document.activeElement).toBe(button('닫기 · Esc'))
    expect(key('=', { code: 'Equal', ctrlKey: true }).defaultPrevented).toBe(true)
    expect(document.activeElement).toBe(surface()); expect(scale()).toBe('125%')
    key('+', { ctrlKey: true, shiftKey: true }); expect(scale()).toBe('150%')
    for (let i = 0; i < 20; i++) key('+', { ctrlKey: true, repeat: true })
    expect(scale()).toBe('800%')
    key('0', { ctrlKey: true }); expect(scale()).toBe('100%')
    key('-', { ctrlKey: true }); expect(scale()).toBe('100%')
    key('ArrowRight'); await flush(); expect(viewer!.currentFile?.id).toBe(2); expect(document.activeElement).toBe(surface())
    key('ArrowRight'); await flush(); expect(viewer!.currentFile?.id).toBe(3); expect(document.activeElement).toBe(surface())
    key('ArrowLeft'); await flush(); expect(viewer!.currentFile?.id).toBe(2)
})

it('keeps newly added panes empty across layout changes and targets only the selected queue and zoom', async () => {
    const { getFile } = setup(); await flush()
    click(button('2분할')); await flush(); expect(panes()).toHaveLength(2)
    click(button('4분할')); await flush(); expect(panes()).toHaveLength(4)
    expect(getFile.mock.calls.every(([id]) => id === 1)).toBe(true)
    await choose(1, 2); select(1); key('+', { ctrlKey: true }); expect(scale(1)).toBe('125%'); expect(scale(0)).toBe('100%')
    const video = document.querySelector('video')!; video.currentTime = 30
    key('l'); expect(video.currentTime).toBe(35)
    select(0); key('l'); expect(video.currentTime).toBe(35)
    key('ArrowLeft'); await flush(); expect(viewer!.currentFile?.id).toBe(4)
    select(1); expect(viewer!.currentFile?.id).toBe(2); expect(scale(1)).toBe('125%')
    click(button('1화면')); await flush()
    expect(panes()).toHaveLength(1); expect(viewer!.currentFile?.id).toBe(2); expect(scale()).toBe('125%'); expect(document.activeElement).toBe(surface())
})

function submitNumber(value: string) {
    const input = document.querySelector<HTMLInputElement>('.sbv-jump input')!
    input.focus(); input.value = value; input.dispatchEvent(new Event('input', { bubbles: true }))
    input.form!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
    return input
}
it('validates file numbers, protects input keys, and restores repeated shortcuts after a jump', async () => {
    const { getFile } = setup(); await flush()
    for (const value of ['', '0', '5', '1.5']) {
        const input = submitNumber(value); await flush()
        expect(input.getAttribute('aria-invalid')).toBe('true'); expect(document.activeElement).toBe(input)
        expect(viewer!.currentFile?.id).toBe(1)
    }
    expect(document.querySelector('.sbv-jump-error')!.textContent).toBe('1부터 4까지의 정수를 입력하세요.')
    setLanguage('en'); expect(document.querySelector('.sbv-jump-error')!.textContent).toBe('Enter a whole number from 1 to 4.')
    setLanguage('ko')
    key('ArrowRight'); key('+', {ctrlKey:true}); expect(getFile).toHaveBeenCalledOnce(); expect(scale()).toBe('100%')
    submitNumber('2'); await flush(); expect(viewer!.currentFile?.id).toBe(2); expect(document.activeElement).toBe(surface())
    expect(document.querySelector('video')!.paused).toBe(true)
    key('ArrowRight'); await flush(); expect(viewer!.currentFile?.id).toBe(3)
    key('+', {ctrlKey:true}); expect(scale()).toBe('125%')
    const calls = getFile.mock.calls.length
    submitNumber('3'); await flush(); expect(getFile).toHaveBeenCalledTimes(calls); expect(scale()).toBe('125%')
})

it('jumps only the selected independent pane and follows the displayed shuffled order', async () => {
    setup(); await flush(); click(button('2분할')); await flush(); await choose(1, 2)
    submitNumber('4'); await flush(); expect(viewer!.currentFile?.id).toBe(4)
    select(0); expect(viewer!.currentFile?.id).toBe(1)
    const shuffle = document.querySelectorAll<HTMLInputElement>('.sbv-options input')[1]; shuffle.click()
    key('ArrowRight'); await flush(); const shuffledSecond = viewer!.currentFile?.id
    submitNumber('1'); await flush(); expect(viewer!.currentFile?.id).toBe(1)
    submitNumber('2'); await flush(); expect(viewer!.currentFile?.id).toBe(shuffledSecond)
    select(1); expect(viewer!.currentFile?.id).toBe(4)
    expect(document.querySelector<HTMLInputElement>('.sbv-jump input')!.value).toBe('4')
})

it('keeps linked video playback singular on number jumps and ignores a pending jump after close', async () => {
    const { getFile } = setup(2); await flush(); click(button('2분할')); await flush(); click(button('같은 미디어')); await flush()
    expect(document.querySelectorAll('video')).toHaveLength(1)
    expect(document.querySelector('video')!.paused).toBe(false)
    submitNumber('3'); await flush(); expect(document.querySelectorAll('.sbv-image')).toHaveLength(2)
    submitNumber('2'); await flush(); expect(document.querySelectorAll('video')).toHaveLength(1)
    const video = document.querySelector('video')!; video.play(); submitNumber('2'); await flush(); expect(document.querySelector('video')).toBe(video)
    const pending = deferred<any>(); getFile.mockReturnValueOnce(pending.promise)
    submitNumber('4'); expect(document.querySelector<HTMLInputElement>('.sbv-jump input')!.disabled).toBe(true)
    viewer!.destroy(); pending.resolve({ id:4, kind:'video', title:'늦은 응답' }); await flush()
    expect(document.querySelector('video')).toBeNull(); expect(document.querySelector('.sb-viewer')).toBeNull()
})

it('preserves playback position, speed, audio state and zoom when changing layout', async () => {
    setup(2); await flush(); select(0); key('+', { ctrlKey: true })
    const old = document.querySelector('video')!; old.currentTime = 67; old.playbackRate = 1.5; old.volume = .35; old.muted = true
    click(button('4분할')); await flush()
    const current = document.querySelector('video')!
    expect(current).not.toBe(old); expect(old.getAttribute('src')).toBeNull()
    expect(current.currentTime).toBe(67); expect(current.playbackRate).toBe(1.5); expect(current.volume).toBe(.35); expect(current.muted).toBe(true)
    expect(scale()).toBe('125%'); expect(current.paused).toBe(false)
})

it('shows only the selected zoomed minimap, clamps dragging and restores focus for shortcuts', async () => {
    setup(); await flush(); click(button('2분할')); await flush(); await choose(1, 3)
    select(0); key('+', { ctrlKey: true }); key('+', { ctrlKey: true }); key('+', { ctrlKey: true })
    const firstMap = panes()[0].querySelector<HTMLElement>('.sbv-minimap')!, rect = firstMap.querySelector<HTMLButtonElement>('button')!
    expect(firstMap.hidden).toBe(false); expect(rect.style.width).toBe('50%')
    vi.spyOn(firstMap, 'getBoundingClientRect').mockReturnValue({ width: 160, height: 80 } as DOMRect)
    rect.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true, button: 0, pointerId: 7, clientX: 60, clientY: 30 }))
    rect.dispatchEvent(new PointerEvent('pointermove', { bubbles: true, pointerId: 7, clientX: 5000, clientY: -5000 }))
    rect.dispatchEvent(new PointerEvent('pointerup', { bubbles: true, pointerId: 7 }))
    expect(rect.style.left).toBe('50%'); expect(rect.style.top).toBe('0%'); expect(document.activeElement).toBe(surface())
    key('+', { ctrlKey: true }); expect(scale()).toBe('300%')
    const left = rect.style.left
    surface().querySelector('img')!.dispatchEvent(new PointerEvent('pointermove', { bubbles: true, clientX: -9000 }))
    expect(rect.style.left).toBe(left)
    select(1); expect(firstMap.hidden).toBe(true)
    key('+', { ctrlKey: true }); expect(panes()[1].querySelector<HTMLElement>('.sbv-minimap')!.hidden).toBe(false)
})

it('protects range, search, speed, settings inputs, composition and a second modal using real focus', async () => {
    setup(2); await flush()
    for (const selector of ['.sbp-timeline', '.sbp-volume', '.sbp-rate']) {
        document.querySelector<HTMLElement>(selector)!.focus()
        expect(key('+', { ctrlKey: true }).defaultPrevented).toBe(false)
        expect(key('ArrowRight').defaultPrevented).toBe(false)
    }
    select(0); expect(key('+', { ctrlKey: true, isComposing: true }).defaultPrevented).toBe(false)
    click(button('미디어 선택/교체')); await flush()
    expect(document.activeElement?.tagName).toBe('INPUT'); key('+', { ctrlKey: true }); expect(scale()).toBe('100%')
    const picker = document.querySelector<HTMLDialogElement>('.sbv-picker')!; picker.close(); await flush()
    select(0); const modal = document.createElement('dialog'); const other = document.createElement('button'); modal.append(other); document.body.append(modal); modal.showModal(); other.focus()
    expect(key('+', { ctrlKey: true }).defaultPrevented).toBe(false); expect(scale()).toBe('100%'); modal.remove()
})

it('uses one video and one audio output for linked panes and restores separate pane states paused', async () => {
    setup(); await flush(); click(button('2분할')); await flush(); await choose(1, 2)
    select(0); key('+', { ctrlKey: true }); select(1); key('+', { ctrlKey: true }); key('+', { ctrlKey: true })
    const before = document.querySelector('video')!; before.currentTime = 42; before.playbackRate = 1.5
    click(button('같은 미디어')); await flush(); click(button('4분할')); await flush()
    expect(document.querySelectorAll('video')).toHaveLength(1)
    expect(document.querySelectorAll('.sbv-canvas')).toHaveLength(4)
    expect(document.querySelectorAll('.sbp-controls')).toHaveLength(1)
    const source = document.querySelector('video')!; expect(source.currentTime).toBe(42); expect(source.playbackRate).toBe(1.5)
    for (let i = 0; i < 4; i++) { select(i); key('l') }
    expect(source.currentTime).toBe(62)
    select(3); key('+', { ctrlKey: true }); expect(scale(3)).toBe('125%'); expect(scale(0)).toBe('150%')
    const rate = document.querySelector<HTMLSelectElement>('.sbp-rate')!; rate.value = '2'; rate.dispatchEvent(new Event('change')); expect(source.playbackRate).toBe(2)
    select(2); key('k'); expect(source.paused).toBe(false); key('k'); expect(source.paused).toBe(true)
    click(button('서로 다른 미디어')); await flush()
    expect(panes()).toHaveLength(4); expect(scale(0)).toBe('125%'); expect(scale(1)).toBe('150%')
    expect(document.querySelector('video')!.currentTime).toBe(42); expect(document.querySelector('video')!.paused).toBe(true)
    select(0); expect(viewer!.currentFile?.id).toBe(1); select(1); expect(viewer!.currentFile?.id).toBe(2)
})

it('replaces a linked file in all panes and resets their zoom without creating another video', async () => {
    setup(2); await flush(); click(button('같은 미디어')); await flush(); click(button('4분할')); await flush()
    select(2); key('+', { ctrlKey: true }); await choose(2, 3)
    expect(document.querySelectorAll('video')).toHaveLength(0); expect(document.querySelectorAll('.sbv-viewport>.sbv-image')).toHaveLength(4)
    for (let i = 0; i < 4; i++) expect(scale(i)).toBe('100%')
    expect(document.activeElement).toBe(surface(2))
})

it('clears delayed playback gestures on layout change and does not treat minimap clicks as play', async () => {
    vi.useFakeTimers(); setup(2); await flush()
    document.querySelector('.sbv-canvas')!.dispatchEvent(new MouseEvent('click', { bubbles: true, detail: 1 }))
    const plays = vi.mocked(HTMLMediaElement.prototype.play).mock.calls.length
    click(button('2분할')); await flush(); await vi.advanceTimersByTimeAsync(600)
    // Reopening the playing pane is expected; the removed surface's delayed click is cancelled.
    expect(vi.mocked(HTMLMediaElement.prototype.play).mock.calls.length).toBe(plays + 1)
    select(0); key('+', { ctrlKey: true }); document.querySelector('.sbv-map-rect')!.dispatchEvent(new MouseEvent('click', { bubbles: true, detail: 1 }))
    await vi.advanceTimersByTimeAsync(600); expect(document.querySelector('video')!.paused).toBe(false)
    viewer!.destroy(); viewer = undefined; await vi.advanceTimersByTimeAsync(3000)
    expect(document.querySelectorAll('video,.sb-viewer')).toHaveLength(0); expect(vi.getTimerCount()).toBe(0)
})

it('seeks on double-click of the drawn video without toggling playback or scaling its controls', async () => {
    vi.useFakeTimers(); setup(2); await flush()
    const video = document.querySelector('video')!, canvas = document.querySelector('.sbv-canvas')!
    vi.spyOn(canvas, 'getBoundingClientRect').mockReturnValue({ left: 0, width: 800 } as DOMRect)
    video.currentTime = 20
    canvas.dispatchEvent(new MouseEvent('click', { bubbles: true, detail: 1, clientX: 20 }))
    canvas.dispatchEvent(new MouseEvent('click', { bubbles: true, detail: 2, clientX: 20 }))
    canvas.dispatchEvent(new MouseEvent('dblclick', { bubbles: true, cancelable: true, detail: 2, clientX: 20 }))
    await vi.advanceTimersByTimeAsync(600)
    expect(video.currentTime).toBe(10); expect(video.paused).toBe(false); expect(video.play).toHaveBeenCalledOnce()
    key('+', { ctrlKey: true }); expect(scale()).toBe('125%')
    expect(document.querySelector('.sbp-controls')!.closest('.sbv-viewport')).toBeNull()
    expect(document.activeElement).toBe(surface())
})

it('ignores stale loads after pane removal and close while preserving the selected pane load', async () => {
    const late = deferred<any>(), getFile = vi.fn((id: number) => id === 2 ? late.promise : Promise.resolve({ id, kind: 'image', title: '파일 ' + id, width: 2000, height: 1000 }))
    setup(1, { getFile }); await flush(); click(button('2분할')); await flush()
    click(button('미디어 선택/교체', panes()[1])); await flush(); click(document.querySelector<HTMLButtonElement>('[data-file-id="2"]')!); await flush()
    select(0); click(button('1화면')); await flush()
    late.resolve({ id: 2, kind: 'video' }); await flush(); expect(document.querySelector('video')).toBeNull(); expect(viewer!.currentFile?.id).toBe(1)
    viewer!.destroy(); viewer = undefined; expect(document.querySelector('.sb-viewer')).toBeNull()
})

it('retains an in-flight collection selection through layout changes and discards it after close', async () => {
    const pending = deferred<any>(), getSequence = vi.fn().mockResolvedValueOnce({ ids: [1], title: '처음', revision: 1 }).mockImplementation(() => pending.promise)
    setup(1, { getSequence }); await flush(); click(button('미디어 선택/교체')); await flush(); click(document.querySelector<HTMLButtonElement>('[data-file-id="3"]')!); await flush()
    click(button('2분할')); await flush(); pending.resolve({ ids: [3], title: '선택', revision: 1 }); await flush()
    expect(viewer!.currentFile?.id).toBe(3); expect(surface(1).children).toHaveLength(0)
    viewer!.destroy(); viewer = undefined; await flush(); expect(document.querySelector('.sb-viewer')).toBeNull()
})

it('preserves the initial autoplay request when splitting during an asynchronous file read', async () => {
    const pending = deferred<any>(); setup(2, { getFile: () => pending.promise }); await flush()
    click(button('2분할')); await flush(); select(1)
    pending.resolve({ id: 2, kind: 'video', title: '늦게 로드된 영상', width: 2000, height: 1000 }); await flush()
    expect(document.querySelectorAll('video')).toHaveLength(1)
    expect(document.querySelector('video')!.paused).toBe(false); expect(HTMLMediaElement.prototype.play).toHaveBeenCalledOnce()
    expect(document.activeElement).toBe(surface(1)); expect(surface(1).children).toHaveLength(0)
})

it('keeps fullscreen scoped to the selected pane and retranslates the controls', async () => {
    setup(); await flush(); click(button('2분할')); await flush(); select(1); key('f')
    expect(viewer!.dialog.classList.contains('sbv-expanded')).toBe(true)
    const escape = new Event('cancel', { cancelable: true }); viewer!.dialog.dispatchEvent(escape)
    expect(escape.defaultPrevented).toBe(true); expect(viewer!.dialog.classList.contains('sbv-expanded')).toBe(false); expect(document.activeElement).toBe(surface(1))
    setLanguage('en'); expect(button('Four panes')).toBeTruthy(); expect(button('Choose/replace media')).toBeTruthy()
})

it('supports remapped zoom commands without retaining the old default binding', async () => {
    setup(); await flush(); select(0); expect(setShortcut('viewer.zoomIn', 'Alt+Z')).toBeNull()
    expect(key('+', { ctrlKey: true }).defaultPrevented).toBe(false); expect(scale()).toBe('100%')
    key('z', { altKey: true }); expect(scale()).toBe('125%')
})

it('keeps shared image loading current when layout changes, and ignores it after closing', async () => {
    const pending = deferred<any>(), getFile = vi.fn().mockResolvedValueOnce({ id: 1, kind: 'image', width: 2000, height: 1000 }).mockImplementation(() => pending.promise)
    setup(1, { getFile }); await flush(); click(button('같은 미디어')); await flush()
    click(button('4분할')); await flush(); pending.resolve({ id: 1, kind: 'image', width: 2000, height: 1000 }); await flush()
    expect(document.querySelectorAll('.sbv-viewport>.sbv-image')).toHaveLength(4)
    key('ArrowRight'); await flush(); viewer!.destroy(); viewer = undefined; await flush()
    expect(document.querySelectorAll('.sbv-image,.sb-viewer')).toHaveLength(0)
})

it('preserves independent automatic-next settings and cleans up the removed video', async () => {
    setup(2); await flush(); click(button('2분할')); await flush(); await choose(1, 2)
    const videos = [...document.querySelectorAll('video')]
    select(0); click(document.querySelector<HTMLInputElement>('.sbv-options input')!)
    select(1); expect(document.querySelector<HTMLInputElement>('.sbv-options input')!.checked).toBe(false)
    videos[1].dispatchEvent(new Event('ended')); await flush(); expect(document.querySelectorAll('video')).toHaveLength(2)
    videos[0].dispatchEvent(new Event('ended')); await flush(); expect(document.querySelectorAll('video')).toHaveLength(1)
    select(0); expect(viewer!.currentFile?.id).toBe(3)
    click(button('1화면')); await flush(); expect(videos[1].getAttribute('src')).toBeNull()
})

it('returns focus from a removed player control after auto-next so consecutive keys keep working', async () => {
    setup(2); await flush(); click(document.querySelector<HTMLInputElement>('.sbv-options input')!)
    const play = document.querySelector<HTMLButtonElement>('.sbp-controls button')!; play.focus()
    document.querySelector('video')!.dispatchEvent(new Event('ended')); await flush()
    expect(document.activeElement).toBe(surface()); expect(viewer!.currentFile?.id).toBe(3)
    key('+', { ctrlKey: true }); expect(scale()).toBe('125%')
    key('ArrowRight'); await flush(); expect(viewer!.currentFile?.id).toBe(4)
})
