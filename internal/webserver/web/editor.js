// Shared by the React desktop launcher and the mobile web page. The server owns
// immutable frame IDs; this UI sends crop coordinates and deleted IDs only.
const labels = { pending: '대기 중', preparing: '프레임 준비 중', ready: '편집 가능', running: '변환 중', verifying: '결과 확인 중', publishing: '저장 중', completed: '완료', failed: '실패', cancelled: '취소됨', interrupted: '중단됨', released: '정리됨', stop_unconfirmed: '앱 재시작 필요' };
const active = value => ['pending', 'preparing', 'running', 'verifying', 'publishing'].includes(value);
const clamp = (n, lo, hi) => Math.max(lo, Math.min(hi, Math.round(n)));
export function formatEditTime(seconds) {
    const ticks = Math.max(0, Math.round((Number.isFinite(seconds) ? seconds : 0) * 100));
    const whole = Math.floor(ticks / 100);
    const pad = value => String(value).padStart(2, '0');
    return `${pad(Math.floor(whole / 3600))}:${pad(Math.floor(whole / 60) % 60)}:${pad(whole % 60)}.${pad(ticks % 100)}`;
}
export function keptFrames(count, deleted) { const removed = new Set(deleted); return Array.from({ length: count }, (_, n) => n).filter(n => !removed.has(n)); }
export function boundedCrop(crop, width, height) {
    const w = clamp(Number(crop.width) || 1, 1, width), h = clamp(Number(crop.height) || 1, 1, height);
    return { x: clamp(Number(crop.x) || 0, 0, width - w), y: clamp(Number(crop.y) || 0, 0, height - h), width: w, height: h };
}
// Ranges keep long selections proportional to edits, rather than total frames.
export class FrameRanges {
    constructor(values = []) {
        const ranges = values.map(value => typeof value === 'number' ? { first: value, last: value } : { ...value })
            .filter(r => Number.isSafeInteger(r.first) && Number.isSafeInteger(r.last) && r.first >= 0 && r.last >= r.first)
            .sort((a, b) => a.first - b.first);
        this.ranges = [];
        for (const r of ranges) {
            const last = this.ranges[this.ranges.length - 1];
            if (last && r.first <= last.last + 1)
                last.last = Math.max(last.last, r.last);
            else
                this.ranges.push({ ...r });
        }
    }
    get length() { return this.ranges.reduce((n, r) => n + r.last - r.first + 1, 0); }
    get size() { return this.length; }
    includes(n) { return this.ranges.some(r => r.first <= n && n <= r.last); }
    has(n) { return this.includes(n); }
    clear() { this.ranges = []; }
    add(n) { this.addRange(n, n); }
    addRange(first, last) {
        if (!Number.isSafeInteger(first) || !Number.isSafeInteger(last) || first < 0 || last < first)
            return;
        const merged = [];
        let range = { first, last };
        for (const r of this.ranges) {
            if (r.last < range.first - 1)
                merged.push(r);
            else if (range.last < r.first - 1) {
                merged.push(range);
                range = r;
            }
            else
                range = { first: Math.min(range.first, r.first), last: Math.max(range.last, r.last) };
        }
        merged.push(range);
        this.ranges = merged;
    }
    delete(n) { this.ranges = this.ranges.flatMap(r => n < r.first || n > r.last ? [r] : [...(n > r.first ? [{ first: r.first, last: n - 1 }] : []), ...(n < r.last ? [{ first: n + 1, last: r.last }] : [])]); }
    next(n, count) {
        for (const r of this.ranges) {
            if (r.first > n)
                break;
            if (r.last >= n)
                n = r.last + 1;
        }
        return n < count ? n : null;
    }
    toJSON() { return this.ranges; }
}
export function mountRangeControl(host, { label, count, values, change, format }) {
    const doc = host.ownerDocument, root = doc.createElement('section');
    root.className = 'sbe-trim-range';
    root.setAttribute('aria-label', label);
    const heading = doc.createElement('div');
    heading.className = 'sbe-range-heading';
    const title = doc.createElement('strong');
    title.textContent = label;
    const value = doc.createElement('output');
    heading.append(title, value);
    root.append(heading);
    const track = doc.createElement('div');
    track.className = 'sbe-range-track';
    const selected = doc.createElement('div');
    selected.className = 'sbe-range-selection';
    const marker = doc.createElement('div');
    marker.className = 'sbe-range-marker';
    track.append(selected, marker);
    root.append(track);
    const handles = {};
    let before = null;
    for (const side of ['start', 'end']) {
        const handle = doc.createElement('button');
        handle.type = 'button';
        handle.className = 'sbe-range-handle sbe-range-' + side;
        handle.setAttribute('role', 'slider');
        handle.setAttribute('aria-label', label + ' ' + (side === 'start' ? '시작' : '종료') + ' 손잡이');
        handle.title = handle.getAttribute('aria-label') + ' (←·↓ / →·↑: 한 칸, Home / End: 처음·끝)';
        handle.setAttribute('aria-keyshortcuts', 'ArrowLeft ArrowDown ArrowRight ArrowUp Home End');
        const grip = doc.createElement('span');
        grip.textContent = 'Ⅱ';
        grip.setAttribute('aria-hidden', 'true');
        handle.append(grip);
        track.append(handle);
        handles[side] = handle;
        function move(n, done = false) { const [start, end] = values(), max = count(); change(side === 'start' ? clamp(n, 0, end - 1) : start, side === 'end' ? clamp(n, start + 1, max) : end, side, done, before); update(); }
        handle.addEventListener('focus', () => {
            for (const [name, item] of Object.entries(handles)) item.style.zIndex = name === side ? '2' : '1';
        });
        handle.addEventListener('keydown', event => {
            if (!['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown', 'Home', 'End'].includes(event.key))
                return;
            event.preventDefault();
            event.stopPropagation();
            before = [...values()];
            let n = values()[side === 'start' ? 0 : 1];
            n += ['ArrowRight', 'ArrowUp'].includes(event.key) ? 1 : -1;
            if (event.key === 'Home')
                n = 0;
            if (event.key === 'End')
                n = count();
            move(n, true);
            before = null;
        });
    }
    function update(current = null) {
        const max = Math.max(1, count()), [start, end] = values();
        selected.style.left = start / max * 100 + '%';
        selected.style.width = (end - start) / max * 100 + '%';
        for (const [side, n] of [['start', start], ['end', end]]) {
            const h = handles[side];
            h.style.left = n / max * 100 + '%';
            h.setAttribute('aria-valuemin', '0');
            h.setAttribute('aria-valuemax', String(max));
            h.setAttribute('aria-valuenow', String(n));
            h.setAttribute('aria-valuetext', format(n));
        }
        value.textContent = format(start) + ' – ' + format(end);
        marker.hidden = current === null;
        marker.style.left = (current || 0) / max * 100 + '%';
    }
    let drag = null;
    track.addEventListener('pointerdown', event => {
        if (event.button !== 0 || !event.target.closest('.sbe-range-handle')) return;
        const rect = track.getBoundingClientRect(), [start, end] = values(), max = Math.max(1, count());
        const left = rect.left + start / max * rect.width, right = rect.left + end / max * rect.width;
        // At almost identical positions, drag left to grab start and right to grab end.
        const side = Math.abs(right - left) < 8 ? null : Math.abs(event.clientX - left) <= Math.abs(event.clientX - right) ? 'start' : 'end';
        before = [...values()];
        drag = { id: event.pointerId, x: event.clientX, side, start, end, width: rect.width };
        track.setPointerCapture(event.pointerId);
        if (side) handles[side].focus();
        event.preventDefault();
    });
    track.addEventListener('pointermove', event => {
        if (!drag || drag.id !== event.pointerId || !drag.width) return;
        const dx = event.clientX - drag.x;
        if (!drag.side && Math.abs(dx) < 2) return;
        if (!drag.side) { drag.side = dx < 0 ? 'start' : 'end'; handles[drag.side].focus(); }
        const max = count(), n = (drag.side === 'start' ? drag.start : drag.end) + dx / drag.width * max;
        const [start, end] = values();
        change(drag.side === 'start' ? clamp(n, 0, end - 1) : start, drag.side === 'end' ? clamp(n, start + 1, max) : end, drag.side, false, before);
        update();
    });
    function finishDrag(event) {
        if (!drag || drag.id !== event.pointerId) return;
        const [start, end] = values();
        if (drag.side) change(start, end, drag.side, true, before);
        drag = null; before = null;
    }
    track.addEventListener('pointerup', finishDrag);
    track.addEventListener('pointercancel', finishDrag);
    host.append(root);
    update();
    return { root, update };
}
export function mountMediaEditor(host, options) {
    const { api, file, onClose } = options;
    const sourceGIF = file?.nativeGIF || file?.kind === 'image' || file?.detectedFormat === 'gif';
    const doc = host.ownerDocument;
    const view = doc.defaultView;
    function node(tag, text, className) {
        const el = doc.createElement(tag);
        if (text !== undefined)
            el.textContent = text;
        if (className)
            el.className = className;
        return el;
    }
    function button(text, action, parent, className = '') { const el = node('button', text, className); el.type = 'button'; el.addEventListener('click', () => void action()); parent?.append(el); return el; }
    const dialog = node('dialog', undefined, 'sb-editor');
    dialog.setAttribute('aria-label', 'GIF 만들기·동영상 자르기');
    host.append(dialog);
    const heading = node('header', undefined, 'sbe-heading');
    const title = node('h2', 'GIF 만들기·동영상 자르기');
    heading.append(title);
    const headingActions = node('div', undefined, 'sbe-heading-actions');
    heading.append(headingActions);
    const shortcutToggle = button('단축키 안내', () => showShortcuts(shortcutsPanel.hidden), headingActions, 'sbe-shortcut-toggle');
    const keyboardIcon = doc.createElementNS('http://www.w3.org/2000/svg', 'svg');
    keyboardIcon.setAttribute('viewBox', '0 0 24 24'); keyboardIcon.setAttribute('width', '18'); keyboardIcon.setAttribute('height', '18');
    keyboardIcon.setAttribute('fill', 'none'); keyboardIcon.setAttribute('stroke', 'currentColor'); keyboardIcon.setAttribute('stroke-width', '1.7'); keyboardIcon.setAttribute('aria-hidden', 'true');
    const keyboardPath = doc.createElementNS('http://www.w3.org/2000/svg', 'path');
    keyboardPath.setAttribute('d', 'M4 5h16a1 1 0 0 1 1 1v12a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1V6a1 1 0 0 1 1-1ZM6 9h2m3 0h2m3 0h2M6 12h2m3 0h2m3 0h2M7 16h10');
    keyboardIcon.append(keyboardPath); shortcutToggle.prepend(keyboardIcon);
    const closeEditor = button('닫기', () => dialog.close(), headingActions);
    closeEditor.title = '편집기 닫기 (Esc)';
    dialog.append(heading);
    const shortcutsPanel = node('section', undefined, 'sbe-shortcuts');
    shortcutsPanel.id = 'sbe-shortcuts-' + crypto.randomUUID(); shortcutsPanel.hidden = true; shortcutsPanel.tabIndex = -1;
    shortcutsPanel.setAttribute('role', 'region'); shortcutsPanel.setAttribute('aria-label', '편집 단축키 안내');
    shortcutToggle.setAttribute('aria-controls', shortcutsPanel.id); shortcutToggle.setAttribute('aria-expanded', 'false');
    const shortcutHeading = node('div', undefined, 'sbe-shortcuts-heading');
    shortcutHeading.append(node('h3', '단축키 안내'));
    button('단축키 안내 닫기', () => showShortcuts(false), shortcutHeading);
    shortcutsPanel.append(shortcutHeading);
    const shortcutMode = node('p', '', 'sbe-hint'); shortcutsPanel.append(shortcutMode);
    const frameShortcuts = [
        { label: '이전 프레임', keys: '←', aria: 'ArrowLeft' },
        { label: '다음 프레임', keys: '→', aria: 'ArrowRight' },
        { label: '선택 프레임 삭제', keys: 'Delete', aria: 'Delete' },
        { label: '실행 취소', keys: 'Ctrl+Z / ⌘+Z', aria: 'Control+z Meta+z' },
        { label: '다시 실행', keys: 'Ctrl+Shift+Z / ⌘+Shift+Z', aria: 'Control+Shift+z Meta+Shift+z' },
    ];
    function shortcutGroup(label, rows, hint) {
        const group = node('section', undefined, 'sbe-shortcut-group');
        group.append(node('h4', label));
        const list = node('dl');
        for (const row of rows) {
            const value = node('dd'); value.append(node('kbd', row.keys));
            list.append(node('dt', row.label), value);
        }
        group.append(list);
        if (hint) group.append(node('p', hint, 'sbe-hint'));
        shortcutsPanel.append(group);
        return group;
    }
    const frameShortcutGroup = shortcutGroup('프레임 편집', [...frameShortcuts, { label: '프레임 범위 선택', keys: 'Shift+클릭' }], '입력창에 초점이 있을 때는 입력창의 기본 동작을 우선합니다. Shift+클릭은 프레임 체크박스에서 사용합니다.');
    const rangeShortcutGroup = shortcutGroup('구간 손잡이', [
        { label: '이전 값으로 한 칸 이동', keys: '← / ↓' },
        { label: '다음 값으로 한 칸 이동', keys: '→ / ↑' },
        { label: '허용 범위의 처음·끝으로 이동', keys: 'Home / End' },
    ], '시작 또는 종료 손잡이에 초점을 맞춘 뒤 사용합니다. 손잡이에 초점이 있으면 방향키로 구간 경계를 조절합니다.');
    shortcutGroup('공통', [
        { label: '다음·이전 조작 항목으로 이동', keys: 'Tab / Shift+Tab' },
        { label: '단축키 안내 또는 편집기 닫기', keys: 'Esc' },
    ], '안내가 열려 있으면 Esc로 안내를 먼저 닫습니다.');
    dialog.append(shortcutsPanel);
    let shortcutScroll = 0;
    function showShortcuts(open) {
        if (open) shortcutScroll = mainPane.scrollTop;
        shortcutsPanel.hidden = !open;
        shortcutToggle.setAttribute('aria-expanded', String(open));
        if (open) { mainPane.scrollTop = 0; shortcutsPanel.focus({ preventScroll: true }); }
        else { mainPane.scrollTop = shortcutScroll; shortcutToggle.focus({ preventScroll: true }); }
    }
    function describeShortcut(control, index) {
        const shortcut = frameShortcuts[index];
        control.title = `${shortcut.label} (${shortcut.keys})`;
        control.setAttribute('aria-keyshortcuts', shortcut.aria);
    }
    const message = node('p', '', 'sbe-message');
    message.setAttribute('role', 'status');
    dialog.append(message);
    const content = node('div', undefined, 'sbe-content');
    const editorBody = node('div', undefined, 'sbe-body');
    const mainPane = node('div', undefined, 'sbe-main-pane');
    mainPane.append(shortcutsPanel, message, content);
    editorBody.append(mainPane); dialog.append(editorBody);
    let dead = false, busy = false, session = null, selection = new FrameRanges(), current = 0, page = 0, anchor = 0;
    let crop = null, appliedCrop = null, clipStart = 0, clipEnd = 0, deleted = new FrameRanges(), undo = [], redo = [], playing = false, playbackVersion = 0, playTimer, pollTimer, reading = false;
    let prepareRetry = null, createRetry = null, sessions = [], jobs = [], selectedJob = null, pointer = null, paintSerial = 0;
    const imageCache = new Map(), frameTimes = new Map(), frameDelays = new Map();
    let navTarget = null, navTimer;
    const frameURL = options.frameURL || ((id, n, thumbnail = false) => `/edit-assets/sessions/${id}/${n}${thumbnail ? '?thumbnail=1' : ''}`);
    const resultURL = options.resultURL || (id => `/edit-assets/jobs/${id}`);
    function report(text, error = false) {
        if (dead)
            return;
        message.textContent = text;
        message.setAttribute('role', error ? 'alert' : 'status');
    }
    async function unwrap(promise) {
        let timer;
        try {
            const timeout = new Promise((_, reject) => { timer = setTimeout(() => reject(new Error('요청 응답을 받지 못했습니다. PC 작업 목록을 확인한 뒤 다시 요청하세요.')), 15000); });
            const result = await Promise.race([promise, timeout]);
            if (!result.ok) {
                const e = new Error(result.error.message);
                e.code = result.error.code;
                throw e;
            }
            return result.data;
        }
        finally {
            clearTimeout(timer);
        }
    }
    async function action(work) {
        if (busy || dead)
            return;
        busy = true;
        sync();
        report('처리 중…');
        try {
            await work();
        }
        catch (e) {
            report(e.message || '요청 결과를 받지 못했습니다. 작업 목록에서 확인하거나 다시 요청하세요.', true);
        }
        finally {
            busy = false;
            if (!dead)
                sync();
        }
    }
    function stopPlayback() { playing = false; playbackVersion++; clearTimeout(playTimer); play.textContent = '편집 결과 재생'; }
    const storageKey = () => `storagebucket-edit-v1:${session.id}`;
    function persist() {
        if (!session || !crop)
            return;
        try {
            localStorage.setItem(storageKey(), JSON.stringify({ crop, appliedCrop, clipStart, clipEnd, deleted, undo, redo }));
        }
        catch {
            report('브라우저 저장 공간이 부족하여 편집 초안을 보관하지 못했습니다.', true);
        }
    }
    function snapshot() { return { crop: { ...crop }, appliedCrop: appliedCrop ? { ...appliedCrop } : null, clipStart, clipEnd, deleted: deleted.toJSON().map(r => ({ ...r })) }; }
    function remember(previous) {
        scheduleResources();
        undo.push(previous);
        if (undo.length > 50)
            undo.shift();
        redo = [];
        createRetry = null;
        stopPlayback();
        persist();
        sync();
    }
    function restore(value) {
        crop = boundedCrop(value.crop, session.width, session.height);
        appliedCrop = boundedCrop(value.appliedCrop || value.crop, session.width, session.height);
        clipStart = clamp(value.clipStart || 0, 0, editableCount(session) - 1);
        clipEnd = clamp(value.clipEnd || editableCount(session), clipStart + 1, editableCount(session));
        deleted = validRanges(value.deleted);
        if (deleted.length >= session.frameCount)
            deleted.delete(session.frameCount - 1);
        selection.clear();
        persist();
        sync();
    }
    const setup = node('section', undefined, 'sbe-setup');
    const sourceSurface = node('section', undefined, 'sbe-source-surface');
    const sourceTitle = node('h3', file?.title || '편집 미리보기');
    sourceSurface.append(sourceTitle);
    content.append(sourceSurface, setup);
    let source = null, paintedSession = null, quickCrop = false, preciseCrop = null, quickPrepareRetry = null;
    const timeMode = value => value?.options.quickVideo || value?.options.kind === 'trim';
    if (file?.src && !sourceGIF) {
        source = node('video');
        source.src = file.src;
        source.controls = true;
        source.playsInline = true;
        source.preload = 'auto';
        source.className = 'sbe-source';
        sourceSurface.append(source);
        source.addEventListener('error', () => report('이 브라우저는 원본을 재생하지 못합니다. 프레임 정밀 편집에서 편집용 미리보기로 확인할 수 있습니다.'));
    }
    const cropPlaceholder = node('p', file ? '원본 영상을 읽고 있습니다. 재생 정보가 준비되면 바로 구간을 선택할 수 있습니다.' : '영상 카드의 ⋯ 메뉴에서 “GIF, WebP로 만들기”를 선택해 주세요. 기존 편집 자료는 아래에서 다시 열 수 있습니다.', 'sbe-crop-placeholder');
    sourceSurface.append(cropPlaceholder);
    const fields = node('div', undefined, 'sbe-fields');
    setup.append(fields);
    function field(label, element, parent = fields) { const wrap = node('label', label); wrap.append(element); parent.append(wrap); return element; }
    function numeric(label, value, min, max, step = 1, parent = fields) { const el = node('input'); el.type = 'number'; el.value = String(value); el.min = String(min); el.max = String(max); el.step = String(step); return field(label, el, parent); }
    const kind = node('select');
    for (const [value, text] of [['gif', 'GIF 만들기'], ['webp', 'animated WebP 만들기'], ['trim', '동영상 자르기']]) {
        const o = node('option', text);
        o.value = value;
        kind.append(o);
    }
    field('작업 종류', kind);
    const start = numeric('시작 시간(초)', 0, 0, file?.duration || 86400, .01);
    const end = numeric('종료 시간(초)', file?.duration || 3, .01, file?.duration || 86400, .01);
    const fps = numeric('애니메이션 FPS', 10, 1, Number.MAX_SAFE_INTEGER);
    start.parentElement.hidden = end.parentElement.hidden = fps.parentElement.hidden = true;
    if (sourceGIF) {
        fps.parentElement.hidden = true;
        kind.querySelector('option[value=trim]').disabled = true;
    }
    const mute = node('input');
    let sourceRangeTouched = false;
    mute.type = 'checkbox';
    field('영상 소리 제거', mute);
    const sourceRange = mountRangeControl(setup, { label: '원본 구간 선택', count: () => Math.max(1, Math.round((Number.isFinite(source?.duration) && source.duration > 0 ? source.duration : file?.duration || 3) * 100)), values: () => [Math.round(+start.value * 100), Math.round(+end.value * 100)], format: n => formatEditTime(n / 100), change: (a, b, side) => {
            start.value = (a / 100).toFixed(2);
            end.value = (b / 100).toFixed(2);
            sourceRangeTouched = true;
            if (source) {
                source.pause();
                try {
                    source.currentTime = (side === 'start' ? a : Math.max(a, b - 1)) / 100;
                }
                catch { }
            }
        } });
    sourceRange.root.hidden = true;
    for (const input of [start, end])
        input.addEventListener('input', () => sourceRange.update());
    source?.addEventListener('loadedmetadata', () => {
        if (Number.isFinite(source.duration) && source.duration > 0) {
            start.max = end.max = String(source.duration);
            end.value = String(sourceRangeTouched ? Math.min(+end.value, source.duration) : source.duration);
            sourceRange.update();
            if (!session && source.videoWidth > 0 && source.videoHeight > 0) {
                useSession({ id: crypto.randomUUID(), localQuick: true, fileId: file.id, title: file.title || '영상', state: 'ready', width: source.videoWidth, height: source.videoHeight, frameCount: 0, progress: 100,
                    options: { fileId: file.id, quickVideo: true, fullSource: true, kind: 'gif', start: 0, end: source.duration, fps: 1000 } });
            }
        }
    });
    source?.addEventListener('seeked', () => { if (session?.options.quickVideo && quickCrop) void paint(); });
    source?.addEventListener('loadeddata', () => { if (session?.options.quickVideo && quickCrop) void paint(); });
    if (sourceGIF) {
        start.parentElement.hidden = end.parentElement.hidden = true;
        setup.append(node('p', 'GIF는 원본 프레임과 재생 간격을 유지합니다. 프레임 준비 후 남길 구간을 정하세요.', 'sbe-hint'));
    }
    const setupActions = node('div', undefined, 'sbe-actions');
    setup.append(setupActions);
    const sourceActions = node('div', undefined, 'sbe-actions sbe-source-actions');
    setupActions.append(sourceActions);
    if (source) {
        function setSourceBound(side) {
            if (timeMode(session) && session.state === 'ready') {
                const value = Math.round((source.currentTime-session.options.start)*1000);
                if (side === 'start') clipStart = clamp(value, 0, clipEnd-1);
                else clipEnd = clamp(value, clipStart+1, editableCount(session));
                start.value = String(frameTime(clipStart)); end.value = String(frameTime(clipEnd));
                createRetry = null; sync();
            } else { (side === 'start' ? start : end).value = source.currentTime.toFixed(2); sourceRange.update(); }
        }
        button('현재 위치를 시작으로', () => setSourceBound('start'), sourceActions);
        button('현재 위치를 종료로', () => setSourceBound('end'), sourceActions);
        let segmentEnd = null;
        button('선택 구간 재생', () => {
            if (+end.value <= +start.value)
                return report('종료 시간은 시작 시간보다 뒤여야 합니다.', true);
            segmentEnd = +end.value;
            source.currentTime = +start.value;
            source.play().catch(() => report('원본 구간을 재생하지 못했습니다.', true));
        }, sourceActions);
        source.addEventListener('timeupdate', () => {
            if (segmentEnd !== null && source.currentTime >= segmentEnd) {
                source.currentTime = +start.value;
            }
        });
        source.addEventListener('pause', () => { segmentEnd = null; });
    }
    setup.append(node('p', '원본 영상의 구간을 바로 선택하고 저장할 수 있습니다. 프레임별 삭제는 선택 구간의 “프레임 정밀 편집”을 여세요.', 'sbe-hint'));
    const prepare = button('프레임 정밀 편집', () => action(async () => {
        if (!file)
            return;
        const bounded = !sourceGIF && (Number.isFinite(source?.duration) && source.duration > 0 || file.duration > 0);
        const req = { fileId: file.id, fullSource: !bounded, ...(bounded ? { originalFrames: true } : {}), start: bounded ? session?.state === 'ready' ? frameTime(clipStart) ?? +start.value : +start.value : 0, end: bounded ? session?.state === 'ready' ? frameTime(clipEnd) ?? +end.value : +end.value : 0, fps: 1000, width: 0, kind: 'gif' };
        if (session && !session.options.quickVideo && (deleted.length || undo.length) && !confirm('새 프레임을 준비할까요? 현재 편집 초안은 기존 편집 자료에서 다시 열 수 있습니다.'))
            return;
        if (!prepareRetry || JSON.stringify(prepareRetry.payload) !== JSON.stringify(req))
            prepareRetry = { payload: req, id: crypto.randomUUID() };
        const previousSession = session;
        const value = await unwrap(api.prepare({ ...req, requestId: prepareRetry.id }));
        if (dead || session !== previousSession)
            return;
        prepareRetry = null;
        preciseCrop = crop ? { crop: { ...crop }, appliedCrop: { ...(appliedCrop || crop) } } : null;
        useSession(value);
        report('선택 구간의 프레임을 준비하고 있습니다. 아래 진행 상태를 확인해 주세요.');
        await refresh();
    }), setupActions, 'sbe-primary');
    setup.append(node('p', '출력은 원본·크롭 픽셀을 그대로 유지합니다. 삭제한 프레임과 해당 재생 시간을 함께 제거하며 원본은 보존합니다.', 'sbe-hint'));
    if (!file)
        setup.hidden = true;
    const workspace = node('section', undefined, 'sbe-workspace');
    workspace.hidden = true;
    content.append(workspace);
    const muteLabel = mute.parentElement;
    workspace.append(muteLabel);
    const summary = node('p');
    workspace.append(summary);
    const gifWorkspace = node('div', undefined, 'sbe-edit-layout');
    const editMain = node('div', undefined, 'sbe-edit-main'), frameAside = node('aside', undefined, 'sbe-frame-aside');
    frameAside.setAttribute('aria-label', '프레임 탐색');
    frameAside.hidden = true;
    frameAside.append(node('h3', '프레임'));
    editorBody.prepend(frameAside);
    gifWorkspace.append(editMain);
    workspace.append(gifWorkspace);
    const clipControl = mountRangeControl(workspace, { label: '남길 구간', count: () => editableCount(session), values: () => [clipStart, clipEnd || editableCount(session)], format: n => frameLabel(n), change: (a, b, side, done, previous) => {
            stopPlayback();
            clipStart = a;
            clipEnd = b;
            if (timeMode(session)) {
                current = side === 'start' ? a : b - 1;
                start.value = String(frameTime(a)); end.value = String(frameTime(b));
                if (source) {
                    source.pause();
                    try {
                        source.currentTime = frameTime(current);
                    }
                    catch { }
                }
            }
            else
                seekFrame(side === 'start' ? a : b - 1);
            createRetry = null;
            if (done) {
                const before = snapshot();
                before.clipStart = previous?.[0] ?? a;
                before.clipEnd = previous?.[1] ?? b;
                remember(before);
            }
            void requestTiming(a);
            void requestTiming(b);
        } });
    clipControl.root.hidden = true;
    sourceSurface.insertBefore(clipControl.root, sourceSurface.children[1]);
    sourceSurface.insertBefore(sourceRange.root, sourceSurface.children[1]);
    const stage = node('canvas', undefined, 'sbe-stage');
    stage.tabIndex = 0;
    stage.setAttribute('aria-label', '크롭 영역 편집. 모서리와 테두리에서 크기 조절, 영역 안에서 이동. 새 영역은 새 영역 그리기로 선택');
    stage.style.cursor = 'default';
    const cropSurface = node('section', undefined, 'sbe-crop-surface');
    cropSurface.setAttribute('aria-label', '크롭 편집');
    cropSurface.hidden = true;
    sourceSurface.append(cropSurface);
    cropSurface.append(stage);
    const magnifier = node('div', undefined, 'sbe-magnifier');
    magnifier.hidden = true;
    magnifier.setAttribute('aria-hidden', 'true');
    const magnifierCanvas = node('canvas');
    magnifier.append(magnifierCanvas, node('span', undefined, 'sbe-magnifier-crosshair'), node('span', '4×', 'sbe-magnifier-label'));
    dialog.append(magnifier);
    let magnifierImage = null;
    function drawMagnifier() {
        if (!pointer || !magnifierImage || magnifierImage.sessionId !== session?.id || dead) {
            magnifier.hidden = true;
            return;
        }
        const rect = stage.getBoundingClientRect();
        if (!rect.width || !rect.height) { magnifier.hidden = true; return; }
        const bounds = dialog.getBoundingClientRect(), gap = 24;
        const outerWidth = Math.min(192, Math.max(1, (bounds.width || view.innerWidth) - 16));
        const outerHeight = Math.min(160, Math.max(1, (bounds.height || view.innerHeight) - 16));
        const minX = Math.max(8, bounds.left + 8), maxX = Math.min(view.innerWidth - 8, bounds.right - 8) - outerWidth;
        const minY = Math.max(8, bounds.top + 8), maxY = Math.min(view.innerHeight - 8, bounds.bottom - 8) - outerHeight;
        let left = pointer.clientX + gap, top = pointer.clientY - outerHeight / 2;
        if (left > maxX) left = pointer.clientX - gap - outerWidth;
        if (left < minX) {
            left = pointer.clientX - outerWidth / 2;
            top = pointer.clientY - gap - outerHeight;
            if (top < minY) top = pointer.clientY + gap;
        }
        magnifier.style.width = outerWidth + 'px';
        magnifier.style.height = outerHeight + 'px';
        magnifier.style.left = clamp(left, minX, Math.max(minX, maxX)) + 'px';
        magnifier.style.top = clamp(top, minY, Math.max(minY, maxY)) + 'px';
        magnifier.hidden = false;
        const width = Math.max(1, magnifierCanvas.clientWidth || outerWidth - 2), height = Math.max(1, magnifierCanvas.clientHeight || outerHeight - 2);
        const density = view.devicePixelRatio || 1;
        magnifierCanvas.width = Math.max(1, Math.round(width * density));
        magnifierCanvas.height = Math.max(1, Math.round(height * density));
        const image = magnifierImage.image, imageWidth = image.width || session.width, imageHeight = image.height || session.height;
        const center = point({ clientX: pointer.clientX, clientY: pointer.clientY });
        const sampleWidth = width / 4 * imageWidth / rect.width, sampleHeight = height / 4 * imageHeight / rect.height;
        const x = center.x * imageWidth / session.width - sampleWidth / 2, y = center.y * imageHeight / session.height - sampleHeight / 2;
        const ctx = magnifierCanvas.getContext('2d');
        if (!ctx) { magnifier.hidden = true; return; }
        ctx.fillStyle = '#071118';
        ctx.fillRect(0, 0, magnifierCanvas.width, magnifierCanvas.height);
        ctx.drawImage(image, x, y, sampleWidth, sampleHeight, 0, 0, magnifierCanvas.width, magnifierCanvas.height);
        const sx = magnifierCanvas.width / sampleWidth, sy = magnifierCanvas.height / sampleHeight;
        const cropX = (crop.x * imageWidth / session.width - x) * sx, cropY = (crop.y * imageHeight / session.height - y) * sy;
        const cropWidth = crop.width * imageWidth / session.width * sx, cropHeight = crop.height * imageHeight / session.height * sy;
        ctx.fillStyle = '#0005';
        ctx.beginPath(); ctx.rect(0, 0, magnifierCanvas.width, magnifierCanvas.height); ctx.rect(cropX, cropY, cropWidth, cropHeight); ctx.fill('evenodd');
        ctx.strokeStyle = '#a1ead5'; ctx.lineWidth = 2 * density;
        ctx.strokeRect(cropX, cropY, cropWidth, cropHeight);
    }
    const cropFields = node('div', undefined, 'sbe-fields');
    cropSurface.append(cropFields);
    const cropInputs = {};
    let cropMode = 'adjust';
    const drawArea = button('새 영역 그리기', () => { cropMode = 'draw'; stage.style.cursor = 'crosshair'; drawArea.setAttribute('aria-pressed', 'true'); adjustArea.setAttribute('aria-pressed', 'false'); report('영상 위에서 드래그하여 새 크롭 영역을 그리세요.'); }, cropFields);
    const adjustArea = button('영역 이동·크기 조절', () => { cropMode = 'adjust'; stage.style.cursor = 'default'; drawArea.setAttribute('aria-pressed', 'false'); adjustArea.setAttribute('aria-pressed', 'true'); report('영역 안에서 이동하거나 모서리·테두리에서 크기를 조절하세요.'); }, cropFields);
    drawArea.setAttribute('aria-pressed', 'false');
    adjustArea.setAttribute('aria-pressed', 'true');
    for (const [key, label] of [['x', '왼쪽'], ['y', '위쪽'], ['width', '너비'], ['height', '높이']]) {
        const input = numeric(label, 0, 0, Number.MAX_SAFE_INTEGER, 1, cropFields);
        cropInputs[key] = input;
        let before = null;
        input.addEventListener('focus', () => {
            if (crop)
                before = snapshot();
        });
        input.addEventListener('input', () => {
            if (!session || !crop || input.value === '')
                return;
            crop = boundedCrop({ ...crop, [key]: +input.value }, session.width, session.height);
            stopPlayback();
            persist();
            void paint();
        });
        input.addEventListener('change', () => {
            if (!session || !crop)
                return;
            const previous = before || snapshot();
            before = null;
            crop = boundedCrop({ ...crop, [key]: +input.value }, session.width, session.height);
            remember(previous);
        });
    }
    button('전체 영역', () => {
        if (!session)
            return;
        const before = snapshot();
        crop = { x: 0, y: 0, width: session.width, height: session.height };
        remember(before);
    }, cropFields);
    cropSurface.append(node('p', '모서리나 테두리를 드래그해 크기를 바꾸거나 영역 안에서 위치를 옮기세요. 새 사각형을 만들려면 새 영역 그리기를 선택하세요.', 'sbe-hint'));
    const frameActions = node('div', undefined, 'sbe-actions');
    editMain.append(frameActions);
    const previousFrame = button('이전 프레임', () => { seekFrame(Math.max(0, current - 1)); }, frameActions);
    const nextFrame = button('다음 프레임', () => { seekFrame(Math.min(session.frameCount - 1, current + 1)); }, frameActions);
    const position = node('span');
    frameActions.append(position);
    const frameSlider = node('input');
    frameSlider.type = 'range';
    frameSlider.min = '0';
    frameSlider.setAttribute('aria-label', '현재 프레임');
    frameSlider.addEventListener('input', () => { seekFrame(+frameSlider.value); });
    editMain.append(frameSlider);
    const rangeActions = node('div', undefined, 'sbe-actions');
    editMain.append(rangeActions);
    const rangeFrom = numeric('선택 시작 번호', 1, 1, Number.MAX_SAFE_INTEGER, 1, rangeActions), rangeTo = numeric('선택 끝 번호', 1, 1, Number.MAX_SAFE_INTEGER, 1, rangeActions);
    button('범위 선택', () => { selection.clear(); selection.addRange(Math.max(0, +rangeFrom.value - 1), Math.min(session.frameCount - 1, +rangeTo.value - 1)); sync(); }, rangeActions);
    button('선택 해제', () => { selection.clear(); sync(); }, rangeActions);
    const deleteButton = button('선택 프레임 삭제', () => {
        if (!session || !selection.size)
            return;
        const combined = new FrameRanges([...deleted.ranges, ...selection.ranges]);
        if (combined.ranges.reduce((n, r) => n + Math.max(0, Math.min(r.last, clipEnd - 1) - Math.max(r.first, clipStart) + 1), 0) >= clipEnd - clipStart)
            return report('최소 한 프레임은 남겨야 합니다.', true);
        const before = snapshot();
        deleted = combined;
        selection.clear();
        remember(before);
    }, rangeActions);
    const undoButton = button('실행 취소', () => {
        if (!undo.length)
            return;
        stopPlayback();
        redo.push(snapshot());
        restore(undo.pop());
    }, rangeActions);
    const redoButton = button('다시 실행', () => {
        if (!redo.length)
            return;
        stopPlayback();
        undo.push(snapshot());
        restore(redo.pop());
    }, rangeActions);
    [previousFrame, nextFrame, deleteButton, undoButton, redoButton].forEach(describeShortcut);
    const applyCrop = button('모든 프레임에 크롭 적용', () => {
        if (!crop || JSON.stringify(crop) === JSON.stringify(appliedCrop))
            return;
        const before = snapshot();
        appliedCrop = { ...crop };
        remember(before);
        report('남기는 모든 프레임에 같은 크롭 영역을 적용했습니다.');
    }, cropFields, 'sbe-primary');
    const quickCropToggle = button('크롭 영역 조절', () => {
        quickCrop = !quickCrop;
        if (source) source.pause();
        sync();
    }, sourceActions);
    const cropState = node('p', '', 'sbe-hint');
    cropFields.append(cropState);
    const grid = node('nav', undefined, 'sbe-frames');
    grid.setAttribute('aria-label', 'GIF 프레임 목록');
    frameAside.append(grid);
    const paging = node('div', undefined, 'sbe-actions');
    frameAside.append(paging);
    const prevPage = button('이전 48장', () => { page--; navTarget = Math.max(0, page * 48); void renderFrames(); }, paging), pageText = node('span');
    paging.append(pageText);
    const nextPage = button('다음 48장', () => { page++; navTarget = Math.min(session.frameCount - 1, page * 48); void renderFrames(); }, paging);
    const previewBox = node('div', undefined, 'sbe-preview');
    editMain.append(previewBox);
    const play = button('편집 결과 재생', () => {
        if (playing)
            return stopPlayback();
        playing = true;
        const version = ++playbackVersion;
        play.textContent = '재생 멈추기';
        let n = current;
        async function tick() {
            if (!playing || dead || version !== playbackVersion)
                return;
            const index = nextRetained(n);
            if (index === null)
                return stopPlayback();
            n = index + 1;
            try {
                const img = await frameImage(index);
                if (!playing || dead || version !== playbackVersion)
                    return;
                current = index;
                void paint();
                position.textContent = framePosition(index);
                clipControl.update(index);
                renderFrames();
                drawPreview(img);
                playTimer = setTimeout(tick, 1000 * (frameDelays.get(index) || 1 / session.options.fps));
            }
            catch {
                if (version !== playbackVersion)
                    return;
                stopPlayback();
                report('프레임을 불러오지 못했습니다. PC 연결을 확인하세요.', true);
            }
        }
        void tick();
    }, previewBox);
    const previewCanvas = node('canvas');
    previewCanvas.setAttribute('aria-label', '크롭·삭제 적용 미리보기');
    previewBox.append(previewCanvas);
    let layoutFrame = null, lastPreview = null;
    function fitCanvas(canvas, width, height) {
        const paneWidth = mainPane.clientWidth;
        const fallbackWidth = (paneWidth || dialog.clientWidth || view.innerWidth) - 40 - (paneWidth || frameAside.hidden ? 0 : 156);
        const availableWidth = Math.max(1, canvas.parentElement.clientWidth || fallbackWidth);
        const bodyHeight = editorBody.clientHeight || Math.max(1, view.innerHeight - (heading.offsetHeight || 80) - 32);
        const scale = Math.min(availableWidth / width, Math.max(1, bodyHeight * .6) / height);
        const size = { width: width * scale, height: height * scale };
        canvas.style.width = size.width + 'px';
        canvas.style.height = size.height + 'px';
        return size;
    }
    function updateLayout() {
        layoutFrame = null;
        if (dead || !session || session.state !== 'ready' || session.options.kind === 'trim')
            return;
        fitCanvas(stage, session.width, session.height);
        if (lastPreview?.sessionId === session.id)
            drawPreview(lastPreview.image);
        drawMagnifier();
        renderFrames();
    }
    function scheduleLayout() {
        if (!dead && layoutFrame === null)
            layoutFrame = view.requestAnimationFrame(updateLayout);
    }
    const resizeObserver = typeof view.ResizeObserver === 'function' ? new view.ResizeObserver(scheduleLayout) : null;
    resizeObserver?.observe(editorBody);
    resizeObserver?.observe(mainPane);
    resizeObserver?.observe(grid);
    view.addEventListener('resize', scheduleLayout);
    const qualityBox = node('section', undefined, 'sbe-quality');
    workspace.append(qualityBox);
    const preset = node('select');
    for (const [value, text] of [['small', '작은 용량'], ['balanced', '균형'], ['high', '고화질'], ['max', '최대 화질'], ['custom', '사용자 지정']]) {
        const o = node('option', text);
        o.value = value;
        preset.append(o);
    }
    field('화질 프리셋', preset, qualityBox);
    const quality = numeric('화질(1–100)', 80, 1, 100, 1, qualityBox);
    const compression = node('select');
    for (const [value, text] of [['lossy', '손실 압축'], ['lossless', '무손실 압축']]) {
        const option = node('option', text); option.value = value; compression.append(option);
    }
    field('WebP 압축 방식', compression, qualityBox);
    const advanced = node('details', undefined, 'sbe-quality-advanced');
    advanced.append(node('summary', '고급 설정'));
    const advancedFields = node('div', undefined, 'sbe-advanced-fields');
    advanced.append(advancedFields); qualityBox.append(advanced);
    const effort = node('select');
    for (const [value, text] of [['fast', '빠르게'], ['normal', '보통'], ['precise', '정밀하게']]) {
        const o = node('option', text);
        o.value = value;
        effort.append(o);
    }
    field('압축률', effort, advancedFields);
    const dither = node('select');
    for (const [value, text] of [['auto', '자동'], ['sierra2_4a', 'Sierra · 균형'], ['sierra3', 'Sierra3 · 정밀'], ['floyd_steinberg', 'Floyd–Steinberg'], ['bayer', 'Bayer · 규칙적 패턴'], ['atkinson', 'Atkinson'], ['none', '없음']]) {
        const option = node('option', text); option.value = value; dither.append(option);
    }
    field('GIF 디더링', dither, advancedFields);
    const webpPreset = node('select');
    for (const [value, text] of [['none', '기본'], ['photo', '사진'], ['drawing', '그림'], ['icon', '아이콘'], ['text', '텍스트']]) {
        const option = node('option', text); option.value = value; webpPreset.append(option);
    }
    field('WebP 최적화 유형', webpPreset, advancedFields);
    const algorithmHint = node('p', '', 'sbe-hint'); advanced.append(algorithmHint);
    let qualityFormat = 'gif';
    function qualityValue() {
        const webp = kind.value === 'webp', lossless = webp && compression.value === 'lossless';
        const optimization = webp && !lossless ? webpPreset.value : 'none';
        return { preset: preset.value, quality: +quality.value, effort: optimization !== 'none' ? 'normal' : effort.value, lossless,
            ...(!webp && dither.value !== 'auto' ? { dither: dither.value } : {}), ...(optimization !== 'none' ? { webpPreset: optimization } : {}) };
    }
    function loadQuality() {
        qualityFormat = kind.value === 'webp' ? 'webp' : 'gif';
        let v;
        try {
            v = JSON.parse(localStorage.getItem('storagebucket-quality:' + qualityFormat) || 'null');
        }
        catch { }
        preset.value = ['small', 'balanced', 'high', 'max', 'custom'].includes(v?.preset) ? v.preset : 'balanced';
        quality.value = String(clamp(v?.quality || 80, 1, 100));
        effort.value = ['fast', 'normal', 'precise'].includes(v?.effort) ? v.effort : 'normal';
        compression.value = v?.lossless ? 'lossless' : 'lossy';
        dither.value = [...dither.options].some(option => option.value === v?.dither) ? v.dither : 'auto';
        webpPreset.value = [...webpPreset.options].some(option => option.value === v?.webpPreset) ? v.webpPreset : 'none';
        syncQuality();
    }
    function syncQuality() {
        const webp = kind.value === 'webp', lossless = compression.value === 'lossless';
        quality.disabled = preset.value !== 'custom';
        quality.parentElement.hidden = preset.value !== 'custom';
        effort.disabled = preset.value !== 'custom' || webp && !lossless && webpPreset.value !== 'none';
        compression.parentElement.hidden = !webp;
        dither.parentElement.hidden = webp;
        webpPreset.parentElement.hidden = !webp;
        webpPreset.disabled = lossless;
        algorithmHint.textContent = !webp ? '디더링은 제한된 GIF 색상으로 그라데이션을 표현하는 방식입니다. 자동은 압축률에 맞춰 선택합니다.' : lossless ? '무손실 압축에서는 최적화 유형을 적용하지 않습니다.' : webpPreset.value === 'none' ? '화면 내용에 맞는 압축 설정을 선택할 수 있습니다. 일반적인 영상은 기본으로 두세요.' : '최적화 유형을 선택하면 압축률은 자동으로 설정됩니다.';
        if (preset.value !== 'custom') {
            quality.value = String({ small: 60, balanced: 80, high: 95, max: 100 }[preset.value] || 80);
            effort.value = 'normal';
        }
        if (webp && !lossless && webpPreset.value !== 'none') effort.value = 'normal';
        try {
            localStorage.setItem('storagebucket-quality:' + qualityFormat, JSON.stringify({ ...qualityValue(), ...(webp ? { webpPreset: webpPreset.value } : {}) }));
        }
        catch { }
        createRetry = null;
        scheduleResources();
    }
    for (const el of [preset, quality, effort, compression, dither, webpPreset])
        el.addEventListener('change', syncQuality);
    const qualityHint = node('p', '', 'sbe-hint');
    qualityHint.textContent = '프레임 재생은 편집 확인용입니다. “화질 확인”은 현재 위치부터 남긴 프레임을 최대 2초, 실제 설정으로 변환합니다.';
    qualityBox.append(qualityHint);
    qualityBox.append(node('p', '화질 변경은 크기·FPS·편집 내용을 바꾸지 않습니다. WebP 무손실에서는 화질 값이 압축 효율과 처리 시간에 영향을 줍니다.', 'sbe-hint'));
    qualityBox.append(node('p', '화질 확인 결과는 최근 8개를 임시 보관합니다.', 'sbe-hint'));
    const sample = button('화질 확인', () => action(() => submit(true)), qualityBox);
    const resourceBox = node('section', undefined, 'sbe-resources');
    workspace.append(resourceBox);
    resourceBox.setAttribute('aria-label', '처리 PC 자원');
    let resourceTimer, resourceReading = false, resourceSerial = 0;
    const bytes = n => n >= 1024 ** 3 ? (n / 1024 ** 3).toFixed(2) + ' GiB' : n >= 1024 ** 2 ? (n / 1024 ** 2).toFixed(0) + ' MiB' : n >= 1024 ? (n / 1024).toFixed(1) + ' KiB' : n + ' B';
    function scheduleResources() {
        clearTimeout(resourceTimer);
        if (!dead)
            resourceTimer = setTimeout(updateResources, 400);
    }
    function payload() { return { sessionId: session.id, kind: session.options.quickVideo ? kind.value : session.options.kind === 'trim' ? 'trim' : kind.value === 'webp' ? 'webp' : 'gif', crop: appliedCrop || crop || { x: 0, y: 0, width: session.width, height: session.height }, rangeStart: timeMode(session) ? 0 : clipStart, rangeEnd: timeMode(session) ? 0 : clipEnd, ...(timeMode(session) ? { trimStart: frameTime(clipStart), trimEnd: frameTime(clipEnd) } : {}), deleted: [], mute: mute.checked, quality: qualityValue() }; }
    async function updateResources() {
        if (dead || !session || session.localQuick || session.state !== 'ready' || !api.resources || resourceReading)
            return;
        resourceReading = true;
        const serial = ++resourceSerial;
        try {
            const r = await unwrap(api.resources(payload()));
            if (dead || serial !== resourceSerial)
                return;
            resourceBox.replaceChildren(node('h3', '처리 PC 메모리·임시 디스크'));
            resourceBox.append(node('p', r.memoryKnown ? `사용 가능 RAM ${bytes(r.availableRam)} / 전체 ${bytes(r.totalRam)}` : 'PC RAM 정보를 읽지 못했습니다.'));
            resourceBox.append(node('p', r.estimateKnown ? `추가 RAM 예상 ${bytes(r.lowRam)}–${bytes(r.highRam)} (${r.basis === 'sampled' ? '미리보기 실측 반영' : '계산 추정'})` : '추가 RAM을 추정하지 못했습니다.'));
            if (r.live?.known)
                resourceBox.append(node('p', `실행 중 인코더 RAM ${bytes(r.live.currentBytes)} · 관측 최대 ${bytes(r.live.observedPeakBytes)}`));
            resourceBox.append(node('p', r.diskKnown ? `디스크 여유 ${bytes(r.diskAvailable)} · 임시·결과 예상 ${r.diskEstimateKnown ? bytes(r.diskEstimate) : '화질 확인 후 추정'}` : '디스크 여유를 읽지 못했습니다.'));
            resourceBox.append(node('p', r.note, 'sbe-hint'));
            if (r.pressure)
                resourceBox.append(node('p', '예상 RAM이 현재 여유보다 큽니다. 다른 작업을 줄이거나 설정을 조정할 수 있습니다. 그대로 실행할 수도 있습니다.', 'sbe-warning'));
        }
        catch (e) {
            if (!dead)
                resourceBox.textContent = '처리 PC 자원 정보를 가져오지 못했습니다. ' + e.message;
        }
        finally {
            resourceReading = false;
        }
    }
    async function submit(preview) {
        if (!session || session.state !== 'ready')
            return;
        if ((session.options.quickVideo ? kind.value : session.options.kind) !== 'trim' && JSON.stringify(crop) !== JSON.stringify(appliedCrop))
            throw new Error('변경한 크롭을 모든 프레임에 적용한 뒤 저장하세요.');
        const quick = session.options.quickVideo;
        const value = { ...payload(), preview, previewStart: preview && !quick ? nextRetained(current) : 0 };
        if (preview && quick) {
            value.trimStart = clamp(source?.currentTime ?? value.trimStart, value.trimStart, Math.max(value.trimStart, value.trimEnd-2));
            value.trimEnd = Math.min(value.trimEnd, value.trimStart+2);
        }
        if (session.localQuick) {
            const localSession = session;
            if (!quickPrepareRetry) quickPrepareRetry = crypto.randomUUID();
            let prepared = await unwrap(api.prepare({ requestId: quickPrepareRetry, fileId: file.id, quickVideo: true, fullSource: true, start: 0, end: 0, fps: 1000, width: 0, kind: 'gif' }));
            while (['pending', 'preparing'].includes(prepared.state)) {
                if (dead || session !== localSession) return;
                await new Promise(resolve => setTimeout(resolve, 150));
                prepared = api.getSession ? await unwrap(api.getSession({id: prepared.id})) : (await unwrap(api.list())).sessions.find(item => item.id === prepared.id) || prepared;
            }
            if (prepared.state !== 'ready') throw new Error(prepared.error || '원본 영상 정보를 읽지 못했습니다.');
            if (dead || session !== localSession) return;
            quickPrepareRetry = null;
            const previous = session;
            session = prepared;
            if (session.width !== previous.width || session.height !== previous.height) {
                crop = appliedCrop = { x: 0, y: 0, width: session.width, height: session.height };
                throw new Error('원본 크기를 다시 확인했습니다. 크롭 영역을 확인한 뒤 다시 저장하세요.');
            }
            value.sessionId = prepared.id;
            persist();
        }
        const key = JSON.stringify({ value, ranges: deleted.ranges });
        if (!createRetry || createRetry.key !== key)
            createRetry = { key, id: crypto.randomUUID(), selectionId: crypto.randomUUID() };
        const pending = createRetry;
        if (!quick && api.selection) {
            const ranges = deleted.ranges;
            for (let offset = 0; offset < ranges.length || offset === 0; offset += 256) {
                await unwrap(api.selection({ id: pending.selectionId, sessionId: session.id, offset, ranges: ranges.slice(offset, offset + 256), final: offset + 256 >= ranges.length }));
            }
            value.selectionId = pending.selectionId;
        }
        else if (!quick) { // Legacy adapters only have bounded cached sessions.
            value.deleted = keptFrames(session.frameCount, []).filter(n => deleted.has(n));
        }
        const job = await unwrap(api.create({ ...value, requestId: pending.id }));
        if (dead)
            return;
        if (createRetry === pending)
            createRetry = null;
        selectedJob = job.id;
        report(preview ? '화질 확인을 등록했습니다. 최대 2초를 변환하며 갤러리에 등록하지 않습니다.' : '저장 작업을 등록했습니다. 화면을 닫거나 연결이 끊겨도 PC에서 계속 처리합니다.');
        await refresh();
    }
    const save = button('GIF 저장', () => action(() => submit(false)), workspace, 'sbe-primary');
    const history = node('section', undefined, 'sbe-history');
    history.append(node('h3', '편집 자료·작업'));
    content.append(history);
    button('목록 새로고침', () => refresh(), history);
    const sessionList = node('div'), jobList = node('div'), resultBox = node('div', undefined, 'sbe-result');
    history.append(sessionList, jobList, resultBox);
    function useSession(value) {
        const sourceStart = +start.value, sourceEnd = +end.value;
        paintedSession = null;
        stopPlayback();
        paintSerial++;
        pointer = null;
        magnifier.hidden = true;
        magnifierImage = null;
        cropMode = 'adjust';
        drawArea.setAttribute('aria-pressed', 'false');
        adjustArea.setAttribute('aria-pressed', 'true');
        session = value;
        current = 0;
        page = 0;
        selection.clear();
        imageCache.clear();
        frameTimes.clear();
        frameDelays.clear();
        navTarget = 0;
        deleted = new FrameRanges();
        undo = [];
        redo = [];
        crop = value.width ? { x: 0, y: 0, width: value.width, height: value.height } : null;
        appliedCrop = crop ? { ...crop } : null;
        clipStart = 0;
        clipEnd = editableCount(value);
        if (value.state === 'ready' && value.options.quickVideo) {
            clipStart = clamp(Math.round((sourceStart-value.options.start)*1000), 0, editableCount(value)-1);
            clipEnd = clamp(Math.round((sourceEnd-value.options.start)*1000), clipStart+1, editableCount(value));
        }
        if (value.state === 'ready' && value.options.kind !== 'trim') {
            try {
                const draft = JSON.parse(localStorage.getItem(storageKey()) || 'null');
                if (draft && draft.crop && Array.isArray(draft.deleted)) {
                    crop = boundedCrop(draft.crop, value.width, value.height);
                    appliedCrop = boundedCrop(draft.appliedCrop || draft.crop, value.width, value.height);
                    clipStart = clamp(draft.clipStart || 0, 0, editableCount(value) - 1);
                    clipEnd = clamp(draft.clipEnd || editableCount(value), clipStart + 1, editableCount(value));
                    deleted = validRanges(draft.deleted);
                    if (deleted.length >= value.frameCount)
                        deleted.delete(value.frameCount - 1);
                    undo = Array.isArray(draft.undo) ? draft.undo.filter(validSnapshot).slice(-50) : [];
                    redo = Array.isArray(draft.redo) ? draft.redo.filter(validSnapshot).slice(-50) : [];
                }
            }
            catch { /* Corrupt local drafts never change the immutable server frames. */ }
        }
        if (value.state === 'ready' && !value.options.quickVideo && preciseCrop) {
            crop = boundedCrop(preciseCrop.crop, value.width, value.height);
            appliedCrop = boundedCrop(preciseCrop.appliedCrop, value.width, value.height);
            preciseCrop = null;
        }
        title.textContent = value.title + ' · 편집';
        sourceTitle.textContent = value.title;
        if (value.options.kind === 'trim')
            kind.value = 'trim';
        else if (kind.value === 'trim')
            kind.value = 'gif';
        loadQuality();
        if (value.state === 'ready' && (value.options.nativeGIF || value.options.nativeVideo)) {
            void requestTiming(0);
            void requestTiming(value.frameCount - 1);
        }
        if (value.state === 'ready')
            report(value.options.quickVideo ? '남길 구간을 선택한 뒤 저장하세요. 선택한 구간만 변환합니다.' : value.options.kind === 'gif' ? '프레임을 불러왔습니다. 위쪽 “남길 구간” 손잡이로 사용할 부분을 정하고 크롭하거나 프레임을 삭제하세요.' : '영상 준비가 끝났습니다. 남길 구간과 소리 옵션을 확인한 뒤 저장하세요.');
        sync();
    }
    function validSnapshot(value) { return value && value.crop && Array.isArray(value.deleted); }
    function validRanges(value) { const r = new FrameRanges(value); r.ranges = r.ranges.filter(v => v.first < session.frameCount).map(v => ({ ...v, last: Math.min(v.last, session.frameCount - 1) })); return r; }
    const pages = new Map();
    async function ensurePage(n) {
        if (session.renderVersion !== 2 || !api.frames)
            return;
        const id = session.id, offset = Math.floor(n / 48) * 48, key = id + ':' + offset;
        if (pages.has(key))
            return pages.get(key);
        const pending = (async () => {
            let p = await unwrap(api.frameStatus({ id, offset }));
            if (p.state === 'missing')
                p = await unwrap(api.frames({ id, offset }));
            while (['pending', 'running'].includes(p.state)) {
                if (dead || session?.id !== id)
                    throw new Error('편집 화면이 변경되었습니다.');
                await new Promise(resolve => setTimeout(resolve, 600));
                p = await unwrap(api.frameStatus({ id, offset }));
            }
            if ((session?.options.nativeGIF || session?.options.nativeVideo) && !p.times?.length)
                throw new Error('원본 프레임 재생 시간 정보를 읽지 못했습니다. 편집 자료를 다시 준비하세요.');
            cacheTiming(p);
            if (session?.id === id) {
                clipControl.update(current);
                position.textContent = framePosition(current);
            }
            if (p.state !== 'ready')
                throw new Error(p.error || '프레임 준비 실패. 페이지를 다시 열어 재시도하세요.');
        })();
        pages.set(key, pending);
        try {
            await pending;
        }
        finally {
            pages.delete(key);
        }
    }
    async function frameImage(n) {
        const key = session.id + ':' + n;
        if (imageCache.has(key))
            return imageCache.get(key);
        await ensurePage(n);
        const promise = new Promise((resolve, reject) => { const img = new Image(); img.onload = () => resolve(img); img.onerror = () => reject(new Error('프레임 로딩 실패')); img.src = frameURL(session.id, n); });
        imageCache.set(key, promise);
        if (imageCache.size > 8)
            imageCache.delete(imageCache.keys().next().value);
        try {
            return await promise;
        }
        catch (e) {
            imageCache.delete(key);
            throw e;
        }
    }
    function drawPreview(img) {
        if (!appliedCrop)
            return;
        const crop = appliedCrop;
        lastPreview = { sessionId: session.id, image: img };
        const size = fitCanvas(previewCanvas, crop.width, crop.height);
        const density = view.devicePixelRatio || 1;
        const scale = Math.min(1, (img.width || session.width) / session.width, (img.height || session.height) / session.height, size.width * density / crop.width, size.height * density / crop.height);
        previewCanvas.width = Math.max(1, Math.round(crop.width * scale));
        previewCanvas.height = Math.max(1, Math.round(crop.height * scale));
        const ctx = previewCanvas.getContext('2d');
        ctx.drawImage(img, crop.x * (img.width || session.width) / session.width, crop.y * (img.height || session.height) / session.height, crop.width * (img.width || session.width) / session.width, crop.height * (img.height || session.height) / session.height, 0, 0, previewCanvas.width, previewCanvas.height);
    }
    async function paint() {
        if (!session || session.state !== 'ready' || session.options.kind === 'trim')
            return;
        if (session.options.quickVideo && (!quickCrop || !source || source.readyState < 2)) return;
        const serial = ++paintSerial;
        try {
            const img = session.options.quickVideo ? source : await frameImage(current);
            if (dead || serial !== paintSerial)
                return;
            magnifierImage = { sessionId: session.id, image: img };
            stage.width = img.width || Math.min(480, session.width);
            stage.height = img.height || Math.round(stage.width * session.height / session.width);
            fitCanvas(stage, session.width, session.height);
            const ctx = stage.getContext('2d'), sx = stage.width / session.width, sy = stage.height / session.height;
            ctx.clearRect(0, 0, stage.width, stage.height);
            ctx.drawImage(img, 0, 0, stage.width, stage.height);
            paintedSession = session.id;
            if (source) source.hidden = !session.options.quickVideo || quickCrop;
            const x = crop.x * sx, y = crop.y * sy, w = crop.width * sx, h = crop.height * sy;
            ctx.fillStyle = '#0009';
            ctx.beginPath();
            ctx.rect(0, 0, stage.width, stage.height);
            ctx.rect(x, y, w, h);
            ctx.fill('evenodd');
            ctx.strokeStyle = '#7ee7d3';
            ctx.lineWidth = 2;
            ctx.strokeRect(x, y, w, h);
            ctx.fillStyle = '#7ee7d3';
            for (const [hx, hy] of [[x, y], [x + w, y], [x, y + h], [x + w, y + h], [x + w / 2, y], [x + w / 2, y + h], [x, y + h / 2], [x + w, y + h / 2]])
                ctx.fillRect(hx - 4, hy - 4, 8, 8);
            if (!playing)
                drawPreview(img);
            drawMagnifier();
        }
        catch {
            if (!dead && serial === paintSerial)
                report('프레임을 불러오지 못했습니다. 편집 자료와 PC 연결을 확인하세요.', true);
        }
    }
    function cacheTiming(p) {
        if (!p?.times)
            return;
        for (let i = 0; i < p.times.length; i++) {
            frameTimes.set(p.offset + i, p.times[i]);
            frameDelays.set(p.offset + i, p.durations[i]);
        }
    }
    async function requestTiming(n) {
        if (!(session?.options.nativeGIF || session?.options.nativeVideo) || !api.frameStatus || n >= session.frameCount || frameTimes.has(n))
            return;
        const id = session.id;
        try {
            const p = await unwrap(api.frameStatus({ id, offset: n }));
            if (session?.id !== id || dead)
                return;
            cacheTiming(p);
            clipControl.update(current);
            position.textContent = framePosition(current);
        }
        catch { }
    }
    function editableCount(v) { return timeMode(v) ? Math.max(1, Math.ceil((v.options.end - v.options.start) * 1000)) : v?.frameCount || 1; }
    function frameTime(n) {
        if (!session)
            return 0;
        if (timeMode(session))
            return Math.min(session.options.end, session.options.start + n / 1000);
        if (n === session.frameCount)
            return session.options.end;
        if (session.options.nativeGIF || session.options.nativeVideo)
            return frameTimes.get(n) ?? null;
        return session.options.start + n / session.options.fps;
    }
    function frameLabel(n) { const t = frameTime(n); return t === null ? `${n + 1}번 프레임` : formatEditTime(t); }
    function framePosition(n) { return `${n + 1} / ${session.frameCount}장 · ${frameLabel(n)}${deleted.has(n) ? ' · 삭제한 프레임' : ''}`; }
    function nextRetained(n) { const first = deleted.next(Math.max(clipStart, n), clipEnd); return first ?? deleted.next(clipStart, clipEnd); }
    function seekFrame(n) {
        if (!session)
            return;
        stopPlayback();
        current = clamp(n, 0, session.frameCount - 1);
        page = Math.floor(current / 48);
        navTarget = current;
        const time = frameTime(current);
        if (source && time !== null) {
            source.pause();
            try {
                source.currentTime = time;
            }
            catch { }
        }
        sync();
    }
    const rowHeight = 44, maxScrollHeight = 8000000;
    let frameGridKey = '';
    grid.addEventListener('scroll', () => { clearTimeout(navTimer); navTimer = setTimeout(() => void renderFrames(), 20); });
    function navScrollFor(n) {
        const full = session.frameCount * rowHeight;
        if (full <= maxScrollHeight)
            return n * rowHeight;
        return n / Math.max(1, session.frameCount - 1) * (maxScrollHeight - (grid.clientHeight || 320));
    }
    function renderFrames() {
        if (!session || timeMode(session) || session.state !== 'ready')
            return;
        const count = session.frameCount, total = Math.min(count * rowHeight, maxScrollHeight), view = grid.clientHeight || 320;
        if (navTarget !== null) {
            grid.scrollTop = Math.max(0, navScrollFor(navTarget) - view / 2);
            navTarget = null;
        }
        const savedScroll = grid.scrollTop;
        const first = clamp(count * rowHeight <= maxScrollHeight ? Math.floor(grid.scrollTop / rowHeight) : grid.scrollTop / Math.max(1, total - view) * Math.max(0, count - Math.ceil(view / rowHeight)), 0, Math.max(0, count - 1));
        page = Math.floor(first / 48);
        const end = Math.min(count, first + 48), key = session.id + '/' + first + '/' + count;
        if (frameGridKey !== key) {
            frameGridKey = key;
            grid.replaceChildren();
            const top = node('div');
            top.className = 'sbe-frame-spacer';
            top.style.height = Math.max(0, count * rowHeight <= maxScrollHeight ? first * rowHeight : savedScroll) + 'px';
            grid.append(top);
            for (let n = first; n < end; n++) {
                const tile = node('div', undefined, 'sbe-frame');
                tile.dataset.frame = String(n);
                const view = button(`${n + 1}번 프레임`, () => seekFrame(n), tile);
                view.setAttribute('aria-label', `${n + 1}번 프레임 보기`);
                const check = node('input');
                check.type = 'checkbox';
                check.setAttribute('aria-label', `${n + 1}번 프레임 선택`);
                check.title = '프레임 선택 (Shift+클릭: 범위 선택)';
                check.addEventListener('click', event => {
                    if (event.shiftKey)
                        selection.addRange(Math.min(anchor, n), Math.max(anchor, n));
                    else if (check.checked)
                        selection.add(n);
                    else
                        selection.delete(n);
                    anchor = n;
                    seekFrame(n);
                });
                tile.prepend(check);
                grid.append(tile);
            }
            const bottom = node('div');
            bottom.className = 'sbe-frame-spacer';
            bottom.style.height = Math.max(0, total - parseFloat(top.style.height) - (end - first) * rowHeight) + 'px';
            grid.append(bottom);
            grid.scrollTop = savedScroll;
        }
        for (const tile of grid.querySelectorAll('.sbe-frame')) {
            const n = +tile.dataset.frame, check = tile.querySelector('input'), button = tile.querySelector('button');
            tile.className = 'sbe-frame' + (deleted.has(n) ? ' is-deleted' : '') + (n === current ? ' is-current' : '') + (n < clipStart || n >= clipEnd ? ' is-outside' : '');
            check.checked = selection.has(n);
            check.disabled = deleted.has(n);
            if (n === current)
                button.setAttribute('aria-current', 'true');
            else
                button.removeAttribute('aria-current');
        }
        pageText.textContent = `${first + 1}–${end} / ${count}`;
        prevPage.disabled = first === 0;
        nextPage.disabled = end >= count;
    }
    function retainedCount() {
        if (!session)
            return 0;
        let n = clipEnd - clipStart;
        for (const r of deleted.ranges)
            n -= Math.max(0, Math.min(r.last, clipEnd - 1) - Math.max(r.first, clipStart) + 1);
        return n;
    }
    function retainedDuration() {
        if (!session)
            return '';
        if (session.options.nativeGIF || session.options.nativeVideo)
            return '원본 프레임 간격 유지';
        return '약 ' + (retainedCount() / session.options.fps).toFixed(2) + '초';
    }
    function sync() {
        if (dead)
            return;
        fps.disabled = kind.value === 'trim' || sourceGIF || !!session?.options.nativeGIF;
        mute.disabled = kind.value !== 'trim';
        prepare.disabled = busy || !file;
        prepare.textContent = sourceGIF ? '프레임 불러오기' : '프레임 정밀 편집';
        prepare.hidden = !sourceGIF && kind.value === 'trim';
        workspace.hidden = !session;
        clipControl.root.hidden = session?.state !== 'ready';
        const quick = !!session?.options.quickVideo;
        const frameEditing = session?.state === 'ready' && session.options.kind !== 'trim' && !quick;
        const trimMode = (quick ? kind.value : session?.options.kind ?? kind.value) === 'trim';
        const cropEditing = frameEditing || quick && quickCrop && !trimMode;
        const sourceMatches = !session || session.fileId === file?.id;
        sourceSurface.hidden = false;
        if (source) {
            if ((cropEditing || !sourceMatches) && !source.paused) source.pause();
            source.hidden = !sourceMatches || cropEditing && paintedSession === session.id;
        }
        sourceActions.hidden = !source || !sourceMatches || !!session && !timeMode(session);
        quickCropToggle.hidden = !quick || trimMode;
        quickCropToggle.textContent = quickCrop ? '영상으로 돌아가기' : '크롭 영역 조절';
        quickCropToggle.setAttribute('aria-pressed', String(quickCrop));
        cropSurface.hidden = !cropEditing;
        cropPlaceholder.hidden = frameEditing || Boolean(source) && sourceMatches || trimMode;
        frameAside.hidden = !frameEditing;
        editorBody.classList.toggle('has-frames', Boolean(frameEditing));
        frameShortcutGroup.hidden = !frameEditing;
        rangeShortcutGroup.hidden = !(session?.state === 'ready' || !setup.hidden && !sourceGIF);
        shortcutMode.textContent = !session || session.state !== 'ready' ? '편집 자료를 열고 준비가 끝나면 해당 모드의 단축키가 표시됩니다.' : frameEditing ? 'GIF·WebP 프레임 편집에서 사용할 수 있는 단축키입니다.' : '동영상 자르기에서는 구간 손잡이와 공통 단축키를 사용할 수 있습니다.';
        sourceRange.root.hidden = !file || sourceGIF || !!session;
        sourceRange.update();
        if (!session)
            return;
        const gif = quick ? kind.value !== 'trim' : session.options.kind !== 'trim', ready = session.state === 'ready';
        clipControl.root.hidden = !ready;
        sourceRange.root.hidden = true;
        gifWorkspace.hidden = !frameEditing;
        muteLabel.hidden = gif || !ready;
        mute.disabled = !ready;
        summary.textContent = quick ? `${session.title} · 선택 구간 ${(frameTime(clipEnd)-frameTime(clipStart)).toFixed(2)}초` : `${session.title} · ${session.cancelling ? '취소 중' : labels[session.state] || session.state} ${session.progress}%` + (ready && gif ? ` · ${session.frameCount}장 중 ${retainedCount()}장 남음 · ${retainedDuration()} · ${crop?.width || session.width}×${crop?.height || session.height}px` : '') + (session.error ? ` · ${session.error}` : '');
        save.textContent = gif ? (kind.value === 'webp' ? 'WebP 저장' : 'GIF 저장') : '잘라낸 영상 저장';
        qualityBox.hidden = !gif || !ready;
        sample.disabled = busy || !ready;
        if (ready && !resourceBox.childNodes.length)
            scheduleResources();
        save.disabled = busy || !ready || !quick && (kind.value === 'trim') !== (session.options.kind === 'trim');
        sample.disabled = save.disabled;
        resourceBox.hidden = !!session.localQuick;
        clipControl.update(current);
        if (ready && quick) {
            start.value = String(frameTime(clipStart)); end.value = String(frameTime(clipEnd));
            applyCrop.textContent = '크롭 적용';
            for (const key of Object.keys(cropInputs)) {
                cropInputs[key].max = String(key === 'x' || key === 'width' ? session.width : session.height);
                if (doc.activeElement !== cropInputs[key]) cropInputs[key].value = String(crop[key]);
            }
            cropState.textContent = JSON.stringify(crop) === JSON.stringify(appliedCrop) ? `${appliedCrop.width}×${appliedCrop.height}px 적용됨` : '크롭 변경 후 크롭 적용을 누르세요.';
            qualityHint.textContent = '선택 구간을 원본 프레임으로 변환합니다. 화질 확인은 현재 위치 근처에서 최대 2초만 처리합니다.';
            if (quickCrop) void paint();
            return;
        }
        applyCrop.textContent = '모든 프레임에 크롭 적용';
        if (ready && gif) {
            if (!crop)
                crop = { x: 0, y: 0, width: session.width, height: session.height };
            for (const key of Object.keys(cropInputs)) {
                cropInputs[key].max = String(key === 'x' || key === 'width' ? session.width : session.height);
            }
            rangeFrom.max = rangeTo.max = String(session.frameCount);
            for (const key of Object.keys(cropInputs))
                if (doc.activeElement !== cropInputs[key])
                    cropInputs[key].value = String(crop[key]);
            frameSlider.max = String(session.frameCount - 1);
            frameSlider.value = String(current);
            position.textContent = framePosition(current);
            clipControl.update(current);
            cropState.textContent = JSON.stringify(crop) === JSON.stringify(appliedCrop) ? `모든 프레임에 ${appliedCrop?.width}×${appliedCrop?.height}px 적용됨` : '크롭 변경 후 모든 프레임에 적용을 누르세요.';
            previousFrame.disabled = current === 0;
            nextFrame.disabled = current === session.frameCount - 1;
            deleteButton.disabled = !selection.size;
            undoButton.disabled = !undo.length;
            redoButton.disabled = !redo.length;
            renderFrames();
            void paint();
        }
    }
    function point(event) { const rect = stage.getBoundingClientRect(); return { x: clamp((event.clientX - rect.left) / rect.width * session.width, 0, session.width), y: clamp((event.clientY - rect.top) / rect.height * session.height, 0, session.height) }; }
    function cropHit(p, rect) {
        const sx = rect.width / session.width, sy = rect.height / session.height;
        let mode = null, nearest = Infinity;
        // Corners take precedence over edges; both resize in either tool mode.
        for (const [x, hx] of [[crop.x, 'l'], [crop.x + crop.width, 'r']])
            for (const [y, hy] of [[crop.y, 't'], [crop.y + crop.height, 'b']]) {
                const distance = Math.hypot((p.x - x) * sx, (p.y - y) * sy);
                if (distance <= 16 && distance < nearest) { mode = hx + hy; nearest = distance; }
            }
        if (mode) return mode;
        const vertical = p.y >= crop.y && p.y <= crop.y + crop.height;
        const horizontal = p.x >= crop.x && p.x <= crop.x + crop.width;
        for (const [edge, distance] of [
            ['l', vertical ? Math.abs(p.x - crop.x) * sx : Infinity],
            ['r', vertical ? Math.abs(p.x - crop.x - crop.width) * sx : Infinity],
            ['t', horizontal ? Math.abs(p.y - crop.y) * sy : Infinity],
            ['b', horizontal ? Math.abs(p.y - crop.y - crop.height) * sy : Infinity],
        ]) if (distance <= 16 && distance < nearest) { mode = edge; nearest = distance; }
        if (mode) return mode;
        if (cropMode === 'draw') return 'draw';
        return p.x > crop.x && p.x < crop.x + crop.width && p.y > crop.y && p.y < crop.y + crop.height ? 'move' : null;
    }
    const cropCursor = mode => ({ lt: 'nwse-resize', rb: 'nwse-resize', rt: 'nesw-resize', lb: 'nesw-resize', l: 'ew-resize', r: 'ew-resize', t: 'ns-resize', b: 'ns-resize', move: 'move', draw: 'crosshair' }[mode] || 'default');
    stage.addEventListener('pointerdown', event => {
        if (!session || !crop || pointer || event.button !== 0)
            return;
        stopPlayback();
        const p = point(event), rect = stage.getBoundingClientRect();
        const mode = cropHit(p, rect);
        if (!mode) return;
        stage.style.cursor = cropCursor(mode);
        pointer = { id: event.pointerId, p, before: snapshot(), mode, clientX: event.clientX, clientY: event.clientY };
        stage.setPointerCapture(event.pointerId);
        drawMagnifier();
        event.preventDefault();
    });
    stage.addEventListener('pointermove', event => {
        if (!session || !crop) return;
        if (!pointer) { stage.style.cursor = cropCursor(cropHit(point(event), stage.getBoundingClientRect())); return; }
        if (pointer.id !== event.pointerId)
            return;
        pointer.clientX = event.clientX;
        pointer.clientY = event.clientY;
        const p = point(event), old = pointer.before.crop;
        if (pointer.mode === 'move')
            crop = boundedCrop({ ...old, x: old.x + p.x - pointer.p.x, y: old.y + p.y - pointer.p.y }, session.width, session.height);
        else if (pointer.mode === 'draw') {
            crop = boundedCrop({ x: Math.min(pointer.p.x, p.x), y: Math.min(pointer.p.y, p.y), width: Math.max(1, Math.abs(p.x - pointer.p.x)), height: Math.max(1, Math.abs(p.y - pointer.p.y)) }, session.width, session.height);
        } else {
            // Move only the grabbed edges, preserving their opposite sides and grab offset.
            const dx = p.x - pointer.p.x, dy = p.y - pointer.p.y;
            const left = pointer.mode.includes('l') ? clamp(old.x + dx, 0, session.width) : old.x;
            const right = pointer.mode.includes('r') ? clamp(old.x + old.width + dx, 0, session.width) : old.x + old.width;
            const top = pointer.mode.includes('t') ? clamp(old.y + dy, 0, session.height) : old.y;
            const bottom = pointer.mode.includes('b') ? clamp(old.y + old.height + dy, 0, session.height) : old.y + old.height;
            crop = boundedCrop({ x: Math.min(left, right), y: Math.min(top, bottom), width: Math.max(1, Math.abs(right - left)), height: Math.max(1, Math.abs(bottom - top)) }, session.width, session.height);
        }
        for (const key of Object.keys(cropInputs))
            cropInputs[key].value = String(crop[key]);
        drawMagnifier();
        void paint();
    });
    stage.addEventListener('pointerleave', () => { if (!pointer) stage.style.cursor = cropCursor(cropMode); });
    function finishPointer(event) {
        if (!pointer || pointer.id !== event.pointerId)
            return;
        const before = pointer.before;
        pointer = null;
        magnifier.hidden = true;
        if (JSON.stringify(before.crop) !== JSON.stringify(crop)) {
            cropMode = 'adjust';
            drawArea.setAttribute('aria-pressed', 'false');
            adjustArea.setAttribute('aria-pressed', 'true');
            remember(before);
        }
    }
    stage.addEventListener('pointerup', finishPointer);
    function cancelCropPointer(event) {
        if (!pointer || event.pointerId !== undefined && event.pointerId !== pointer.id)
            return;
        crop = pointer.before.crop;
        pointer = null;
        magnifier.hidden = true;
        sync();
    }
    stage.addEventListener('pointercancel', cancelCropPointer);
    stage.addEventListener('lostpointercapture', cancelCropPointer);
    view.addEventListener('blur', cancelCropPointer);
    dialog.addEventListener('keydown', event => {
        if (event.key === 'Escape' && !shortcutsPanel.hidden) {
            event.preventDefault(); event.stopPropagation(); showShortcuts(false); return;
        }
        if (shortcutsPanel.contains(event.target) || !session || session.state !== 'ready' || timeMode(session) || ['INPUT', 'SELECT', 'TEXTAREA'].includes(event.target.tagName))
            return;
        if (event.key === 'ArrowLeft') {
            event.preventDefault();
            previousFrame.click();
        }
        if (event.key === 'ArrowRight') {
            event.preventDefault();
            nextFrame.click();
        }
        if (event.key === 'Delete') {
            event.preventDefault();
            deleteButton.click();
        }
        if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'z') {
            event.preventDefault();
            (event.shiftKey ? redoButton : undoButton).click();
        }
    });
    let historyKey = '';
    function renderHistory() {
        const key = JSON.stringify({ sessions, jobs });
        if (key === historyKey) {
            renderResult();
            return;
        }
        historyKey = key;
        sessionList.replaceChildren();
        jobList.replaceChildren();
        for (const item of sessions) {
            const row = node('div', undefined, 'sbe-record');
            row.append(node('span', `${item.title} · ${item.options.kind === 'gif' ? 'GIF' : '영상'} · ${labels[item.state] || item.state} ${item.progress}%${item.cancelling ? ' · 취소 중' : ''}`));
            button('열기', () => {
                useSession(item);
                mainPane.scrollTop = 0;
                const target = item.state === 'ready' && item.options.kind !== 'trim' ? stage : closeEditor;
                target.focus({ preventScroll: true });
            }, row);
            if (active(item.state))
                button('취소', () => action(async () => { await unwrap(api.cancel({ id: item.id })); await refresh(); }), row);
            else
                button('자료 정리', () => action(async () => {
                    if (!confirm('이 편집 자료와 브라우저의 초안을 정리할까요? 원본과 저장된 결과는 유지됩니다.'))
                        return;
                    await unwrap(api.release({ id: item.id }));
                    try {
                        localStorage.removeItem(`storagebucket-edit-v1:${item.id}`);
                    }
                    catch { }
                    if (session?.id === item.id) {
                        stopPlayback();
                        session = null;
                    }
                    await refresh();
                    sync();
                }), row);
            sessionList.append(row);
        }
        for (const job of jobs) {
            const row = node('div', undefined, 'sbe-record');
            row.append(node('span', `${(job.preview ? '화질 확인 · ' : '') + (job.options.kind === 'webp' ? 'WebP' : job.options.kind === 'gif' ? 'GIF' : '영상')} · ${new Date(job.createdAt).toLocaleString('ko-KR')} · ${job.cancelling ? '취소 중' : labels[job.state] || job.state} ${job.progress}%${job.error ? ' · ' + job.error : ''}`));
            if (job.resources?.known)
                row.append(node('span', `인코더 RAM ${bytes(job.resources.currentBytes)} · 관측 최대 ${bytes(job.resources.observedPeakBytes)}`));
            if (active(job.state) && job.state !== 'publishing')
                button('취소', () => action(async () => { await unwrap(api.cancel({ id: job.id })); await refresh(); }), row);
            if (job.state === 'completed' && (job.resultFileId || job.preview))
                button('결과 보기', () => { selectedJob = job.id; renderResult(); }, row);
            if (['failed', 'cancelled', 'interrupted'].includes(job.state))
                button('같은 설정으로 다시 실행', () => action(async () => { const next = await unwrap(api.create({ ...job.options, requestId: crypto.randomUUID() })); selectedJob = next.id; await refresh(); }), row);
            jobList.append(row);
        }
        if (!sessions.length && !jobs.length)
            sessionList.append(node('p', '편집 자료와 작업이 없습니다.'));
        renderResult();
    }
    let shownResult = '';
    function renderResult() {
        const job = jobs.find(j => j.id === selectedJob && j.state === 'completed' && (j.resultFileId || j.preview));
        const id = job?.id || '';
        if (shownResult === id)
            return;
        shownResult = id;
        for (const video of resultBox.querySelectorAll('video')) {
            video.pause();
            video.removeAttribute('src');
            video.load();
        }
        resultBox.replaceChildren();
        if (!job)
            return;
        const media = node(job.options.kind !== 'trim' ? 'img' : 'video');
        if (job.options.kind !== 'trim')
            media.alt = job.preview ? '실제 화질 미리보기' : '생성한 애니메이션';
        else {
            media.controls = true;
            media.playsInline = true;
        }
        media.src = resultURL(job.id);
        resultBox.append(media);
        const link = node('a', '결과 다운로드');
        link.href = resultURL(job.id) + '?download=1';
        link.download = '';
        resultBox.append(link);
        if (options.reveal && !job.preview)
            button('PC 파일 위치', () => action(() => options.reveal(job.id)), resultBox);
    }
    async function refresh() {
        if (dead || reading)
            return;
        reading = true;
        try {
            const value = await unwrap(api.list());
            if (dead)
                return;
            sessions = value.sessions;
            jobs = value.jobs;
            if (session) {
                const latest = sessions.find(item => item.id === session.id);
                if (latest) {
                    if (session.state !== 'ready' && latest.state === 'ready')
                        useSession(latest);
                    else
                        session = latest;
                }
            }
            renderHistory();
            sync();
            if (session?.state === 'ready')
                void updateResources();
        }
        catch (e) {
            if (!dead)
                report(e.message || 'PC에 연결하지 못했습니다. 연결되면 작업 상태를 다시 확인합니다.', true);
        }
        finally {
            reading = false;
            if (!dead) {
                clearTimeout(pollTimer);
                pollTimer = setTimeout(refresh, 2000);
            }
        }
    }
    kind.addEventListener('change', () => { loadQuality(); sync(); });
    dialog.addEventListener('close', () => { persist(); destroy(); onClose?.(); });
    function destroy() {
        if (dead)
            return;
        dead = true;
        paintSerial++;
        stopPlayback();
        clearTimeout(pollTimer);
        clearTimeout(resourceTimer);
        clearTimeout(navTimer);
        view.removeEventListener('resize', scheduleLayout);
        resizeObserver?.disconnect();
        if (layoutFrame !== null)
            view.cancelAnimationFrame(layoutFrame);
        layoutFrame = null;
        lastPreview = null;
        magnifier.hidden = true;
        magnifierImage = null;
        view.removeEventListener('blur', cancelCropPointer);
        resourceSerial++;
        imageCache.clear();
        if (source) {
            source.pause();
            source.removeAttribute('src');
            source.load();
        }
        for (const video of resultBox.querySelectorAll('video')) {
            video.pause();
            video.removeAttribute('src');
            video.load();
        }
        dialog.remove();
    }
    if (typeof dialog.showModal === 'function')
        dialog.showModal();
    else
        dialog.setAttribute('open', '');
    loadQuality();
    sync();
    void refresh();
    return { destroy };
}
