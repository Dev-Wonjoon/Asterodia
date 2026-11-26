import { translate, bindAttribute } from './i18n.js';
import { attachSharedVideo } from './viewer-video-renderer.js';

export const zoomSteps = [1, 1.25, 1.5, 2, 3, 4, 6, 8];
const clamp = (value, min, max) => Math.max(min, Math.min(max, value));

// Centers are stored in media coordinates, so resizing a pane preserves its subject.
export function viewportGeometry(width, height, mediaWidth, mediaHeight, state) {
    if (!(width > 0 && height > 0 && mediaWidth > 0 && mediaHeight > 0)) return null;
    const scale = Math.min(width / mediaWidth, height / mediaHeight) * state.zoom;
    const w = mediaWidth * scale, h = mediaHeight * scale;
    const visibleX = Math.min(1, width / w), visibleY = Math.min(1, height / h);
    const x = clamp(state.x, visibleX / 2, 1 - visibleX / 2);
    const y = clamp(state.y, visibleY / 2, 1 - visibleY / 2);
    return { width: w, height: h, left: width / 2 - x * w, top: height / 2 - y * h,
        x, y, visibleX, visibleY, mediaWidth, mediaHeight };
}

// A shared video has one frame callback, regardless of the number of panes/maps.
const videoWatches = new WeakMap();
function watchVideo(video, paint) {
    let watch = videoWatches.get(video);
    if (!watch) {
        const callbacks = new Set();
        let frame = null, frameIsVideo = false;
        const cancel = () => {
            if (frame !== null) {
                if (frameIsVideo) video.cancelVideoFrameCallback?.(frame);
                else cancelAnimationFrame(frame);
                frame = null;
            }
        };
        const notify = () => { for (const callback of callbacks) callback(); };
        const schedule = () => {
            if (frame !== null || video.paused || video.ended || !callbacks.size) return;
            frameIsVideo = typeof video.requestVideoFrameCallback === 'function';
            const tick = () => { frame = null; notify(); schedule(); };
            frame = frameIsVideo ? video.requestVideoFrameCallback(tick) : requestAnimationFrame(tick);
        };
        const update = () => { cancel(); notify(); schedule(); };
        const events = ['loadeddata', 'seeked', 'play', 'playing', 'pause', 'ended', 'emptied'];
        for (const event of events) video.addEventListener(event, update);
        watch = { callbacks, schedule, dispose() { cancel(); for (const event of events) video.removeEventListener(event, update); videoWatches.delete(video); } };
        videoWatches.set(video, watch);
    }
    watch.callbacks.add(paint); paint(); watch.schedule();
    return () => { watch.callbacks.delete(paint); if (!watch.callbacks.size) watch.dispose(); };
}

export function mountViewerViewport(host, { source, state, sharedVideo = false, selected = true, poster, width, height, onChange = () => {}, onError = () => {} }) {
    const doc = host.ownerDocument, video = source.tagName === 'VIDEO';
    const nativeVideo = video && !sharedVideo;
    const surface = video && sharedVideo ? doc.createElement('canvas') : source;
    // Move the existing decoder into the viewport; return it before player cleanup.
    const anchor = nativeVideo && source.parentNode ? doc.createComment('viewer-video') : null;
    const originalStyle = nativeVideo ? source.getAttribute('style') : null;
    if (anchor) source.before(anchor);
    surface.classList.add(nativeVideo ? 'sbv-native-video' : video ? 'sbv-canvas' : 'sbv-image');
    if (video && !nativeVideo) { surface.setAttribute('role', 'img'); bindAttribute(surface, 'aria-label', () => source.getAttribute('aria-label') || translate('media.videoTitle')); }
    const map = doc.createElement('div'); map.className = 'sbv-minimap';
    const miniature = video ? doc.createElement('canvas') : doc.createElement('img');
    miniature.setAttribute('aria-hidden', 'true'); if (!video) miniature.alt = '';
    const rect = doc.createElement('button'); rect.type = 'button'; rect.className = 'sbv-map-rect'; rect.dataset.minimapControl = 'true';
    bindAttribute(rect, 'aria-label', () => translate('viewer.moveVisibleArea'));
    map.append(miniature, rect); host.append(surface, map);
    let active = selected, geometry, destroyed = false, drag = null, stopFrames = null, mapTimer = null;
    let viewportWidth = 0, viewportHeight = 0, mapWidth = 0, mapHeight = 0, lastMapPaint = -Infinity;
    const mapInterval = 200;
    let posterImage;
    let fallbackContext = null;
    const renderer = video && sharedVideo ? attachSharedVideo(source, host, () => paint(true)) : null;
    if (video && poster) { posterImage = doc.createElement('img'); posterImage.onload = () => render(); posterImage.src = poster; }
    function canvasContext(canvas, w, h) {
        const ratio = Math.min(2, globalThis.devicePixelRatio || 1);
        const cw = Math.max(1, Math.round(w * ratio)), ch = Math.max(1, Math.round(h * ratio));
        if (canvas.width !== cw) canvas.width = cw;
        if (canvas.height !== ch) canvas.height = ch;
        const context = canvas.getContext('2d');
        context?.setTransform(ratio, 0, 0, ratio, 0, 0);
        context?.clearRect(0, 0, w, h);
        return context;
    }
    function paint(forceMap = false) {
        if (destroyed || !video || !geometry) return;
        const g = geometry;
        const frame = source.readyState >= 2 ? source : posterImage?.complete && posterImage.naturalWidth ? posterImage : null;
        if (!nativeVideo) {
            if (renderer?.paint(frame)) {
                if (fallbackContext) { fallbackContext.clearRect(0, 0, surface.width, surface.height); fallbackContext = null; }
            } else {
                const context = fallbackContext = canvasContext(surface, viewportWidth, viewportHeight);
                if (frame && context) { try { context.drawImage(frame, g.left, g.top, g.width, g.height); } catch { /* A decoded frame may disappear during seeking. */ } }
            }
        }
        const now = performance.now();
        if (!map.hidden && (forceMap || now - lastMapPaint >= mapInterval)) {
            lastMapPaint = now;
            const context = canvasContext(miniature, mapWidth, mapHeight);
            if (frame && context) { try { context.drawImage(frame, 0, 0, mapWidth, mapHeight); } catch {} }
        }
    }
    function syncFrames() {
        if (video && !nativeVideo && geometry && !stopFrames) stopFrames = watchVideo(source, () => paint());
        else if ((!geometry || destroyed) && stopFrames) { stopFrames(); stopFrames = null; }
        // Native video is composed by the browser. Only a visible minimap needs JS paints.
        clearTimeout(mapTimer); mapTimer = null;
        if (nativeVideo && !destroyed && geometry && !map.hidden && !source.paused && !source.ended) {
            mapTimer = setTimeout(() => { mapTimer = null; paint(); syncFrames(); }, mapInterval);
        }
    }
    function render() {
        if (destroyed) return;
        // Let CSS fit images immediately when the pane changes size (rotation,
        // mobile browser chrome, or wrapped controls), even before resize callbacks.
        const fitImage = !video && state.zoom === 1;
        if (fitImage) Object.assign(surface.style, { width: '100%', height: '100%', left: '0px', top: '0px' });
        const w = viewportWidth = host.clientWidth, h = viewportHeight = host.clientHeight;
        const mw = (video ? source.videoWidth : source.naturalWidth) || width || posterImage?.naturalWidth;
        const mh = (video ? source.videoHeight : source.naturalHeight) || height || posterImage?.naturalHeight;
        geometry = viewportGeometry(w, h, mw, mh, state);
        renderer?.update(geometry, w, h);
        map.hidden = !active || state.zoom <= 1 || !geometry;
        if (!geometry) { syncFrames(); return; }
        const g = geometry; state.x = g.x; state.y = g.y;
        if (nativeVideo) {
            Object.assign(surface.style, { width: g.width + 'px', height: g.height + 'px',
                left: '0px', top: '0px', transform: `translate(${g.left}px, ${g.top}px)` });
        } else if (!video) {
            if (!fitImage) Object.assign(surface.style, { width: g.width + 'px', height: g.height + 'px', left: g.left + 'px', top: g.top + 'px' });
            if (miniature.getAttribute('src') !== source.getAttribute('src')) miniature.src = source.src;
        }
        if (!map.hidden) {
            const fit = Math.min(160 / mw, 110 / mh, Math.max(1, w * .3) / mw);
            mapWidth = Math.max(1, mw * fit); mapHeight = Math.max(1, mh * fit);
            map.style.width = mapWidth + 'px'; map.style.height = mapHeight + 'px';
            Object.assign(rect.style, { width: g.visibleX * 100 + '%', height: g.visibleY * 100 + '%', left: (g.x - g.visibleX / 2) * 100 + '%', top: (g.y - g.visibleY / 2) * 100 + '%' });
        }
        paint(true); syncFrames();
    }
    const loaded = () => render();
    source.addEventListener('load', loaded); source.addEventListener('error', onError);
    const dimensions = ['loadedmetadata', 'resize', 'emptied'];
    const playback = ['loadeddata', 'seeked', 'play', 'playing', 'pause', 'ended'];
    const playbackChanged = () => { paint(true); syncFrames(); };
    if (video) {
        for (const event of dimensions) source.addEventListener(event, loaded);
        if (nativeVideo) for (const event of playback) source.addEventListener(event, playbackChanged);
    }
    rect.onpointerdown = event => {
        if (event.button !== 0 || !geometry) return;
        event.preventDefault(); event.stopPropagation();
        const bounds = map.getBoundingClientRect();
        drag = { id: event.pointerId, px: event.clientX, py: event.clientY, x: state.x, y: state.y, width: bounds.width, height: bounds.height };
        rect.setPointerCapture?.(event.pointerId);
    };
    rect.onpointermove = event => {
        if (!drag || drag.id !== event.pointerId) return;
        state.x = drag.x + (event.clientX - drag.px) / Math.max(1, drag.width);
        state.y = drag.y + (event.clientY - drag.py) / Math.max(1, drag.height);
        render(); onChange();
    };
    const endDrag = event => {
        if (!drag || event.pointerId !== drag.id) return;
        const id = drag.id; drag = null;
        if (rect.hasPointerCapture?.(id)) rect.releasePointerCapture(id);
        if (!destroyed) host.focus({ preventScroll: true });
    };
    rect.onpointerup = rect.onpointercancel = rect.onlostpointercapture = endDrag;
    rect.onkeydown = event => {
        const deltas = { ArrowLeft: [-.05, 0], ArrowRight: [.05, 0], ArrowUp: [0, -.05], ArrowDown: [0, .05] };
        if (event.ctrlKey || event.metaKey || event.altKey || !deltas[event.key]) return;
        event.preventDefault(); event.stopPropagation();
        state.x += deltas[event.key][0]; state.y += deltas[event.key][1]; render(); onChange();
    };
    const observer = typeof ResizeObserver === 'function' ? new ResizeObserver(render) : null;
    observer?.observe(host); globalThis.addEventListener?.('resize', render);
    render();
    return { surface, render, select(value) { if (active !== value) { active = value; render(); } },
        getPerformance: () => renderer?.getPerformance() || { backend: nativeVideo ? 'native' : video ? 'canvas2d' : 'image' },
        zoom(direction) {
            state.zoom = direction === 0 ? 1 : zoomSteps[clamp(zoomSteps.indexOf(state.zoom) + direction, 0, zoomSteps.length - 1)];
            if (direction === 0) state.x = state.y = .5;
            render(); onChange();
        },
        destroy() {
            destroyed = true; drag = null; stopFrames?.(); clearTimeout(mapTimer); observer?.disconnect(); globalThis.removeEventListener?.('resize', render);
            renderer?.destroy();
            source.removeEventListener('load', loaded); source.removeEventListener('error', onError);
            if (video) {
                for (const event of dimensions) source.removeEventListener(event, loaded);
                if (nativeVideo) for (const event of playback) source.removeEventListener(event, playbackChanged);
            }
            if (posterImage) { posterImage.onload = null; posterImage.removeAttribute('src'); }
            rect.onpointerdown = rect.onpointermove = rect.onpointerup = rect.onpointercancel = rect.onlostpointercapture = rect.onkeydown = null;
            if (nativeVideo) {
                surface.classList.remove('sbv-native-video');
                if (originalStyle === null) surface.removeAttribute('style'); else surface.setAttribute('style', originalStyle);
                if (anchor?.parentNode) anchor.replaceWith(source); else surface.remove();
            } else surface.remove();
            map.remove();
        } };
}
