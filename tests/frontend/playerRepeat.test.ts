// @vitest-environment happy-dom
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { mountVideoPlayer } from '../../internal/webserver/web/player.js'
import { setLanguage } from '@renderer/shared/i18n'

let player: ReturnType<typeof mountVideoPlayer> | undefined
let playing: WeakSet<HTMLMediaElement>
beforeEach(() => {
    vi.useFakeTimers(); localStorage.clear(); setLanguage('ko'); playing = new WeakSet()
    vi.spyOn(HTMLMediaElement.prototype, 'duration', 'get').mockReturnValue(120)
    vi.spyOn(HTMLMediaElement.prototype, 'seeking', 'get').mockReturnValue(false)
    vi.spyOn(HTMLMediaElement.prototype, 'paused', 'get').mockImplementation(function () { return !playing.has(this) })
    vi.spyOn(HTMLMediaElement.prototype, 'play').mockImplementation(function () { playing.add(this); this.dispatchEvent(new Event('play')); return Promise.resolve() })
    vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(function () { playing.delete(this); this.dispatchEvent(new Event('pause')) })
    vi.spyOn(HTMLMediaElement.prototype, 'load').mockImplementation(() => {})
})
afterEach(() => { player?.destroy(); player = undefined; document.body.replaceChildren(); vi.useRealTimers(); vi.restoreAllMocks(); setLanguage('ko') })
const button = (label: string) => document.querySelector<HTMLButtonElement>(`button[aria-label="${label}"]`)!
function setup(extra = {}) {
    player = mountVideoPlayer(document.body, { src: '/synthetic.mp4', ...extra })
    player.video.dispatchEvent(new Event('loadedmetadata'))
    button('반복 재생').click()
    return player.video
}
function range(video: HTMLMediaElement, start = 10, end = 20) {
    video.currentTime = start; button('현재 위치를 시작점 A로 지정').click()
    video.currentTime = end; button('현재 위치를 끝점 B로 지정').click()
}
it('toggles native single-file repeat and clears it without persisting to other files', () => {
    const video = setup()
    expect(player!.isRepeating()).toBe(false)
    button('한 영상 반복').click()
    expect(video.loop).toBe(true); expect(player!.isRepeating()).toBe(true)
    expect(button('한 영상 반복').getAttribute('aria-pressed')).toBe('true')
    video.currentTime = 120; video.dispatchEvent(new Event('ended'))
    expect(video.currentTime).toBe(0); expect(video.play).toHaveBeenCalledOnce()
    button('반복 해제').click()
    expect(video.loop).toBe(false); expect(player!.isRepeating()).toBe(false)
    player!.destroy(); player = undefined; setup()
    expect(player!.isRepeating()).toBe(false)
})
it('requires ordered points and switches exclusively between single-file and range repeat', () => {
    const video = setup()
    expect(button('현재 위치를 끝점 B로 지정').disabled).toBe(true)
    video.currentTime = 10; button('현재 위치를 시작점 A로 지정').click()
    video.currentTime = 5; button('현재 위치를 끝점 B로 지정').click()
    expect(player!.getRepeatState()).toEqual({ mode: 'off', start: 10, end: null })
    video.currentTime = 10.05; button('현재 위치를 끝점 B로 지정').click()
    expect(player!.isRepeating()).toBe(false)
    video.currentTime = 20; button('현재 위치를 끝점 B로 지정').click()
    expect(player!.getRepeatState()).toEqual({ mode: 'range', start: 10, end: 20 })
    expect(document.querySelector('.sbp-repeat-status')!.textContent).toContain('A 0:10.0 · B 0:20.0')
    button('한 영상 반복').click(); expect(video.loop).toBe(true)
    expect(player!.getRepeatState().mode).toBe('one')
    video.currentTime = 120; button('현재 위치를 시작점 A로 지정').click()
    expect(player!.getRepeatState().mode).toBe('one')
    range(video); expect(video.loop).toBe(false)
})
it('loops at B and clamps out-of-range playback while respecting paused scrubbing', async () => {
    const video = setup(); range(video)
    await video.play()
    expect(video.currentTime).toBe(10)
    video.currentTime = 20.2; video.dispatchEvent(new Event('timeupdate')); expect(video.currentTime).toBe(10)
    video.currentTime = 4; video.dispatchEvent(new Event('seeked')); expect(video.currentTime).toBe(10)
    video.pause(); video.currentTime = 30; video.dispatchEvent(new Event('timeupdate'))
    expect(video.currentTime).toBe(30)
    await video.play(); expect(video.currentTime).toBe(10)
})
it('uses a boundary timer between timeupdate events and releases it on pause and destroy', async () => {
    const video = setup(); range(video); video.currentTime = 19.99
    await video.play(); video.currentTime = 20
    await vi.advanceTimersByTimeAsync(30); expect(video.currentTime).toBe(10)
    video.pause(); video.currentTime = 20
    await vi.advanceTimersByTimeAsync(11000); expect(video.currentTime).toBe(20)
    await video.play(); player!.destroy(); player = undefined
    video.currentTime = 20; await vi.advanceTimersByTimeAsync(11000)
    expect(video.currentTime).toBe(20)
})
it('restarts a range that ends at the media duration and stops after clearing it', () => {
    const video = setup(); range(video, 110, 120)
    video.dispatchEvent(new Event('ended')); expect(video.currentTime).toBe(110)
    expect(video.play).toHaveBeenCalledOnce()
    button('반복 해제').click(); video.currentTime = 120; video.dispatchEvent(new Event('ended'))
    expect(video.currentTime).toBe(120); expect(video.play).toHaveBeenCalledOnce()
})
it('validates restored points against new metadata and resets when the media is emptied', () => {
    const video = setup({ repeatState: { mode: 'range', start: 10, end: 200 } })
    expect(player!.getRepeatState()).toEqual({ mode: 'off', start: null, end: null })
    range(video); video.dispatchEvent(new Event('emptied'))
    expect(player!.isRepeating()).toBe(false); expect(button('현재 위치를 끝점 B로 지정').disabled).toBe(true)
})
it('opens an accessible repeat panel, closes it with Escape, and updates translations', () => {
    setup(); expect(button('반복 재생').getAttribute('aria-expanded')).toBe('true')
    button('한 영상 반복').click(); setLanguage('en')
    expect(document.querySelector('.sbp-repeat-status')!.textContent).toBe('Repeating current video')
    button('Repeat current video').dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))
    expect(document.querySelector<HTMLElement>('.sbp-repeat-panel')!.hidden).toBe(true)
    expect(document.activeElement).toBe(button('Repeat playback'))
    button('Repeat playback').click(); button('Player settings').click()
    expect(document.querySelector<HTMLElement>('.sbp-repeat-panel')!.hidden).toBe(true)
})
it('supports audio repeat through the same player', async () => {
    const audio = setup({ kind: 'audio' }); range(audio); await audio.play()
    audio.currentTime = 21; audio.dispatchEvent(new Event('timeupdate')); expect(audio.currentTime).toBe(10)
})
