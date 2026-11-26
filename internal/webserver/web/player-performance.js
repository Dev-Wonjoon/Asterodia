// Sampling is opt-in: no polling, frame callbacks, or telemetry leave the app.
export function observePlayback(video) {
    let waiting = 0, stalls = 0, waited = 0, waitingAt = null, previous = null;
    const finishWait = () => { if (waitingAt !== null) { waited += performance.now() - waitingAt; waitingAt = null; } };
    const startWait = () => { waiting++; if (waitingAt === null) waitingAt = performance.now(); };
    const stall = () => { stalls++; };
    const reset = () => { waiting = stalls = waited = 0; waitingAt = previous = null; };
    const events = { waiting: startWait, stalled: stall, playing: finishWait, pause: finishWait, ended: finishWait, emptied: reset };
    for (const [event, handler] of Object.entries(events)) video.addEventListener(event, handler);
    return {
        sample() {
            const now = performance.now(), quality = video.getVideoPlaybackQuality?.();
            const total = quality?.totalVideoFrames ?? null, dropped = quality?.droppedVideoFrames ?? null;
            const elapsed = previous ? (now - previous.now) / 1000 : 0;
            const delta = previous && total !== null && previous.total !== null && total >= previous.total ? total - previous.total : null;
            const lost = previous && dropped !== null && previous.dropped !== null && dropped >= previous.dropped ? dropped - previous.dropped : null;
            let bufferedAhead = 0;
            for (let i = 0; i < video.buffered.length; i++) {
                if (video.buffered.start(i) <= video.currentTime && video.currentTime <= video.buffered.end(i)) {
                    bufferedAhead = video.buffered.end(i) - video.currentTime; break;
                }
            }
            const requests = (performance.getEntriesByName?.(video.currentSrc || video.src) || []).filter(entry => entry.entryType === 'resource');
            const latest = requests.at(-1);
            const sample = {
                paused: video.paused, seeking: video.seeking, readyState: video.readyState,
                width: video.videoWidth || 0, height: video.videoHeight || 0, playbackRate: video.playbackRate,
                totalFrames: total, droppedFrames: dropped, droppedRatio: total > 0 && dropped !== null ? dropped / total : null,
                sampleFrames: delta, sampleDroppedFrames: lost, sampleDroppedRatio: delta > 0 && lost !== null ? lost / delta : null,
                sampleSeconds: elapsed, presentedFPS: elapsed > 0 && delta !== null && lost !== null ? (delta - lost) / elapsed : null,
                bufferedSeconds: bufferedAhead, waitingEvents: waiting, stalledEvents: stalls,
                waitingMilliseconds: waited + (waitingAt === null ? 0 : now - waitingAt),
                lastRequest: latest ? { duration: latest.duration, transferBytes: latest.transferSize,
                    serverTiming: (latest.serverTiming || []).map(({ name, duration }) => ({ name, duration })) } : null,
            };
            previous = { now, total, dropped };
            return sample;
        },
        destroy() { for (const [event, handler] of Object.entries(events)) video.removeEventListener(event, handler); },
    };
}
