// @vitest-environment happy-dom
import { afterEach, expect, it, vi } from 'vitest'
import { observePlayback } from '../../internal/webserver/web/player-performance.js'

afterEach(() => vi.restoreAllMocks())

it('samples displayed frames, buffered time and wait durations without polling', () => {
    let now = 0, total = 100, dropped = 2
    vi.spyOn(performance, 'now').mockImplementation(() => now)
    const video = document.createElement('video'); video.currentTime = 10
    Object.defineProperty(video, 'buffered', { value: { length: 1, start: () => 5, end: () => 14 } })
    video.getVideoPlaybackQuality = () => ({ totalVideoFrames: total, droppedVideoFrames: dropped } as VideoPlaybackQuality)
    const metrics = observePlayback(video)
    expect(metrics.sample()).toMatchObject({ presentedFPS: null, droppedRatio: .02, bufferedSeconds: 4 })
    now = 100; video.dispatchEvent(new Event('waiting')); video.dispatchEvent(new Event('stalled'))
    now = 350; expect(metrics.sample().waitingMilliseconds).toBe(250)
    video.dispatchEvent(new Event('playing')); now = 1350; total = 160; dropped = 3
    expect(metrics.sample()).toMatchObject({ presentedFPS: 59, sampleFrames: 60, sampleDroppedFrames: 1, sampleDroppedRatio: 1/60,
        waitingMilliseconds: 250, waitingEvents: 1, stalledEvents: 1 })
    video.dispatchEvent(new Event('emptied')); expect(metrics.sample().presentedFPS).toBeNull()
    metrics.destroy(); video.dispatchEvent(new Event('waiting')); expect(metrics.sample().waitingEvents).toBe(0)
})

it('reports unsupported frame counters as unavailable and exposes server timing without file URLs', () => {
    const video = document.createElement('video'); video.src = '/media?path=private'
    Object.defineProperty(video, 'getVideoPlaybackQuality', { value: undefined })
    vi.spyOn(performance, 'getEntriesByName').mockReturnValue([{ entryType: 'resource', duration: 12, transferSize: 4096,
        serverTiming: [{ name: 'library', duration: .1 }, { name: 'file', duration: .2 }] } as any])
    const metrics = observePlayback(video), sample = metrics.sample()
    expect(sample).toMatchObject({ totalFrames: null, droppedFrames: null, presentedFPS: null,
        lastRequest: { duration: 12, transferBytes: 4096, serverTiming: [{ name: 'library', duration: .1 }, { name: 'file', duration: .2 }] } })
    expect(JSON.stringify(sample)).not.toContain('private'); metrics.destroy()
})
