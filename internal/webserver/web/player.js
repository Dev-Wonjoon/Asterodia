const preferenceKey = 'storagebucket:player-seek-seconds:v1';
const clamp = (n, min, max) => Math.max(min, Math.min(max, n));
export function readSeekSeconds() {
    try { const n = Number(localStorage.getItem(preferenceKey)); return Number.isInteger(n) && n >= 1 && n <= 300 ? n : 10; }
    catch { return 10; }
}
export function formatTime(value) {
    if (!Number.isFinite(value) || value < 0) return '0:00';
    const n = Math.floor(value), hours = Math.floor(n / 3600);
    return (hours ? hours + ':' : '') + (hours ? String(Math.floor(n / 60) % 60).padStart(2, '0') : Math.floor(n / 60)) + ':' + String(n % 60).padStart(2, '0');
}

// Shared by the desktop WebView and the browser. Owns all listeners and timers.
export function mountVideoPlayer(host, { src, title = '동영상', poster, onError, keyboardSeek = true } = {}) {
    const doc = host.ownerDocument, root = doc.createElement('section');
    root.className = 'sb-player'; root.setAttribute('aria-label', title + ' 플레이어'); root.tabIndex = 0;
    const node = (tag, className, text) => { const el = doc.createElement(tag); if (className) el.className = className; if (text) el.textContent = text; return el; };
    const listeners = [];
    const on = (el, event, fn, options) => { el.addEventListener(event, fn, options); listeners.push(() => el.removeEventListener(event, fn, options)); };
    const button = (label, text, action, parent) => { const el = node('button', '', text); el.type = 'button'; el.setAttribute('aria-label', label); el.title = label; on(el, 'click', action); parent.append(el); return el; };
    const icons = {
        play: '<path d="M8 5v14l11-7z" fill="currentColor" stroke="none"/>',
        pause: '<path d="M7 5h4v14H7zm6 0h4v14h-4z" fill="currentColor" stroke="none"/>',
        volume: '<path d="M3 9h4l5-4v14l-5-4H3z"/><path d="M16 8q5 4 0 8m3-11q8 7 0 14"/>',
        muted: '<path d="M3 9h4l5-4v14l-5-4H3z"/><path d="m16 9 6 6m0-6-6 6"/>',
        settings: '<circle cx="12" cy="12" r="3"/><circle cx="12" cy="12" r="8"/><path d="M12 1v3m0 16v3M1 12h3m16 0h3M4 4l2 2m12 12 2 2M4 20l2-2M18 6l2-2"/>',
        fullscreen: '<path d="M3 9V3h6m6 0h6v6m0 6v6h-6m-6 0H3v-6"/>',
    };
    const icon = (el, name) => { el.innerHTML = '<svg viewBox="0 0 24 24" width="20" height="20" aria-hidden="true" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round">' + icons[name] + '</svg>'; };
    const screen = node('div', 'sbp-screen'), video = node('video');
    video.src = src || ''; video.preload = 'none'; video.playsInline = true; video.setAttribute('aria-label', title);
    if (poster) video.poster = poster;
    const feedback = node('div', 'sbp-feedback'); feedback.setAttribute('role', 'status'); feedback.setAttribute('aria-live', 'polite');
    const message = node('p', 'sbp-message'); message.hidden = true;
    const overlay = node('div', 'sbp-overlay'), seek = node('input', 'sbp-timeline');
    seek.type = 'range'; seek.min = '0'; seek.max = '0'; seek.step = '0.1'; seek.value = '0'; seek.setAttribute('aria-label', '재생 위치');
    const controls = node('div', 'sbp-controls');
    let clickTimer, hideTimer, feedbackTimer, lastTap = null, touchStart = null, lastTouch = -Infinity, seconds = readSeekSeconds(), destroyed = false;
    function showFeedback(text) { feedback.textContent = text; feedback.classList.add('is-visible'); clearTimeout(feedbackTimer); feedbackTimer = setTimeout(() => feedback.classList.remove('is-visible'), 900); }
    function wake() { root.classList.add('is-awake'); clearTimeout(hideTimer); if (!video.paused) hideTimer = setTimeout(() => root.classList.remove('is-awake'), 2500); }
    function togglePlay() {
        if (video.paused) { const pending = video.play(); pending?.catch(() => { if (!destroyed) { message.textContent = '재생을 시작하지 못했습니다. 다시 시도해 주세요.'; message.hidden = false; } }); }
        else video.pause();
        wake();
    }
    function jump(delta) {
        if (!Number.isFinite(video.duration) || video.duration <= 0) { showFeedback('재생을 시작한 뒤 이동할 수 있습니다.'); return; }
        video.currentTime = clamp(video.currentTime + delta, 0, video.duration);
        showFeedback((delta < 0 ? '↶ ' : '↷ ') + Math.abs(delta) + '초'); sync(); wake();
    }
    const play = button('재생', '▶', togglePlay, controls);
    const mute = button('음소거', '♪', () => { video.muted = !video.muted; }, controls);
    const volume = node('input', 'sbp-volume'); volume.type = 'range'; volume.min = '0'; volume.max = '1'; volume.step = '0.05'; volume.value = '1'; volume.setAttribute('aria-label', '음량');
    on(volume, 'input', () => { video.volume = Number(volume.value); video.muted = video.volume === 0; }); controls.append(volume);
    const time = node('output', 'sbp-time', '0:00 / 0:00'); controls.append(time);
    const rate = node('select', 'sbp-rate'); rate.setAttribute('aria-label', '재생 속도');
    for (const value of [.25, .5, .75, 1, 1.25, 1.5, 1.75, 2]) { const option = node('option', '', value + '×'); option.value = String(value); option.selected = value === 1; rate.append(option); }
    on(rate, 'change', () => { video.playbackRate = Number(rate.value); }); controls.append(rate);
    const settings = node('div', 'sbp-settings'); settings.hidden = true;
    const settingLabel = node('label', '', '더블 클릭·탭 이동 (초)'); const interval = node('input'); interval.type = 'number'; interval.min = '1'; interval.max = '300'; interval.step = '1'; interval.value = String(seconds);
    on(interval, 'change', () => { const n = Number(interval.value); if (Number.isInteger(n) && n >= 1 && n <= 300) { seconds = n; try { localStorage.setItem(preferenceKey, String(n)); } catch {} } interval.value = String(seconds); });
    settingLabel.append(interval); settings.append(settingLabel, node('small', '', '왼쪽은 뒤로, 오른쪽은 앞으로 이동합니다.'));
    const settingsButton = button('플레이어 설정', '⚙', () => { settings.hidden = !settings.hidden; settingsButton.setAttribute('aria-expanded', String(!settings.hidden)); wake(); if (!settings.hidden) interval.focus(); }, controls);
    icon(settingsButton, 'settings');
    settingsButton.setAttribute('aria-expanded', 'false');
    let expandedDialog = null;
    let previousOverflow = '';
    function leaveExpanded() {
        if (!expandedDialog) return;
        const dialog = expandedDialog; expandedDialog = null;
        root.classList.remove('is-expanded'); host.append(root); dialog.remove();
        doc.body.style.overflow = previousOverflow;
        fullscreen.setAttribute('aria-label', '전체화면'); if (!destroyed) fullscreen.focus({ preventScroll: true }); wake();
    }
    function expandInWindow() {
        const dialog = node('dialog', 'sbp-fullscreen-dialog');
        dialog.setAttribute('aria-label', title + ' 전체화면'); dialog.setAttribute('data-detail-trigger', '');
        previousOverflow = doc.body.style.overflow; doc.body.style.overflow = 'hidden';
        expandedDialog = dialog; doc.body.append(dialog); dialog.append(root); root.classList.add('is-expanded');
        on(dialog, 'close', leaveExpanded);
        dialog.showModal(); fullscreen.setAttribute('aria-label', '전체화면 종료'); root.focus(); wake();
    }
    const fullscreen = button('전체화면', '⛶', async () => {
        if (expandedDialog) { expandedDialog.close(); return; }
        try {
            if (doc.fullscreenElement === root) await doc.exitFullscreen();
            else if (root.requestFullscreen) await root.requestFullscreen();
            else expandInWindow();
        } catch { if (!destroyed) expandInWindow(); }
    }, controls);
    icon(fullscreen, 'fullscreen');
    const jumps = node('div', 'sbp-jumps'); jumps.setAttribute('aria-label', '시간 이동');
    for (const delta of [-300, -60, -30, -10, 10, 30, 60, 300]) {
        const text = (delta < 0 ? '-' : '+') + (Math.abs(delta) >= 60 ? Math.abs(delta) / 60 + '분' : Math.abs(delta) + '초');
        const b = button(text + ' 이동', text, () => jump(delta), jumps); b.dataset.seconds = String(delta);
    }
    const error = () => { message.textContent = '현재 환경에서 재생할 수 없습니다. 파일 경로나 지원 코덱을 확인해 주세요.'; message.hidden = false; message.setAttribute('role', 'alert'); onError?.(); wake(); };
    function sync() {
        const duration = Number.isFinite(video.duration) && video.duration > 0 ? video.duration : 0;
        seek.disabled = !duration; seek.max = String(duration); seek.value = String(video.currentTime || 0);
        seek.setAttribute('aria-valuetext', formatTime(video.currentTime) + ' / ' + formatTime(duration));
        time.textContent = formatTime(video.currentTime) + ' / ' + formatTime(duration);
        icon(play, video.paused ? 'play' : 'pause'); play.setAttribute('aria-label', video.paused ? '재생' : '일시정지'); play.title = video.paused ? '재생' : '일시정지';
        icon(mute, video.muted || !video.volume ? 'muted' : 'volume'); mute.setAttribute('aria-label', video.muted ? '음소거 해제' : '음소거');
        volume.value = String(video.muted ? 0 : video.volume); root.classList.toggle('is-paused', video.paused);
        for (const b of jumps.children) b.disabled = !duration;
    }
    function sideAt(x) { const r = video.getBoundingClientRect(); return x < r.left + r.width / 2 ? -1 : 1; }
    on(video, 'click', event => {
        if (performance.now() - lastTouch < 700 || event.detail > 1) return;
        root.focus({ preventScroll: true });
        clearTimeout(clickTimer); clickTimer = setTimeout(togglePlay, 500);
    });
    on(video, 'dblclick', event => { event.preventDefault(); if (performance.now() - lastTouch < 700) return; clearTimeout(clickTimer); jump(sideAt(event.clientX) * seconds); });
    // Pointer gestures avoid synthetic click/dblclick duplication on touch screens.
    on(video, 'pointerdown', event => { if (event.pointerType === 'touch' && event.isPrimary !== false) { lastTouch = performance.now(); touchStart = { id: event.pointerId, x: event.clientX, y: event.clientY, at: lastTouch }; } });
    on(video, 'pointerup', event => {
        if (event.pointerType !== 'touch' || !touchStart || touchStart.id !== event.pointerId) return;
        const now = performance.now(), start = touchStart; touchStart = null; lastTouch = now;
        if (now - start.at > 350 || Math.hypot(event.clientX - start.x, event.clientY - start.y) > 20) { lastTap = null; return; }
        const side = sideAt(event.clientX);
        if (lastTap && now - lastTap.at <= 300 && lastTap.side === side) { clearTimeout(clickTimer); lastTap = null; jump(side * seconds); }
        else { clearTimeout(clickTimer); lastTap = { at: now, side }; clickTimer = setTimeout(() => { lastTap = null; togglePlay(); }, 300); }
    });
    on(video, 'pointercancel', () => { touchStart = null; lastTap = null; clearTimeout(clickTimer); });
    on(seek, 'input', () => { if (Number.isFinite(video.duration)) video.currentTime = clamp(Number(seek.value), 0, video.duration); sync(); });
    on(seek, 'pointermove', event => { const r = seek.getBoundingClientRect(); seek.title = formatTime(clamp((event.clientX - r.left) / Math.max(1, r.width), 0, 1) * Number(seek.max)); });
    for (const event of ['loadedmetadata', 'durationchange', 'timeupdate', 'volumechange', 'ratechange', 'ended', 'emptied']) on(video, event, sync);
    on(video, 'play', () => { message.hidden = true; sync(); wake(); }); on(video, 'pause', () => { sync(); wake(); });
    on(video, 'waiting', () => { message.textContent = '불러오는 중…'; message.hidden = false; }); on(video, 'playing', () => { message.hidden = true; });
    on(video, 'error', error); on(root, 'pointermove', wake); on(root, 'focusin', wake);
    on(doc, 'fullscreenchange', () => { fullscreen.setAttribute('aria-label', doc.fullscreenElement === root ? '전체화면 종료' : '전체화면'); });
    on(root, 'keydown', event => {
        if (event.key === 'Escape' && !settings.hidden) { event.preventDefault(); event.stopPropagation(); settings.hidden = true; settingsButton.setAttribute('aria-expanded', 'false'); settingsButton.focus(); return; }
        if (event.target !== root && event.target !== video) return;
        const key = event.key.toLowerCase();
        if ([' ', 'k', 'arrowleft', 'arrowright', 'm', 'f'].includes(key)) { event.preventDefault(); event.stopPropagation(); }
        if (key === ' ' || key === 'k') togglePlay();
        if (keyboardSeek && (key === 'arrowleft' || key === 'arrowright')) jump(key === 'arrowleft' ? -seconds : seconds);
        if (key === 'm') video.muted = !video.muted;
        if (key === 'f') fullscreen.click();
        if (key === 'escape') { settings.hidden = true; settingsButton.setAttribute('aria-expanded', 'false'); }
    });
    overlay.append(seek, controls); screen.append(video, feedback, message, overlay, settings); root.append(screen, jumps); host.append(root); sync(); wake();
    return { video, root, destroy() { destroyed = true; leaveExpanded(); clearTimeout(clickTimer); clearTimeout(hideTimer); clearTimeout(feedbackTimer); listeners.forEach(remove => remove()); if (doc.fullscreenElement === root) doc.exitFullscreen()?.catch(() => {}); video.pause(); video.removeAttribute('src'); video.load(); root.remove(); } };
}
