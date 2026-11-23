import { translate, bindText, bindAttribute, localizeWidget, getLocale, translateError, translateMessage } from "./i18n.js"
// Shared by the React desktop launcher and the mobile web page. The server owns
// immutable frame IDs; this UI sends crop coordinates and deleted IDs only.
const labels = { get pending() { return translate("대기 중") }, get preparing() { return translate("프레임 준비 중") }, get ready() { return translate("편집 가능") }, get running() { return translate("변환 중") }, get verifying() { return translate("결과 확인 중") }, get publishing() { return translate("저장 중") }, get completed() { return translate("완료") }, get failed() { return translate("실패") }, get cancelled() { return translate("취소됨") }, get interrupted() { return translate("중단됨") }, get released() { return translate("정리됨") }, get stop_unconfirmed() { return translate("앱 재시작 필요") } };
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
    removeRange(first, last) { this.ranges = this.ranges.flatMap(r => last < r.first || first > r.last ? [r] : [...(first > r.first ? [{ first: r.first, last: first - 1 }] : []), ...(last < r.last ? [{ first: last + 1, last: r.last }] : [])]); }
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
    const readLabel = typeof label === 'function' ? label : () => label;
    root.className = 'sbe-trim-range';
    bindAttribute(root, 'aria-label', readLabel);
    const heading = doc.createElement('div');
    heading.className = 'sbe-range-heading';
    const title = doc.createElement('strong');
    bindText(title, readLabel);
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
        bindAttribute(handle, 'aria-label', () => readLabel() + ' ' + (side === 'start' ? translate("시작") : translate("종료")) + translate(" 손잡이"));
        bindAttribute(handle, 'title', () => handle.getAttribute('aria-label') + translate(" (←·↓ / →·↑: 한 칸, Home / End: 처음·끝)"));
        handle.setAttribute('aria-keyshortcuts', 'ArrowLeft ArrowDown ArrowRight ArrowUp Home End');
        const grip = doc.createElement('span');
        bindText(grip, () => 'Ⅱ');
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
        bindText(value, () => format(start) + ' – ' + format(end));
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
        if (typeof text === 'function') bindText(el, text);
        else if (text !== undefined)
            bindText(el, () => text);
        if (className)
            el.className = className;
        return el;
    }
    function button(text, action, parent, className = '') { const el = node('button', text, className); el.type = 'button'; el.addEventListener('click', event => void action(event)); parent?.append(el); return el; }
    const dialog = node('dialog', undefined, 'sb-editor');
    bindAttribute(dialog, 'aria-label', () => translate("GIF 만들기·동영상 자르기"));
    host.append(dialog);
    const heading = node('header', undefined, 'sbe-heading');
    const title = node('h2', () => (translate("GIF 만들기·동영상 자르기")));
    heading.append(title);
    const headingActions = node('div', undefined, 'sbe-heading-actions');
    heading.append(headingActions);
    const shortcutToggle = button(() => (translate("단축키 안내")), () => showShortcuts(shortcutsPanel.hidden), headingActions, 'sbe-shortcut-toggle');
    const keyboardIcon = doc.createElementNS('http://www.w3.org/2000/svg', 'svg');
    keyboardIcon.setAttribute('viewBox', '0 0 24 24'); keyboardIcon.setAttribute('width', '18'); keyboardIcon.setAttribute('height', '18');
    keyboardIcon.setAttribute('fill', 'none'); keyboardIcon.setAttribute('stroke', 'currentColor'); keyboardIcon.setAttribute('stroke-width', '1.7'); keyboardIcon.setAttribute('aria-hidden', 'true');
    const keyboardPath = doc.createElementNS('http://www.w3.org/2000/svg', 'path');
    keyboardPath.setAttribute('d', 'M4 5h16a1 1 0 0 1 1 1v12a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1V6a1 1 0 0 1 1-1ZM6 9h2m3 0h2m3 0h2M6 12h2m3 0h2m3 0h2M7 16h10');
    keyboardIcon.append(keyboardPath); shortcutToggle.prepend(keyboardIcon);
    const closeEditor = button(() => (translate("닫기")), () => dialog.close(), headingActions);
    bindAttribute(closeEditor, 'title', () => translate("편집기 닫기 (Esc)"));
    dialog.append(heading);
    const shortcutsPanel = node('section', undefined, 'sbe-shortcuts');
    shortcutsPanel.id = 'sbe-shortcuts-' + crypto.randomUUID(); shortcutsPanel.hidden = true; shortcutsPanel.tabIndex = -1;
    shortcutsPanel.setAttribute('role', 'region'); bindAttribute(shortcutsPanel, 'aria-label', () => translate("편집 단축키 안내"));
    shortcutToggle.setAttribute('aria-controls', shortcutsPanel.id); shortcutToggle.setAttribute('aria-expanded', 'false');
    const shortcutHeading = node('div', undefined, 'sbe-shortcuts-heading');
    shortcutHeading.append(node('h3', () => (translate("단축키 안내"))));
    button(() => (translate("단축키 안내 닫기")), () => showShortcuts(false), shortcutHeading);
    shortcutsPanel.append(shortcutHeading);
    const shortcutMode = node('p', '', 'sbe-hint'); shortcutsPanel.append(shortcutMode);
    const frameShortcuts = [
        { get label() { return translate("이전 프레임") }, keys: '←', aria: 'ArrowLeft' },
        { get label() { return translate("다음 프레임") }, keys: '→', aria: 'ArrowRight' },
        { get label() { return translate("선택 프레임 삭제") }, keys: 'Delete', aria: 'Delete' },
        { get label() { return translate("실행 취소") }, keys: 'Ctrl+Z / ⌘+Z', aria: 'Control+z Meta+z' },
        { get label() { return translate("다시 실행") }, keys: 'Ctrl+Shift+Z / ⌘+Shift+Z', aria: 'Control+Shift+z Meta+Shift+z' },
    ];
    function shortcutGroup(label, rows, hint) {
        const group = node('section', undefined, 'sbe-shortcut-group');
        group.append(node('h4', label));
        const list = node('dl');
        for (const row of rows) {
            const value = node('dd'); value.append(node('kbd', () => (row.keys)));
            list.append(node('dt', () => (row.label)), value);
        }
        group.append(list);
        if (hint) group.append(node('p', hint, 'sbe-hint'));
        shortcutsPanel.append(group);
        return group;
    }
    const frameShortcutGroup = shortcutGroup(() => (translate("프레임 편집")), [...frameShortcuts, { get label() { return translate("프레임 범위 선택") }, get keys() { return translate("Shift+클릭") } }, { get label() { return translate("개별 선택 추가·해제") }, get keys() { return translate("Ctrl+클릭 / ⌘+클릭") } }, { get label() { return translate("연속 프레임 선택") }, get keys() { return translate("드래그") } }], () => (translate("프레임 행 또는 체크박스에서 선택합니다. Ctrl+Shift+클릭은 기존 선택에 범위를 추가합니다. 드래그 중 Esc는 선택을 되돌립니다. 입력창에서는 입력창의 기본 동작을 우선합니다.")));
    const rangeShortcutGroup = shortcutGroup(() => (translate("구간 손잡이")), [
        { get label() { return translate("이전 값으로 한 칸 이동") }, keys: '← / ↓' },
        { get label() { return translate("다음 값으로 한 칸 이동") }, keys: '→ / ↑' },
        { get label() { return translate("허용 범위의 처음·끝으로 이동") }, keys: 'Home / End' },
    ], () => (translate("시작 또는 종료 손잡이에 초점을 맞춘 뒤 사용합니다. 손잡이에 초점이 있으면 방향키로 구간 경계를 조절합니다.")));
    shortcutGroup(() => (translate("공통")), [
        { get label() { return translate("다음·이전 조작 항목으로 이동") }, keys: 'Tab / Shift+Tab' },
        { get label() { return translate("단축키 안내 또는 편집기 닫기") }, keys: 'Esc' },
    ], () => (translate("안내가 열려 있으면 Esc로 안내를 먼저 닫습니다.")));
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
        bindAttribute(control, 'title', () => `${shortcut.label} (${shortcut.keys})`);
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
    let dead = false, busy = false, session = null, selection = new FrameRanges(), current = 0, page = 0, anchor = null;
    let frameSelectionBase = new FrameRanges(), framePointer = null, frameScroll = null, suppressFrameClick = false, frameClickTimer;
    let crop = null, appliedCrop = null, clipStart = 0, clipEnd = 0, deleted = new FrameRanges(), undo = [], redo = [], playing = false, playbackVersion = 0, playTimer, pollTimer, reading = false;
    let prepareRetry = null, createRetry = null, sessions = [], jobs = [], selectedJob = null, pointer = null, paintSerial = 0;
    const imageCache = new Map(), frameTimes = new Map(), frameDelays = new Map();
    let navTarget = null, navTimer;
    const frameURL = options.frameURL || ((id, n, thumbnail = false) => `/edit-assets/sessions/${id}/${n}${thumbnail ? '?thumbnail=1' : ''}`);
    const resultURL = options.resultURL || (id => `/edit-assets/jobs/${id}`);
    function report(text, error = false) {
        if (dead)
            return;
        bindText(message, () => translateMessage(text));
        message.setAttribute('role', error ? 'alert' : 'status');
    }
    async function unwrap(promise) {
        let timer;
        try {
            const timeout = new Promise((_, reject) => { timer = setTimeout(() => reject(new Error(translate("요청 응답을 받지 못했습니다. PC 작업 목록을 확인한 뒤 다시 요청하세요."))), 15000); });
            const result = await Promise.race([promise, timeout]);
            if (!result.ok) {
                const e = new Error(translateError(result.error));
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
        report(translate("처리 중…"));
        try {
            await work();
        }
        catch (e) {
            report(e.message || translate("요청 결과를 받지 못했습니다. 작업 목록에서 확인하거나 다시 요청하세요."), true);
        }
        finally {
            busy = false;
            if (!dead)
                sync();
        }
    }
    function stopPlayback() { playing = false; playbackVersion++; clearTimeout(playTimer); bindText(play, () => translate("편집 결과 재생")); }
    const storageKey = () => `storagebucket-edit-v1:${session.id}`;
    function persist() {
        if (!session || !crop)
            return;
        try {
            localStorage.setItem(storageKey(), JSON.stringify({ crop, appliedCrop, clipStart, clipEnd, deleted, undo, redo }));
        }
        catch {
            report(translate("브라우저 저장 공간이 부족하여 편집 초안을 보관하지 못했습니다."), true);
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
        stopFrameDrag(); anchor = null; frameSelectionBase.clear();
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
    const sourceTitle = node('h3', () => (file?.title || translate("편집 미리보기")));
    sourceSurface.append(sourceTitle);
    content.append(sourceSurface, setup);
    let source = null, paintedSession = null, quickCrop = false, preciseCrop = null, quickPrepareRetry = null;
    const timeMode = value => value?.options.quickVideo || value?.options.kind === 'trim';
    if (file?.src && !sourceGIF) {
        source = node('video');
        source.preload = 'metadata';
        source.src = file.src;
        source.controls = true;
        source.playsInline = true;
        source.className = 'sbe-source';
        sourceSurface.append(source);
        source.addEventListener('error', () => report(translate("이 브라우저는 원본을 재생하지 못합니다. 프레임 정밀 편집에서 편집용 미리보기로 확인할 수 있습니다.")));
    }
    const cropPlaceholder = node('p', () => (file ? translate("원본 영상을 읽고 있습니다. 재생 정보가 준비되면 바로 구간을 선택할 수 있습니다.") : translate("영상 카드의 ⋯ 메뉴에서 “GIF, WebP로 만들기”를 선택해 주세요. 기존 편집 자료는 아래에서 다시 열 수 있습니다.")), 'sbe-crop-placeholder');
    sourceSurface.append(cropPlaceholder);
    const overview = node('section', undefined, 'sbe-overview');
    bindAttribute(overview, 'aria-label', () => translate("전체 영상 미리보기"));
    overview.append(node('h3', () => translate("전체 영상 미리보기")));
    const overviewStatus = node('p', () => translate("대표 화면을 먼저 표시합니다. 선택한 구간만 정밀 프레임으로 불러옵니다."), 'sbe-hint');
    const overviewGrid = node('div', undefined, 'sbe-overview-grid');
    overview.append(overviewStatus, overviewGrid);
    sourceSurface.append(overview);
    let overviewDuration = file?.duration || 0, overviewFrameCount = 0;
    function selectOverview(index) {
        if (dead || busy || session && (!timeMode(session) || session.fileId !== file?.id)) return;
        const duration = overviewDuration || source?.duration;
        if (!Number.isFinite(duration) || duration <= 0) return;
        const a = duration * index / 12;
        const span = Math.min(duration / 12, overviewFrameCount > 0 ? 1000 * duration / overviewFrameCount : duration / 12);
        const b = Math.min(duration, a + span);
        start.value = String(a); end.value = String(b); sourceRangeTouched = true;
        if (session?.state === 'ready' && timeMode(session)) {
            clipStart = clamp(Math.round((a-session.options.start)*1000), 0, editableCount(session)-1);
            clipEnd = clamp(Math.round((b-session.options.start)*1000), clipStart+1, editableCount(session));
            createRetry = null;
        }
        if (source) { source.pause(); try { source.currentTime = a; } catch {} }
        sync();
    }
    async function loadOverview() {
        if (!source || !api.overview) return;
        const tiles = Array.from({length:12}, (_, index) => {
            const tile = button(() => formatEditTime(overviewDuration * index / 12), () => selectOverview(index), overviewGrid);
            tile.className = 'sbe-overview-frame';
            bindAttribute(tile, 'aria-label', () => translate("{0} 구간 선택", {"0":formatEditTime(overviewDuration * index / 12)}));
            const image = node('img'); image.hidden = true; image.draggable = false; image.alt = '';
            tile.prepend(image);
            return {tile,image};
        });
        // Show the beginning, end and middle first; each response updates one
        // tile without waiting for all frames, a session, or an encode worker.
        for (const index of [0,11,6,3,9,1,2,4,5,7,8,10]) {
            if (dead || session && !timeMode(session)) return;
            try {
                const value = await unwrap(api.overview({fileId:file.id,index}));
                if (dead || session && !timeMode(session)) return;
                overviewDuration = value.duration; overviewFrameCount = value.frameCount;
                if (!value.image?.startsWith('data:image/jpeg;base64,')) throw new Error('Invalid overview image');
                tiles[index].image.src = value.image; tiles[index].image.hidden = false;
                // Rebind only captions; replacing tile contents would discard images.
                for (const [n, {tile}] of tiles.entries()) {
                    const text = [...tile.childNodes].find(child => child.nodeType === 3);
                    if (text) text.textContent = formatEditTime(overviewDuration * n / 12);
                    bindAttribute(tile, 'aria-label', () => translate("{0} 구간 선택", {"0":formatEditTime(overviewDuration * n / 12)}));
                }
            } catch {
                if (dead) return;
                tiles[index].tile.classList.add('is-unavailable');
            }
        }
    }
    const fields = node('div', undefined, 'sbe-fields');
    setup.append(fields);
    function field(label, element, parent = fields) { const wrap = node('label', label); wrap.append(element); parent.append(wrap); return element; }
    function numeric(label, value, min, max, step = 1, parent = fields) { const el = node('input'); el.type = 'number'; el.value = String(value); el.min = String(min); el.max = String(max); el.step = String(step); return field(label, el, parent); }
    const kind = node('select');
    for (const [value, text] of [['gif', () => translate("GIF 만들기")], ['webp', () => translate("animated WebP 만들기")], ['trim', () => translate("동영상 자르기")]]) {
        const o = node('option', text);
        o.value = value;
        kind.append(o);
    }
    field(() => (translate("작업 종류")), kind);
    const start = numeric(() => (translate("시작 시간(초)")), 0, 0, file?.duration || 86400, .01);
    const end = numeric(() => (translate("종료 시간(초)")), file?.duration || 3, .01, file?.duration || 86400, .01);
    const fps = numeric(() => (translate("애니메이션 FPS")), 10, 1, Number.MAX_SAFE_INTEGER);
    start.parentElement.hidden = end.parentElement.hidden = fps.parentElement.hidden = true;
    if (sourceGIF) {
        fps.parentElement.hidden = true;
        kind.querySelector('option[value=trim]').disabled = true;
    }
    const mute = node('input');
    let sourceRangeTouched = false;
    mute.type = 'checkbox';
    field(() => (translate("영상 소리 제거")), mute);
    const sourceRange = mountRangeControl(setup, { label: () => translate("원본 구간 선택"), count: () => Math.max(1, Math.round((Number.isFinite(source?.duration) && source.duration > 0 ? source.duration : file?.duration || 3) * 100)), values: () => [Math.round(+start.value * 100), Math.round(+end.value * 100)], format: n => formatEditTime(n / 100), change: (a, b, side) => {
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
                useSession({ id: crypto.randomUUID(), localQuick: true, fileId: file.id, title: file.title || translate("영상"), state: 'ready', width: source.videoWidth, height: source.videoHeight, frameCount: 0, progress: 100,
                    options: { fileId: file.id, quickVideo: true, fullSource: true, kind: 'gif', start: 0, end: source.duration, fps: 1000 } });
            }
        }
    });
    source?.addEventListener('seeked', () => { if (session?.options.quickVideo && quickCrop) void paint(); });
    source?.addEventListener('loadeddata', () => { if (session?.options.quickVideo && quickCrop) void paint(); });
    if (sourceGIF) {
        start.parentElement.hidden = end.parentElement.hidden = true;
        setup.append(node('p', () => (translate("GIF는 원본 프레임과 재생 간격을 유지합니다. 프레임 준비 후 남길 구간을 정하세요.")), 'sbe-hint'));
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
        button(() => (translate("현재 위치를 시작으로")), () => setSourceBound('start'), sourceActions);
        button(() => (translate("현재 위치를 종료로")), () => setSourceBound('end'), sourceActions);
        let segmentEnd = null;
        button(() => (translate("선택 구간 재생")), () => {
            if (+end.value <= +start.value)
                return report(translate("종료 시간은 시작 시간보다 뒤여야 합니다."), true);
            segmentEnd = +end.value;
            source.currentTime = +start.value;
            source.play().catch(() => report(translate("원본 구간을 재생하지 못했습니다."), true));
        }, sourceActions);
        source.addEventListener('timeupdate', () => {
            if (segmentEnd !== null && source.currentTime >= segmentEnd) {
                source.currentTime = +start.value;
            }
        });
        source.addEventListener('pause', () => { segmentEnd = null; });
    }
    setup.append(node('p', () => (translate("원본 영상의 구간을 바로 선택하고 저장할 수 있습니다. 프레임별 삭제는 선택 구간의 “프레임 정밀 편집”을 여세요.")), 'sbe-hint'));
    const prepare = button(() => (translate("프레임 정밀 편집")), () => action(async () => {
        if (!file)
            return;
        const bounded = !sourceGIF && (Number.isFinite(source?.duration) && source.duration > 0 || file.duration > 0);
        const req = { fileId: file.id, fullSource: !bounded, ...(bounded ? { originalFrames: true } : {}), start: bounded ? session?.state === 'ready' ? frameTime(clipStart) ?? +start.value : +start.value : 0, end: bounded ? session?.state === 'ready' ? frameTime(clipEnd) ?? +end.value : +end.value : 0, fps: 1000, width: 0, kind: 'gif' };
        if (session && !session.options.quickVideo && (deleted.length || undo.length) && !confirm(translate("새 프레임을 준비할까요? 현재 편집 초안은 기존 편집 자료에서 다시 열 수 있습니다.")))
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
        report(translate("선택 구간의 프레임을 준비하고 있습니다. 아래 진행 상태를 확인해 주세요."));
        await refresh();
    }), setupActions, 'sbe-primary');
    setup.append(node('p', () => (translate("출력은 원본·크롭 픽셀을 그대로 유지합니다. 삭제한 프레임과 해당 재생 시간을 함께 제거하며 원본은 보존합니다.")), 'sbe-hint'));
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
    bindAttribute(frameAside, 'aria-label', () => translate("프레임 탐색"));
    frameAside.hidden = true;
    frameAside.append(node('h3', () => (translate("프레임"))));
    editorBody.prepend(frameAside);
    gifWorkspace.append(editMain);
    workspace.append(gifWorkspace);
    const clipControl = mountRangeControl(workspace, { label: () => translate("남길 구간"), count: () => editableCount(session), values: () => [clipStart, clipEnd || editableCount(session)], format: n => frameLabel(n), change: (a, b, side, done, previous) => {
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
    bindAttribute(stage, 'aria-label', () => translate("크롭 영역 편집. 모서리와 테두리에서 크기 조절, 영역 안에서 이동. 새 영역은 새 영역 그리기로 선택"));
    stage.style.cursor = 'default';
    const cropSurface = node('section', undefined, 'sbe-crop-surface');
    bindAttribute(cropSurface, 'aria-label', () => translate("크롭 편집"));
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
    const drawArea = button(() => (translate("새 영역 그리기")), () => { cropMode = 'draw'; stage.style.cursor = 'crosshair'; drawArea.setAttribute('aria-pressed', 'true'); adjustArea.setAttribute('aria-pressed', 'false'); report(translate("영상 위에서 드래그하여 새 크롭 영역을 그리세요.")); }, cropFields);
    const adjustArea = button(() => (translate("영역 이동·크기 조절")), () => { cropMode = 'adjust'; stage.style.cursor = 'default'; drawArea.setAttribute('aria-pressed', 'false'); adjustArea.setAttribute('aria-pressed', 'true'); report(translate("영역 안에서 이동하거나 모서리·테두리에서 크기를 조절하세요.")); }, cropFields);
    drawArea.setAttribute('aria-pressed', 'false');
    adjustArea.setAttribute('aria-pressed', 'true');
    for (const [key, label] of [['x', () => translate("왼쪽")], ['y', () => translate("위쪽")], ['width', () => translate("너비")], ['height', () => translate("높이")]]) {
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
    button(() => (translate("전체 영역")), () => {
        if (!session)
            return;
        const before = snapshot();
        crop = { x: 0, y: 0, width: session.width, height: session.height };
        remember(before);
    }, cropFields);
    cropSurface.append(node('p', () => (translate("모서리나 테두리를 드래그해 크기를 바꾸거나 영역 안에서 위치를 옮기세요. 새 사각형을 만들려면 새 영역 그리기를 선택하세요.")), 'sbe-hint'));
    const frameActions = node('div', undefined, 'sbe-actions');
    editMain.append(frameActions);
    const previousFrame = button(() => (translate("이전 프레임")), () => { seekFrame(Math.max(0, current - 1)); }, frameActions);
    const nextFrame = button(() => (translate("다음 프레임")), () => { seekFrame(Math.min(session.frameCount - 1, current + 1)); }, frameActions);
    const position = node('span');
    frameActions.append(position);
    const frameSlider = node('input');
    frameSlider.type = 'range';
    frameSlider.min = '0';
    bindAttribute(frameSlider, 'aria-label', () => translate("현재 프레임"));
    frameSlider.addEventListener('input', () => { seekFrame(+frameSlider.value); });
    editMain.append(frameSlider);
    const rangeActions = node('div', undefined, 'sbe-actions');
    editMain.append(rangeActions);
    const rangeFrom = numeric(() => (translate("선택 시작 번호")), 1, 1, Number.MAX_SAFE_INTEGER, 1, rangeActions), rangeTo = numeric(() => (translate("선택 끝 번호")), 1, 1, Number.MAX_SAFE_INTEGER, 1, rangeActions);
    button(() => (translate("범위 선택")), () => { selection = selectedFrameRange(Math.max(0, +rangeFrom.value - 1), Math.min(session.frameCount - 1, +rangeTo.value - 1)); anchor = Math.max(0, +rangeFrom.value - 1); frameSelectionBase = new FrameRanges(selection.ranges); sync(); }, rangeActions);
    button(() => (translate("선택 해제")), () => { selection.clear(); anchor = null; frameSelectionBase.clear(); sync(); }, rangeActions);
    const deleteButton = button(() => (translate("선택 프레임 삭제")), () => {
        if (!session || !selection.size || busy)
            return;
        const combined = new FrameRanges([...deleted.ranges, ...selection.ranges]);
        if (combined.ranges.reduce((n, r) => n + Math.max(0, Math.min(r.last, clipEnd - 1) - Math.max(r.first, clipStart) + 1), 0) >= clipEnd - clipStart)
            return report(translate("최소 한 프레임은 남겨야 합니다."), true);
        const before = snapshot();
        deleted = combined;
        selection.clear();
        anchor = null; frameSelectionBase.clear();
        remember(before);
        grid.focus({ preventScroll: true });
    }, rangeActions);
    const undoButton = button(() => (translate("실행 취소")), () => {
        if (!undo.length)
            return;
        stopPlayback();
        redo.push(snapshot());
        restore(undo.pop());
    }, rangeActions);
    const redoButton = button(() => (translate("다시 실행")), () => {
        if (!redo.length)
            return;
        stopPlayback();
        undo.push(snapshot());
        restore(redo.pop());
    }, rangeActions);
    [previousFrame, nextFrame, deleteButton, undoButton, redoButton].forEach(describeShortcut);
    const applyCrop = button(() => (translate("모든 프레임에 크롭 적용")), () => {
        if (!crop || JSON.stringify(crop) === JSON.stringify(appliedCrop))
            return;
        const before = snapshot();
        appliedCrop = { ...crop };
        remember(before);
        report(translate("남기는 모든 프레임에 같은 크롭 영역을 적용했습니다."));
    }, cropFields, 'sbe-primary');
    const quickCropToggle = button(() => (translate("크롭 영역 조절")), () => {
        quickCrop = !quickCrop;
        if (source) source.pause();
        sync();
    }, sourceActions);
    const cropState = node('p', '', 'sbe-hint');
    cropFields.append(cropState);
    const grid = node('nav', undefined, 'sbe-frames');
    bindAttribute(grid, 'aria-label', () => translate("GIF 프레임 목록"));
    grid.tabIndex = 0;
    const frameSelectionCount = node('p', () => (translate("Shift·Ctrl 클릭 또는 드래그로 선택")), 'sbe-frame-selection');
    frameSelectionCount.setAttribute('aria-live', 'polite');
    frameAside.append(frameSelectionCount);
    frameAside.append(grid);
    const paging = node('div', undefined, 'sbe-actions');
    frameAside.append(paging);
    const prevPage = button(() => (translate("이전 48장")), () => { page--; navTarget = Math.max(0, page * 48); void renderFrames(); }, paging), pageText = node('span');
    paging.append(pageText);
    const nextPage = button(() => (translate("다음 48장")), () => { page++; navTarget = Math.min(session.frameCount - 1, page * 48); void renderFrames(); }, paging);
    const previewBox = node('div', undefined, 'sbe-preview');
    editMain.append(previewBox);
    const play = button(() => (translate("편집 결과 재생")), () => {
        if (playing)
            return stopPlayback();
        playing = true;
        const version = ++playbackVersion;
        bindText(play, () => translate("재생 멈추기"));
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
                bindText(position, () => framePosition(index));
                clipControl.update(index);
                renderFrames();
                drawPreview(img);
                playTimer = setTimeout(tick, 1000 * (frameDelays.get(index) || 1 / session.options.fps));
            }
            catch {
                if (version !== playbackVersion)
                    return;
                stopPlayback();
                report(translate("프레임을 불러오지 못했습니다. PC 연결을 확인하세요."), true);
            }
        }
        void tick();
    }, previewBox);
    const previewCanvas = node('canvas');
    bindAttribute(previewCanvas, 'aria-label', () => translate("크롭·삭제 적용 미리보기"));
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
    for (const [value, text] of [['small', () => translate("작은 용량")], ['balanced', () => translate("균형")], ['high', () => translate("고화질")], ['max', () => translate("최대 화질")], ['custom', () => translate("사용자 지정")]]) {
        const o = node('option', text);
        o.value = value;
        preset.append(o);
    }
    field(() => (translate("화질 프리셋")), preset, qualityBox);
    const quality = numeric(() => (translate("화질(1–100)")), 80, 1, 100, 1, qualityBox);
    const compression = node('select');
    for (const [value, text] of [['lossy', () => translate("손실 압축")], ['lossless', () => translate("무손실 압축")]]) {
        const option = node('option', text); option.value = value; compression.append(option);
    }
    field(() => (translate("WebP 압축 방식")), compression, qualityBox);
    const advanced = node('details', undefined, 'sbe-quality-advanced');
    advanced.append(node('summary', () => (translate("고급 설정"))));
    const advancedFields = node('div', undefined, 'sbe-advanced-fields');
    advanced.append(advancedFields); qualityBox.append(advanced);
    const effort = node('select');
    for (const [value, text] of [['fast', () => translate("빠르게")], ['normal', () => translate("보통")], ['precise', () => translate("정밀하게")]]) {
        const o = node('option', text);
        o.value = value;
        effort.append(o);
    }
    field(() => (translate("압축률")), effort, advancedFields);
    const dither = node('select');
    for (const [value, text] of [['auto', () => translate("자동")], ['sierra2_4a', () => translate("Sierra · 균형")], ['sierra3', () => translate("Sierra3 · 정밀")], ['floyd_steinberg', 'Floyd–Steinberg'], ['bayer', () => translate("Bayer · 규칙적 패턴")], ['atkinson', 'Atkinson'], ['none', () => translate("없음")]]) {
        const option = node('option', text); option.value = value; dither.append(option);
    }
    field(() => (translate("GIF 디더링")), dither, advancedFields);
    const webpPreset = node('select');
    for (const [value, text] of [['none', () => translate("기본")], ['photo', () => translate("사진")], ['drawing', () => translate("그림")], ['icon', () => translate("아이콘")], ['text', () => translate("텍스트")]]) {
        const option = node('option', text); option.value = value; webpPreset.append(option);
    }
    field(() => (translate("WebP 최적화 유형")), webpPreset, advancedFields);
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
        bindText(algorithmHint, () => !webp ? translate("디더링은 제한된 GIF 색상으로 그라데이션을 표현하는 방식입니다. 자동은 압축률에 맞춰 선택합니다.") : lossless ? translate("무손실 압축에서는 최적화 유형을 적용하지 않습니다.") : webpPreset.value === 'none' ? translate("화면 내용에 맞는 압축 설정을 선택할 수 있습니다. 일반적인 영상은 기본으로 두세요.") : translate("최적화 유형을 선택하면 압축률은 자동으로 설정됩니다."));
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
    bindText(qualityHint, () => translate("프레임 재생은 편집 확인용입니다. “화질 확인”은 현재 위치부터 남긴 프레임을 최대 2초, 실제 설정으로 변환합니다."));
    qualityBox.append(qualityHint);
    qualityBox.append(node('p', () => (translate("화질 변경은 크기·FPS·편집 내용을 바꾸지 않습니다. WebP 무손실에서는 화질 값이 압축 효율과 처리 시간에 영향을 줍니다.")), 'sbe-hint'));
    qualityBox.append(node('p', () => (translate("화질 확인 결과는 최근 8개를 임시 보관합니다.")), 'sbe-hint'));
    const sample = button(() => (translate("화질 확인")), () => action(() => submit(true)), qualityBox);
    const resourceBox = node('section', undefined, 'sbe-resources');
    workspace.append(resourceBox);
    bindAttribute(resourceBox, 'aria-label', () => translate("처리 PC 자원"));
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
            const heading = node('div', undefined, 'sbe-resources-heading');
            heading.append(node('h3', () => (translate("처리 PC 자원"))));
            if (r.estimateKnown)
                heading.append(node('span', () => (r.basis === 'sampled' ? translate("미리보기 실측 반영") : translate("계산 추정")), 'sbe-resource-badge'));
            const cards = node('div', undefined, 'sbe-resource-grid');
            function card(title, label, known, value, detail, estimateLabel, estimateKnown, estimate, unknownEstimate) {
                const section = node('section', undefined, 'sbe-resource-card');
                section.setAttribute('aria-label', title);
                section.append(node('h4', title));
                const metric = node('div', undefined, 'sbe-resource-metric');
                metric.append(node('p', label, 'sbe-resource-label'));
                const amount = node('p', undefined, 'sbe-resource-value');
                if (known) {
                    const [number, unit] = bytes(value).split(' ');
                    amount.append(node('strong', number), node('span', unit, 'sbe-resource-unit'));
                } else {
                    amount.classList.add('is-unknown');
                    bindText(amount, () => translate("확인 불가"));
                }
                metric.append(amount);
                if (detail)
                    metric.append(node('p', detail, 'sbe-resource-detail'));
                const expected = node('dl', undefined, 'sbe-resource-estimate');
                expected.append(node('dt', estimateLabel), node('dd', estimateKnown ? estimate : unknownEstimate, estimateKnown ? '' : 'is-unknown'));
                section.append(metric, expected);
                cards.append(section);
                return section;
            }
            const low = bytes(r.lowRam), high = bytes(r.highRam);
            const [lowNumber, lowUnit] = low.split(' '), [, highUnit] = high.split(' ');
            const range = lowUnit === highUnit ? `${lowNumber}–${high}` : `${low} – ${high}`;
            const memory = card(translate("메모리 · RAM"), translate("사용 가능"), r.memoryKnown, r.availableRam,
                r.memoryKnown ? translate("전체 {0}", { "0": bytes(r.totalRam) }) : translate("PC 메모리 정보를 읽지 못했습니다."),
                translate("추가 RAM 예상"), r.estimateKnown, range, translate("추정 불가"));
            memory.classList.add('sbe-resource-memory');
            memory.classList.toggle('is-warning', Boolean(r.pressure));
            const disk = card(translate("저장 공간"), translate("남은 공간"), r.diskKnown, r.diskAvailable,
                r.diskKnown ? '' : translate("저장 공간 정보를 읽지 못했습니다."),
                translate("임시·결과 예상"), r.diskEstimateKnown, bytes(r.diskEstimate), translate("화질 확인 후 추정"));
            disk.classList.add('sbe-resource-disk');
            resourceBox.replaceChildren(heading, cards);
            if (r.live?.known) {
                const live = node('div', undefined, 'sbe-resource-live');
                live.append(node('span', () => (translate("실행 중 인코더 RAM"))), node('strong', bytes(r.live.currentBytes)), node('span', () => (translate("관측 최대 {0}", { "0": bytes(r.live.observedPeakBytes) }))));
                resourceBox.append(live);
            }
            if (r.note)
                resourceBox.append(node('p', r.note, 'sbe-hint sbe-resource-note'));
            if (r.pressure) {
                const warning = node('div', undefined, 'sbe-resource-warning');
                warning.append(node('strong', () => (translate("메모리 여유 부족 예상"))), node('p', () => (translate("예상 RAM이 현재 여유보다 큽니다. 다른 작업을 줄이거나 설정을 조정할 수 있습니다. 그대로 실행할 수도 있습니다."))));
                resourceBox.append(warning);
            }
        }
        catch (e) {
            if (!dead)
                bindText(resourceBox, () => translate("처리 PC 자원 정보를 가져오지 못했습니다. ") + e.message);
        }
        finally {
            resourceReading = false;
        }
    }
    async function submit(preview) {
        if (!session || session.state !== 'ready')
            return;
        if ((session.options.quickVideo ? kind.value : session.options.kind) !== 'trim' && JSON.stringify(crop) !== JSON.stringify(appliedCrop))
            throw new Error(translate("변경한 크롭을 모든 프레임에 적용한 뒤 저장하세요."));
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
            if (prepared.state !== 'ready') throw new Error(prepared.error || translate("원본 영상 정보를 읽지 못했습니다."));
            if (dead || session !== localSession) return;
            quickPrepareRetry = null;
            const previous = session;
            session = prepared;
            if (session.width !== previous.width || session.height !== previous.height) {
                crop = appliedCrop = { x: 0, y: 0, width: session.width, height: session.height };
                throw new Error(translate("원본 크기를 다시 확인했습니다. 크롭 영역을 확인한 뒤 다시 저장하세요."));
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
        report(preview ? translate("화질 확인을 등록했습니다. 최대 2초를 변환하며 갤러리에 등록하지 않습니다.") : translate("저장 작업을 등록했습니다. 화면을 닫거나 연결이 끊겨도 PC에서 계속 처리합니다."));
        await refresh();
    }
    const save = button(() => (translate("GIF 저장")), () => action(() => submit(false)), workspace, 'sbe-primary');
    const history = node('section', undefined, 'sbe-history');
    history.append(node('h3', () => (translate("편집 자료·작업"))));
    const sessionLimit = 20;
    const sessionCount = node('p', '', 'sbe-hint');
    bindAttribute(sessionCount, 'aria-label', () => translate("편집 초안 보관 개수"));
    const jobCount = node('p', '', 'sbe-hint');
    bindAttribute(jobCount, 'aria-label', () => translate("작업 내역 보관 개수"));
    const sessionLimitHint = node('p', () => (translate("편집 초안과 작업 내역은 각각 최근 20개를 보관하며, 초과하면 오래된 항목부터 자동 정리합니다. 진행 중인 작업과 복구 대상은 보호하며, 원본 영상과 저장된 결과는 유지됩니다.")), 'sbe-hint');
    history.append(sessionCount, jobCount, sessionLimitHint);
    content.append(history);
    button(() => (translate("목록 새로고침")), () => refresh(), history);
    const sessionList = node('div'), jobList = node('div'), resultBox = node('div', undefined, 'sbe-result');
    history.append(sessionList, jobList, resultBox);
    function useSession(value) {
        stopFrameDrag(); anchor = null; frameSelectionBase.clear();
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
        bindText(title, () => value.title + translate(" · 편집"));
        bindText(sourceTitle, () => value.title);
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
            report(value.options.quickVideo ? translate("남길 구간을 선택한 뒤 저장하세요. 선택한 구간만 변환합니다.") : value.options.kind === 'gif' ? translate("프레임을 불러왔습니다. 위쪽 “남길 구간” 손잡이로 사용할 부분을 정하고 크롭하거나 프레임을 삭제하세요.") : translate("영상 준비가 끝났습니다. 남길 구간과 소리 옵션을 확인한 뒤 저장하세요."));
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
                    throw new Error(translate("편집 화면이 변경되었습니다."));
                await new Promise(resolve => setTimeout(resolve, 600));
                p = await unwrap(api.frameStatus({ id, offset }));
            }
            if ((session?.options.nativeGIF || session?.options.nativeVideo) && !p.times?.length)
                throw new Error(translate("원본 프레임 재생 시간 정보를 읽지 못했습니다. 편집 자료를 다시 준비하세요."));
            cacheTiming(p);
            if (session?.id === id) {
                clipControl.update(current);
                bindText(position, () => framePosition(current));
            }
            if (p.state !== 'ready')
                throw new Error(p.error || translate("프레임 준비 실패. 페이지를 다시 열어 재시도하세요."));
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
        const promise = new Promise((resolve, reject) => { const img = new Image(); img.onload = () => resolve(img); img.onerror = () => reject(new Error(translate("프레임 로딩 실패"))); img.src = frameURL(session.id, n); });
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
                report(translate("프레임을 불러오지 못했습니다. 편집 자료와 PC 연결을 확인하세요."), true);
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
            bindText(position, () => framePosition(current));
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
    function frameLabel(n) { const t = frameTime(n); return t === null ? translate("{0}번 프레임", { "0": n + 1 }) : formatEditTime(t); }
    function framePosition(n) { return translate("{0} / {1}장 · {2}{3}", { "0": n + 1, "1": session.frameCount, "2": frameLabel(n), "3": deleted.has(n) ? translate(" · 삭제한 프레임") : '' }); }
    function nextRetained(n) { const first = deleted.next(Math.max(clipStart, n), clipEnd); return first ?? deleted.next(clipStart, clipEnd); }
    function seekFrame(n, reveal = true) {
        if (!session)
            return;
        stopPlayback();
        current = clamp(n, 0, session.frameCount - 1);
        page = Math.floor(current / 48);
        navTarget = reveal ? current : null;
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
    function selectedFrameRange(first, last, base = new FrameRanges()) {
        const value = new FrameRanges([...base.ranges, { first: Math.min(first, last), last: Math.max(first, last) }]);
        for (const removed of deleted.ranges) value.removeRange(removed.first, removed.last);
        return value;
    }
    function selectFrame(n, event, toggle = false) {
        if (suppressFrameClick) { event.preventDefault(); return; }
        if (dead || busy || !session || session.state !== 'ready' || timeMode(session)) return;
        if (deleted.has(n)) { if (!toggle) seekFrame(n, false); return; }
        const additive = event.ctrlKey || event.metaKey;
        if (event.shiftKey) {
            if (anchor === null) anchor = current;
            selection = selectedFrameRange(anchor, n, additive ? frameSelectionBase : new FrameRanges());
        } else {
            selection = new FrameRanges(additive || toggle ? selection.ranges : []);
            if ((additive || toggle) && selection.has(n)) selection.delete(n); else selection.add(n);
            anchor = n; frameSelectionBase = new FrameRanges(selection.ranges);
        }
        seekFrame(n, false);
    }
    function frameIndexAt(clientY) {
        const rect = grid.getBoundingClientRect(), y = clientY - rect.top;
        if (session.frameCount * rowHeight <= maxScrollHeight) return clamp(Math.floor((grid.scrollTop + y) / rowHeight), 0, session.frameCount - 1);
        const first = clamp(grid.scrollTop / Math.max(1, maxScrollHeight - (grid.clientHeight || 320)) * Math.max(0, session.frameCount - Math.ceil((grid.clientHeight || 320) / rowHeight)), 0, session.frameCount - 1);
        return clamp(first + Math.floor(y / rowHeight), 0, session.frameCount - 1);
    }
    function updateFrameDrag() {
        if (!framePointer?.active || !session) return;
        framePointer.last = frameIndexAt(framePointer.y);
        selection = selectedFrameRange(framePointer.first, framePointer.last, framePointer.additive ? framePointer.base : new FrameRanges());
        renderFrames(); deleteButton.disabled = !selection.size;
    }
    function scrollFrames() {
        if (!framePointer?.active || dead) return;
        const rect = grid.getBoundingClientRect(), before = grid.scrollTop;
        if (framePointer.y < rect.top + 28) grid.scrollTop = Math.max(0, before - 14);
        else if (framePointer.y > rect.bottom - 28) grid.scrollTop = Math.min(Math.max(0, grid.scrollHeight - grid.clientHeight), before + 14);
        if (grid.scrollTop !== before) updateFrameDrag();
        frameScroll = view.requestAnimationFrame(scrollFrames);
    }
    function stopFrameDrag(cancel = false) {
        const previous = framePointer; framePointer = null;
        if (frameScroll !== null) view.cancelAnimationFrame(frameScroll);
        frameScroll = null;
        if (previous) { try { grid.releasePointerCapture(previous.id); } catch { } }
        if (cancel && previous?.active) { selection = previous.base; renderFrames(); deleteButton.disabled = !selection.size; }
    }
    grid.addEventListener('pointerdown', event => {
        if (event.button !== 0 || event.pointerType && event.pointerType !== 'mouse' || busy || !session || session.state !== 'ready' || timeMode(session)) return;
        const tile = event.target.closest('.sbe-frame');
        if (!tile || deleted.has(+tile.dataset.frame)) return;
        framePointer = { id: event.pointerId, first: +tile.dataset.frame, last: +tile.dataset.frame, x: event.clientX, startY: event.clientY, y: event.clientY, active: false, additive: event.ctrlKey || event.metaKey, base: new FrameRanges(selection.ranges) };
    });
    function moveFramePointer(event) {
        if (!framePointer || event.pointerId !== framePointer.id) return;
        if (busy || dead) { stopFrameDrag(true); return; }
        framePointer.y = event.clientY;
        if (!framePointer.active) {
            if (Math.hypot(event.clientX - framePointer.x, event.clientY - framePointer.startY) < 5) return;
            framePointer.active = true; grid.setPointerCapture?.(event.pointerId); stopPlayback();
            frameScroll = view.requestAnimationFrame(scrollFrames);
        }
        event.preventDefault(); updateFrameDrag();
    }
    function finishFramePointer(event) {
        if (!framePointer || event.pointerId !== framePointer.id) return;
        const previous = framePointer;
        if (previous.active) {
            previous.y = event.clientY; updateFrameDrag();
            suppressFrameClick = true; clearTimeout(frameClickTimer); frameClickTimer = setTimeout(() => { suppressFrameClick = false; }, 0);
            anchor = previous.first; frameSelectionBase = new FrameRanges(selection.ranges);
        }
        stopFrameDrag();
        if (previous.active) { seekFrame(previous.last, false); grid.focus({ preventScroll: true }); }
    }
    function cancelFramePointer(event) { if (!event || event.type === 'blur' || framePointer?.id === event.pointerId) stopFrameDrag(true); }
    view.addEventListener('pointermove', moveFramePointer, { passive: false }); view.addEventListener('pointerup', finishFramePointer);
    view.addEventListener('pointercancel', cancelFramePointer); view.addEventListener('blur', cancelFramePointer);
    grid.addEventListener('lostpointercapture', cancelFramePointer);
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
                const view = button(() => (translate("{0}번 프레임", { "0": n + 1 })), event => { event.stopPropagation(); selectFrame(n, event); }, tile);
                bindAttribute(view, 'aria-label', () => translate("{0}번 프레임 보기", { "0": n + 1 }));
                const check = node('input');
                check.type = 'checkbox';
                bindAttribute(check, 'aria-label', () => translate("{0}번 프레임 선택", { "0": n + 1 }));
                bindAttribute(check, 'title', () => translate("프레임 선택 (Shift+클릭: 범위 선택, Ctrl+클릭: 개별 선택)"));
                check.addEventListener('click', event => {
                    event.stopPropagation(); selectFrame(n, event, true);
                });
                tile.addEventListener('click', event => { if (!event.target.closest('button,input')) selectFrame(n, event); });
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
            tile.className = 'sbe-frame' + (deleted.has(n) ? ' is-deleted' : '') + (selection.has(n) ? ' is-selected' : '') + (n === current ? ' is-current' : '') + (n < clipStart || n >= clipEnd ? ' is-outside' : '');
            check.checked = selection.has(n);
            check.disabled = busy || deleted.has(n);
            button.disabled = busy;
            if (n === current)
                button.setAttribute('aria-current', 'true');
            else
                button.removeAttribute('aria-current');
        }
        bindText(pageText, () => `${first + 1}–${end} / ${count}`);
        const selectionText = selection.size ? translate("{0}개 선택 · Delete로 삭제", { "0": selection.size }) : translate("Shift·Ctrl 클릭 또는 드래그로 선택");
        if (frameSelectionCount.textContent !== selectionText) bindText(frameSelectionCount, () => selectionText);
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
            return translate("원본 프레임 간격 유지");
        return translate("약 ") + (retainedCount() / session.options.fps).toFixed(2) + translate("초");
    }
    function sync() {
        if (dead)
            return;
        fps.disabled = kind.value === 'trim' || sourceGIF || !!session?.options.nativeGIF;
        mute.disabled = kind.value !== 'trim';
        prepare.disabled = busy || !file;
        bindText(prepare, () => sourceGIF ? translate("프레임 불러오기") : translate("프레임 정밀 편집"));
        prepare.hidden = !sourceGIF && kind.value === 'trim';
        workspace.hidden = !session;
        clipControl.root.hidden = session?.state !== 'ready';
        const quick = !!session?.options.quickVideo;
        const frameEditing = session?.state === 'ready' && session.options.kind !== 'trim' && !quick;
        const trimMode = (quick ? kind.value : session?.options.kind ?? kind.value) === 'trim';
        const cropEditing = frameEditing || quick && quickCrop && !trimMode;
        const sourceMatches = !session || session.fileId === file?.id;
        overview.hidden = !source || !api.overview || !sourceMatches || !!session && !timeMode(session);
        sourceSurface.hidden = false;
        if (source) {
            if ((cropEditing || !sourceMatches) && !source.paused) source.pause();
            source.hidden = !sourceMatches || cropEditing && paintedSession === session.id;
        }
        sourceActions.hidden = !source || !sourceMatches || !!session && !timeMode(session);
        quickCropToggle.hidden = !quick || trimMode;
        bindText(quickCropToggle, () => quickCrop ? translate("영상으로 돌아가기") : translate("크롭 영역 조절"));
        quickCropToggle.setAttribute('aria-pressed', String(quickCrop));
        cropSurface.hidden = !cropEditing;
        cropPlaceholder.hidden = frameEditing || Boolean(source) && sourceMatches || trimMode;
        frameAside.hidden = !frameEditing;
        editorBody.classList.toggle('has-frames', Boolean(frameEditing));
        frameShortcutGroup.hidden = !frameEditing;
        rangeShortcutGroup.hidden = !(session?.state === 'ready' || !setup.hidden && !sourceGIF);
        bindText(shortcutMode, () => !session || session.state !== 'ready' ? translate("편집 자료를 열고 준비가 끝나면 해당 모드의 단축키가 표시됩니다.") : frameEditing ? translate("GIF·WebP 프레임 편집에서 사용할 수 있는 단축키입니다.") : translate("동영상 자르기에서는 구간 손잡이와 공통 단축키를 사용할 수 있습니다."));
        sourceRange.root.hidden = !file || sourceGIF || !!session;
        sourceRange.update();
        if (!session) {
            save.disabled = true;
            sample.disabled = true;
            return;
        }
        const gif = quick ? kind.value !== 'trim' : session.options.kind !== 'trim', ready = session.state === 'ready';
        clipControl.root.hidden = !ready;
        sourceRange.root.hidden = true;
        gifWorkspace.hidden = !frameEditing;
        muteLabel.hidden = gif || !ready;
        mute.disabled = !ready;
        bindText(summary, () => quick ? translate("{0} · 선택 구간 {1}초", { "0": session.title, "1": (frameTime(clipEnd)-frameTime(clipStart)).toFixed(2) }) : `${session.title} · ${session.cancelling ? translate("취소 중") : labels[session.state] || session.state} ${session.progress}%` + (ready && gif ? translate(" · {0}장 중 {1}장 남음 · {2} · {3}×{4}px", { "0": session.frameCount, "1": retainedCount(), "2": retainedDuration(), "3": crop?.width || session.width, "4": crop?.height || session.height }) : '') + (session.error ? ` · ${session.error}` : ''));
        bindText(save, () => gif ? (kind.value === 'webp' ? translate("WebP 저장") : translate("GIF 저장")) : translate("잘라낸 영상 저장"));
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
            bindText(applyCrop, () => translate("크롭 적용"));
            for (const key of Object.keys(cropInputs)) {
                cropInputs[key].max = String(key === 'x' || key === 'width' ? session.width : session.height);
                if (doc.activeElement !== cropInputs[key]) cropInputs[key].value = String(crop[key]);
            }
            bindText(cropState, () => JSON.stringify(crop) === JSON.stringify(appliedCrop) ? translate("{0}×{1}px 적용됨", { "0": appliedCrop.width, "1": appliedCrop.height }) : translate("크롭 변경 후 크롭 적용을 누르세요."));
            bindText(qualityHint, () => translate("선택 구간을 원본 프레임으로 변환합니다. 화질 확인은 현재 위치 근처에서 최대 2초만 처리합니다."));
            if (quickCrop) void paint();
            return;
        }
        bindText(applyCrop, () => translate("모든 프레임에 크롭 적용"));
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
            bindText(position, () => framePosition(current));
            clipControl.update(current);
            bindText(cropState, () => JSON.stringify(crop) === JSON.stringify(appliedCrop) ? translate("모든 프레임에 {0}×{1}px 적용됨", { "0": appliedCrop?.width, "1": appliedCrop?.height }) : translate("크롭 변경 후 모든 프레임에 적용을 누르세요."));
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
        if (event.key === 'Escape' && framePointer?.active) { event.preventDefault(); event.stopPropagation(); stopFrameDrag(true); return; }
        if (event.key === 'Escape' && !shortcutsPanel.hidden) {
            event.preventDefault(); event.stopPropagation(); showShortcuts(false); return;
        }
        const frameCheckbox = grid.contains(event.target) && event.target.tagName === 'INPUT' && event.target.type === 'checkbox';
        if (shortcutsPanel.contains(event.target) || busy || !session || session.state !== 'ready' || timeMode(session) || !frameCheckbox && (['INPUT', 'SELECT', 'TEXTAREA'].includes(event.target.tagName) || event.target.isContentEditable))
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
        bindText(sessionCount, () => translate("편집 초안 {0}개 · 최근 {1}개 자동 보관", { "0": sessions.length, "1": sessionLimit }));
        bindText(jobCount, () => translate("작업 내역 {0}개 · 최근 {1}개 자동 보관", { "0": jobs.length, "1": sessionLimit }));
        sessionList.replaceChildren();
        jobList.replaceChildren();
        for (const item of sessions) {
            const row = node('div', undefined, 'sbe-record');
            row.append(node('span', () => (`${item.title} · ${item.options.kind === 'gif' ? 'GIF' : translate("영상")} · ${labels[item.state] || item.state} ${item.progress}%${item.cancelling ? translate(" · 취소 중") : ''}`)));
            button(() => (translate("열기")), () => {
                useSession(item);
                mainPane.scrollTop = 0;
                const target = item.state === 'ready' && item.options.kind !== 'trim' ? stage : closeEditor;
                target.focus({ preventScroll: true });
            }, row);
            if (active(item.state))
                button(() => (translate("취소")), () => action(async () => { await unwrap(api.cancel({ id: item.id })); await refresh(); }), row);
            else
                button(() => (translate("편집 초안 삭제")), () => action(async () => {
                    if (!confirm(translate("이 편집 초안과 임시 프레임을 삭제할까요? 원본 영상과 저장된 결과는 유지됩니다.")))
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
            row.append(node('span', () => (`${(job.preview ? translate("화질 확인 · ") : '') + (job.options.kind === 'webp' ? 'WebP' : job.options.kind === 'gif' ? 'GIF' : translate("영상"))} · ${new Date(job.createdAt).toLocaleString(getLocale())} · ${job.cancelling ? translate("취소 중") : labels[job.state] || job.state} ${job.progress}%${job.error ? ' · ' + job.error : ''}`)));
            if (job.resources?.known)
                row.append(node('span', () => (translate("인코더 RAM {0} · 관측 최대 {1}", { "0": bytes(job.resources.currentBytes), "1": bytes(job.resources.observedPeakBytes) }))));
            if (active(job.state) && job.state !== 'publishing')
                button(() => (translate("취소")), () => action(async () => { await unwrap(api.cancel({ id: job.id })); await refresh(); }), row);
            if (job.state === 'completed' && (job.resultFileId || job.preview))
                button(() => (translate("결과 보기")), () => { selectedJob = job.id; renderResult(); }, row);
            if (['failed', 'cancelled', 'interrupted'].includes(job.state)) {
                const retry = button(() => (translate("같은 설정으로 다시 실행")), () => action(async () => { const next = await unwrap(api.create({ ...job.options, requestId: crypto.randomUUID() })); selectedJob = next.id; await refresh(); }), row);
                retry.disabled = !sessions.some(item => item.id === (job.sessionId || job.options.sessionId) && item.state === 'ready');
                if (retry.disabled) bindAttribute(retry, 'title', () => translate("편집 초안을 다시 준비한 뒤 저장하세요."));
            }
            jobList.append(row);
        }
        if (!sessions.length && !jobs.length)
            sessionList.append(node('p', () => (translate("편집 자료와 작업이 없습니다."))));
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
            bindAttribute(media, 'alt', () => job.preview ? translate("실제 화질 미리보기") : translate("생성한 애니메이션"));
        else {
            media.controls = true;
            media.playsInline = true;
        }
        media.src = resultURL(job.id);
        resultBox.append(media);
        const link = node('a', () => (translate("결과 다운로드")));
        link.href = resultURL(job.id) + '?download=1';
        link.download = '';
        resultBox.append(link);
        if (options.reveal && !job.preview)
            button(() => (translate("PC 파일 위치")), () => action(() => options.reveal(job.id)), resultBox);
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
            try {
                const retained = new Set(sessions.map(item => `storagebucket-edit-v1:${item.id}`));
                if (session?.localQuick) retained.add(storageKey());
                for (let i = localStorage.length - 1; i >= 0; i--) {
                    const key = localStorage.key(i);
                    if (key?.startsWith('storagebucket-edit-v1:') && !retained.has(key)) localStorage.removeItem(key);
                }
            } catch { }
            if (session) {
                const latest = sessions.find(item => item.id === session.id);
                if (latest) {
                    if (session.state !== 'ready' && latest.state === 'ready')
                        useSession(latest);
                    else
                        session = latest;
                } else if (!session.localQuick) {
                    stopPlayback();
                    stopFrameDrag();
                    paintSerial++;
                    session = null;
                    imageCache.clear();
                    lastPreview = null;
                    report(translate("오래된 편집 초안이 정리되었습니다. 원본을 다시 열어 편집을 시작할 수 있습니다."));
                }
            }
            renderHistory();
            sync();
            if (session?.state === 'ready')
                void updateResources();
        }
        catch (e) {
            if (!dead)
                report(e.message || translate("PC에 연결하지 못했습니다. 연결되면 작업 상태를 다시 확인합니다."), true);
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
        stopLanguage();
        stopFrameDrag(); clearTimeout(frameClickTimer);
        view.removeEventListener('pointermove', moveFramePointer); view.removeEventListener('pointerup', finishFramePointer);
        view.removeEventListener('pointercancel', cancelFramePointer); view.removeEventListener('blur', cancelFramePointer);
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
        overviewGrid.replaceChildren();
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
    const stopLanguage = localizeWidget(dialog);
    void loadOverview();
    return { destroy };
}
