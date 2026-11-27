import { readPreference } from './preferences.js'
import { translate, bindText, bindAttribute, localizeWidget } from './i18n.js'
import { matchShortcut, isComposing } from './shortcuts.js';
import { observePlayback } from './player-performance.js';
const preferenceKey = 'asterodia:player-seek-seconds:v1';
const audioPreferenceKey = 'asterodia:player-audio:v1';
const clamp = (n, min, max) => Math.max(min, Math.min(max, n));
export function readSeekSeconds() {
    try { const n = Number(readPreference(preferenceKey)); return Number.isInteger(n) && n >= 1 && n <= 300 ? n : 10; }
    catch { return 10; }
}
function readAudioPreference() {
    try {
        const saved = JSON.parse(readPreference(audioPreferenceKey));
        if (saved && typeof saved.volume === 'number' && Number.isFinite(saved.volume) && saved.volume >= 0 && saved.volume <= 1 && typeof saved.muted === 'boolean') return saved;
    } catch {}
    return { volume: 1, muted: false };
}
export function formatTime(value) {
    if (!Number.isFinite(value) || value < 0) return '0:00';
    const n = Math.floor(value), hours = Math.floor(n / 3600);
    return (hours ? hours + ':' : '') + (hours ? String(Math.floor(n / 60) % 60).padStart(2, '0') : Math.floor(n / 60)) + ':' + String(n % 60).padStart(2, '0');
}

// Shared by the desktop WebView and the browser. Owns all listeners and timers.
export function mountVideoPlayer(host, { src, kind = 'video', title = translate("media.videoTitle"), poster, onError, keyboardSeek = true, externalScreen = false, fullscreenAction } = {}) {
    const doc = host.ownerDocument, root = doc.createElement('section');
    root.className = 'sb-player' + (kind === 'audio' ? ' sbp-audio' : ''); bindAttribute(root, 'aria-label', () => title + translate("player.player")); root.tabIndex = 0;
    const node = (tag, className, text) => { const el = doc.createElement(tag); if (className) el.className = className; if (typeof text === 'function') bindText(el, text); else if (text) bindText(el, () => text); return el; };
    if (externalScreen) root.classList.add('sbp-external-screen');
    const listeners = new Set();
    const on = (el, event, fn, options) => { el.addEventListener(event, fn, options); const remove = () => { el.removeEventListener(event, fn, options); listeners.delete(remove); }; listeners.add(remove); return remove; };
    const button = (label, text, action, parent) => { const el = node('button', '', text); el.type = 'button'; const read = typeof label === 'function' ? label : () => label; bindAttribute(el, 'aria-label', read); bindAttribute(el, 'title', read); on(el, 'click', action); parent.append(el); return el; };
    const icons = {
        play: '<path d="M8 5v14l11-7z" fill="currentColor" stroke="none"/>',
        pause: '<path d="M7 5h4v14H7zm6 0h4v14h-4z" fill="currentColor" stroke="none"/>',
        volume: '<path d="M3 9h4l5-4v14l-5-4H3z"/><path d="M16 8q5 4 0 8m3-11q8 7 0 14"/>',
        muted: '<path d="M3 9h4l5-4v14l-5-4H3z"/><path d="m16 9 6 6m0-6-6 6"/>',
        settings: '<circle cx="12" cy="12" r="3"/><circle cx="12" cy="12" r="8"/><path d="M12 1v3m0 16v3M1 12h3m16 0h3M4 4l2 2m12 12 2 2M4 20l2-2M18 6l2-2"/>',
        fullscreen: '<path d="M3 9V3h6m6 0h6v6m0 6v6h-6m-6 0H3v-6"/>',
    };
    const icon = (el, name) => { el.innerHTML = '<svg viewBox="0 0 24 24" width="20" height="20" aria-hidden="true" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round">' + icons[name] + '</svg>'; };
    const screen = node('div', 'sbp-screen'), video = node(kind === 'audio' ? 'audio' : 'video');
    const performanceStats = observePlayback(video);
    const surface = kind === 'audio' ? node('div', 'sbp-artwork', '♫') : video;
    if (kind === 'audio') {
        bindAttribute(surface, 'aria-label', () => title);
        if (poster) {
            const artwork = node('img'); artwork.alt = title; artwork.src = poster;
            on(artwork, 'error', () => artwork.remove(), { once: true }); surface.append(artwork);
        }
    }
    const audioPreference = readAudioPreference();
    video.volume = audioPreference.volume; video.muted = audioPreference.muted;
    video.src = src || ''; video.preload = 'none'; video.playsInline = true; video.setAttribute('aria-label', title);
    if (poster && kind !== 'audio') video.poster = poster;
    const feedback = node('div', 'sbp-feedback'); feedback.setAttribute('role', 'status'); feedback.setAttribute('aria-live', 'polite');
    const message = node('p', 'sbp-message'); message.hidden = true;
    const overlay = node('div', 'sbp-overlay'), seek = node('input', 'sbp-timeline');
    seek.type = 'range'; seek.min = '0'; seek.max = '0'; seek.step = '0.1'; seek.value = '0'; bindAttribute(seek, 'aria-label', () => translate("player.playbackPosition"));
    const controls = node('div', 'sbp-controls');
    let clickTimer, hideTimer, feedbackTimer, lastTap = null, touchStart = null, lastTouch = -Infinity, seconds = readSeekSeconds(), destroyed = false;
    function showFeedback(text) { bindText(feedback, () => text); feedback.classList.add('is-visible'); clearTimeout(feedbackTimer); feedbackTimer = setTimeout(() => feedback.classList.remove('is-visible'), 900); }
    function wake() { root.classList.add('is-awake'); clearTimeout(hideTimer); if (!video.paused) hideTimer = setTimeout(() => root.classList.remove('is-awake'), 2500); }
    function togglePlay() {
        if (video.paused) { const pending = video.play(); pending?.catch(() => { if (!destroyed) { bindText(message, () => translate("player.couldNotStartPlaybackTryAgain")); message.hidden = false; } }); }
        else video.pause();
        wake();
    }
    function jump(delta) {
        if (!Number.isFinite(video.duration) || video.duration <= 0) { showFeedback(translate("player.startPlaybackBeforeSeeking")); return; }
        video.currentTime = clamp(video.currentTime + delta, 0, video.duration);
        showFeedback((delta < 0 ? '↶ ' : '↷ ') + Math.abs(delta) + translate("editor.s")); sync(); wake();
    }
    const play = button(() => (translate("player.play")), '▶', togglePlay, controls);
    const mute = button(() => (translate("player.mute")), '♪', () => { video.muted = !video.muted; audioChanged(); }, controls);
    const volume = node('input', 'sbp-volume'); volume.type = 'range'; volume.min = '0'; volume.max = '1'; volume.step = '0.05'; volume.value = '1'; bindAttribute(volume, 'aria-label', () => translate("player.volume"));
    on(volume, 'input', () => { video.volume = Number(volume.value); video.muted = video.volume === 0; audioChanged(); }); controls.append(volume);
    const time = node('output', 'sbp-time', '0:00 / 0:00'); controls.append(time);
    const rate = node('select', 'sbp-rate'); bindAttribute(rate, 'aria-label', () => translate("player.playbackSpeed"));
    for (const value of [.25, .5, .75, 1, 1.25, 1.5, 1.75, 2]) { const option = node('option', '', value + '×'); option.value = String(value); option.selected = value === 1; rate.append(option); }
    on(rate, 'change', () => { video.playbackRate = Number(rate.value); }); controls.append(rate);
    const settings = node('div', 'sbp-settings'); settings.hidden = true;
    const settingLabel = node('label', '', () => (translate("player.doubleClickTapSeekSeconds"))); const interval = node('input'); interval.type = 'number'; interval.min = '1'; interval.max = '300'; interval.step = '1'; interval.value = String(seconds);
    on(interval, 'change', () => { const n = Number(interval.value); if (Number.isInteger(n) && n >= 1 && n <= 300) { seconds = n; try { localStorage.setItem(preferenceKey, String(n)); } catch {} } interval.value = String(seconds); });
    settingLabel.append(interval); settings.append(settingLabel, node('small', '', () => (translate("player.leftSeeksBackwardRightSeeksForward"))));
    const settingsButton = button(() => (translate("player.playerSettings")), '⚙', () => { settings.hidden = !settings.hidden; settingsButton.setAttribute('aria-expanded', String(!settings.hidden)); wake(); if (!settings.hidden) interval.focus(); }, controls);
    icon(settingsButton, 'settings');
    settingsButton.setAttribute('aria-expanded', 'false');
    let expandedDialog = null;
    let previousOverflow = '';
    function leaveExpanded() {
        if (!expandedDialog) return;
        const dialog = expandedDialog; expandedDialog = null;
        root.classList.remove('is-expanded'); host.append(root); dialog.remove();
        doc.body.style.overflow = previousOverflow;
        bindAttribute(fullscreen, 'aria-label', () => translate("player.fullscreen")); if (!destroyed) fullscreen.focus({ preventScroll: true }); wake();
    }
    function expandInWindow() {
        const dialog = node('dialog', 'sbp-fullscreen-dialog');
        bindAttribute(dialog, 'aria-label', () => title + translate("player.suffix.fullscreen")); dialog.setAttribute('data-detail-trigger', '');
        previousOverflow = doc.body.style.overflow; doc.body.style.overflow = 'hidden';
        expandedDialog = dialog; doc.body.append(dialog); dialog.append(root); root.classList.add('is-expanded');
        on(dialog, 'close', leaveExpanded);
        dialog.showModal(); bindAttribute(fullscreen, 'aria-label', () => translate("player.exitFullscreen")); root.focus(); wake();
    }
    const fullscreen = button(() => (translate("player.fullscreen")), '⛶', async () => {
        if (fullscreenAction) { fullscreenAction(); return; }
        if (expandedDialog) { expandedDialog.close(); return; }
        try {
            if (doc.fullscreenElement === root) await doc.exitFullscreen();
            else if (root.requestFullscreen) await root.requestFullscreen();
            else expandInWindow();
        } catch { if (!destroyed) expandInWindow(); }
    }, controls);
    icon(fullscreen, 'fullscreen');
    const jumps = node('div', 'sbp-jumps'); bindAttribute(jumps, 'aria-label', () => translate("player.seek"));
    for (const delta of [-300, -60, -30, -10, 10, 30, 60, 300]) {
        const text = () => (delta < 0 ? '-' : '+') + (Math.abs(delta) >= 60 ? Math.abs(delta) / 60 + translate("player.min") : Math.abs(delta) + translate("editor.s"));
        const b = button(() => text() + translate("player.suffix.seek"), text, () => jump(delta), jumps); b.dataset.seconds = String(delta);
    }
    const error = () => { bindText(message, () => translate("player.cannotPlayInTheCurrentEnvironmentCheckTheFile")); message.hidden = false; message.setAttribute('role', 'alert'); onError?.(); wake(); };
    const timeText = () => formatTime(video.currentTime) + ' / ' + formatTime(video.duration);
    bindText(time, timeText);
    let lastDuration, lastPaused, lastMuted, lastVolume, lastRate;
    function syncTime() {
        const position = String(video.currentTime || 0), label = timeText();
        if (seek.value !== position) seek.value = position;
        if (seek.getAttribute('aria-valuetext') !== label) seek.setAttribute('aria-valuetext', label);
        if (time.firstChild?.data !== label) time.firstChild.data = label;
    }
    function sync() {
        const duration = Number.isFinite(video.duration) && video.duration > 0 ? video.duration : 0;
        if (duration !== lastDuration) {
            lastDuration = duration; seek.disabled = !duration; seek.max = String(duration);
            for (const b of jumps.children) b.disabled = !duration;
        }
        if (video.paused !== lastPaused) {
            lastPaused = video.paused; icon(play, video.paused ? 'play' : 'pause');
            bindAttribute(play, 'aria-label', () => video.paused ? translate("player.play") : translate("player.pause"));
            bindAttribute(play, 'title', () => video.paused ? translate("player.play") : translate("player.pause"));
            root.classList.toggle('is-paused', video.paused);
        }
        if (video.muted !== lastMuted || video.volume !== lastVolume) {
            lastMuted = video.muted; lastVolume = video.volume;
            icon(mute, video.muted || !video.volume ? 'muted' : 'volume');
            bindAttribute(mute, 'aria-label', () => video.muted ? translate("player.unmute") : translate("player.mute"));
            volume.value = String(video.muted ? 0 : video.volume);
        }
        if (video.playbackRate !== lastRate) { lastRate = video.playbackRate; rate.value = String(video.playbackRate); }
        syncTime();
    }
    function audioChanged() {
        try { localStorage.setItem(audioPreferenceKey, JSON.stringify({ volume: video.volume, muted: video.muted })); } catch {}
        sync();
    }
    function attachSurface(target, { focus = () => root.focus({ preventScroll: true }), bounds = () => target.getBoundingClientRect() } = {}) {
        const owned = [];
        const listen = (event, fn) => owned.push(on(target, event, fn));
        function sideAt(x) { const r = bounds(); return x < r.left + r.width / 2 ? -1 : 1; }
        listen('click', event => {
            if (performance.now() - lastTouch < 700 || event.detail > 1) return;
            focus();
            clearTimeout(clickTimer); clickTimer = setTimeout(togglePlay, 500);
        });
        listen('dblclick', event => { event.preventDefault(); if (performance.now() - lastTouch < 700) return; focus(); clearTimeout(clickTimer); jump(sideAt(event.clientX) * seconds); });
        // Pointer gestures avoid synthetic click/dblclick duplication on touch screens.
        listen('pointerdown', event => { if (event.pointerType === 'touch' && event.isPrimary !== false) { lastTouch = performance.now(); touchStart = { id: event.pointerId, x: event.clientX, y: event.clientY, at: lastTouch }; } });
        listen('pointerup', event => {
            if (event.pointerType !== 'touch' || !touchStart || touchStart.id !== event.pointerId) return;
            const now = performance.now(), start = touchStart; touchStart = null; lastTouch = now;
            if (now - start.at > 350 || Math.hypot(event.clientX - start.x, event.clientY - start.y) > 20) { lastTap = null; return; }
            const side = sideAt(event.clientX);
            focus();
            if (lastTap && now - lastTap.at <= 300 && lastTap.side === side) { clearTimeout(clickTimer); lastTap = null; jump(side * seconds); }
            else { clearTimeout(clickTimer); lastTap = { at: now, side }; clickTimer = setTimeout(() => { lastTap = null; togglePlay(); }, 300); }
        });
        listen('pointercancel', () => { touchStart = null; lastTap = null; clearTimeout(clickTimer); });
        return () => { owned.forEach(remove => remove()); touchStart = null; lastTap = null; clearTimeout(clickTimer); };
    }
    if (!externalScreen) attachSurface(surface);
    on(seek, 'input', () => { if (Number.isFinite(video.duration)) video.currentTime = clamp(Number(seek.value), 0, video.duration); sync(); });
    on(seek, 'pointermove', event => { const r = seek.getBoundingClientRect(); seek.title = formatTime(clamp((event.clientX - r.left) / Math.max(1, r.width), 0, 1) * Number(seek.max)); });
    for (const event of ['loadedmetadata', 'durationchange', 'ratechange', 'ended', 'emptied']) on(video, event, sync);
    on(video, 'timeupdate', syncTime);
    on(video, 'volumechange', audioChanged);
    on(video, 'play', () => { message.hidden = true; sync(); wake(); }); on(video, 'pause', () => { sync(); wake(); });
    on(video, 'waiting', () => { bindText(message, () => translate("gallery.loading")); message.hidden = false; }); on(video, 'playing', () => { message.hidden = true; });
    on(video, 'error', error); on(root, 'pointermove', wake); on(root, 'focusin', wake);
    on(doc, 'fullscreenchange', () => { bindAttribute(fullscreen, 'aria-label', () => doc.fullscreenElement === root ? translate("player.exitFullscreen") : translate("player.fullscreen")); });
    on(root, 'keydown', event => {
        if (event.defaultPrevented || isComposing(event)) return;
        if (event.key === 'Escape' && !settings.hidden) { event.preventDefault(); event.stopPropagation(); settings.hidden = true; settingsButton.setAttribute('aria-expanded', 'false'); settingsButton.focus(); return; }
        if (event.target !== root && event.target !== video) return;
        const item = matchShortcut(event, 'player');
        if (item) { event.preventDefault(); event.stopPropagation(); keyboardAction(item.id); }
        else if (keyboardSeek && !event.ctrlKey && !event.altKey && !event.metaKey && ['ArrowLeft', 'ArrowRight'].includes(event.key)) {
            event.preventDefault(); event.stopPropagation(); jump(event.key === 'ArrowLeft' ? -seconds : seconds);
        }
    });
    function keyboardAction(id) {
        if (id === 'player.play' || id === 'player.playAlternative') togglePlay();
        if (id === 'player.backward' || id === 'player.forward') jump(id === 'player.backward' ? -5 : 5);
        if (id === 'player.mute') { video.muted = !video.muted; audioChanged(); }
        if (id === 'player.fullscreen') fullscreen.click();
    }
    overlay.append(seek, controls); if (surface !== video) screen.append(surface); screen.append(video, feedback, message, overlay, settings); root.append(screen, jumps); host.append(root); sync(); wake();
    const stopLanguage = localizeWidget(root);
    return { video, root, keyboardAction, attachSurface,
        getPerformance: () => performanceStats.sample(),
        cancelGestures() { clearTimeout(clickTimer); lastTap = null; touchStart = null; },
        destroy() { stopLanguage(); performanceStats.destroy(); destroyed = true; leaveExpanded(); clearTimeout(clickTimer); clearTimeout(hideTimer); clearTimeout(feedbackTimer); listeners.forEach(remove => remove()); if (doc.fullscreenElement === root) doc.exitFullscreen()?.catch(() => {}); video.pause(); video.removeAttribute('src'); video.load(); root.remove(); } };
}
