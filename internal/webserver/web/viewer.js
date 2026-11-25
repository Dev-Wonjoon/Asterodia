import { translate, bindText, bindAttribute, localizeWidget } from './i18n.js';
import { mountVideoPlayer } from './player.js';
import { createViewerQueue } from './viewer-queue.js';
import { isTextInput, isComposing, matchShortcut, blockingDialog } from './shortcuts.js';
import { mountViewerViewport } from './viewer-viewport.js';
import { mountViewerPicker } from './viewer-picker.js';

// Adapters keep desktop paths and browser endpoints outside the shared viewer.
export function mountMediaViewer({ mediaId, fileId, getSequence, getFile, contentURL, previewURL, thumbnailURL, listMedia, getAlbum, onClose = () => {} }) {
    const doc = document, previousFocus = doc.activeElement, oldOverflow = doc.body.style.overflow;
    for (const media of doc.querySelectorAll('audio,video')) media.pause();
    const node = (tag, cls, value) => { const el = doc.createElement(tag); if (cls) el.className = cls; if (value) bindText(el, typeof value === 'function' ? value : () => value); return el; };
    const button = (label, action, parent, cls) => { const el = node('button', cls, label); el.type = 'button'; el.onclick = action; parent.append(el); return el; };
    const dialog = node('dialog', 'sb-viewer'); dialog.dataset.detailTrigger = 'true';
    bindAttribute(dialog, 'aria-label', () => translate('viewer.mediaViewer'));
    const header = node('header', 'sbv-header'), title = node('h2', '', () => translate('viewer.open'));
    header.append(title);
    const layouts = node('div', 'sbv-layouts'); layouts.setAttribute('role', 'group'); bindAttribute(layouts, 'aria-label', () => translate('viewer.layout'));
    const layoutButtons = [1, 2, 4].map(count => button(() => translate('viewer.layout' + count), () => setLayout(count), layouts));
    const modes = node('div', 'sbv-modes'); modes.setAttribute('role', 'group'); bindAttribute(modes, 'aria-label', () => translate('viewer.viewMode'));
    const separate = button(() => translate('viewer.differentMedia'), () => setMode(false), modes);
    const linked = button(() => translate('viewer.sameMedia'), () => setMode(true), modes);
    header.append(layouts, modes); const close = button(() => translate('viewer.closeEsc'), () => dialog.close(), header);
    const stage = node('div', 'sbv-stage'), sharedHost = node('div', 'sbv-shared-player'); sharedHost.hidden = true;
    const status = node('p', 'sbv-status'); status.setAttribute('role', 'status');
    const footer = node('footer', 'sbv-footer'), count = node('span', 'sbv-count');
    const previous = button(() => translate('viewer.previous'), () => navigate(-1), footer);
    footer.append(count); const next = button(() => translate('viewer.next'), () => navigate(1), footer);
    const options = node('div', 'sbv-options'); footer.append(options);
    function option(label, key) {
        const wrap = node('label'), input = node('input'), text = node('span', '', label); input.type = 'checkbox';
        input.onchange = () => { const r = resource(); if (!r) return; r[key] = input.checked; if (key === 'shuffle') r.queue?.setShuffle(input.checked); if (key === 'repeat') r.queue?.setRepeat(input.checked); controls(); };
        wrap.append(input, text); options.append(wrap); return input;
    }
    const auto = option(() => translate('viewer.autoNext'), 'auto'), shuffle = option(() => translate('viewer.shuffle'), 'shuffle'), repeat = option(() => translate('viewer.repeatAlbum'), 'repeat');
    dialog.append(header, stage, sharedHost, status, footer); doc.body.append(dialog); doc.body.style.overflow = 'hidden';
    let destroyed = false, panes = [], active, group = null, savedSeparate = null, picker = null, expanded = false;
    const freshState = () => ({ zoom: 1, x: .5, y: .5 });
    const resource = () => group || active;
    const views = r => r === group ? panes : [r];
    const valid = (r, serial) => !destroyed && !r.dead && r.serial === serial;
    function message(r, text) {
        r.message = typeof text === 'function' ? text : () => text;
        for (const pane of views(r)) bindText(pane.status, r.message);
        if (r === resource()) bindText(status, r.message);
    }
    function focusPane(pane = active) { if (!destroyed && pane?.body.isConnected) pane.body.focus({ preventScroll: true }); }
    function select(pane) {
        if (destroyed || !panes.includes(pane)) return;
        active = pane;
        for (const item of panes) {
            item.root.classList.toggle('is-selected', item === pane);
            item.root.setAttribute('aria-current', item === pane ? 'true' : 'false');
            item.viewport?.select(item === pane);
        }
        controls();
    }
    function controls() {
        const r = resource(), queue = r?.queue;
        previous.disabled = !queue?.hasPrevious || r?.loading; next.disabled = !queue?.hasNext || r?.loading;
        auto.disabled = shuffle.disabled = repeat.disabled = !queue?.count || r?.loading;
        auto.checked = Boolean(r?.auto); shuffle.checked = Boolean(r?.shuffle); repeat.checked = Boolean(r?.repeat);
        bindText(count, () => queue?.count ? (queue.index + 1) + ' / ' + queue.count : '0 / 0');
        bindText(title, () => r?.sequence?.title || translate('viewer.open'));
        bindText(status, r?.message || (() => translate('gallery.loading')));
        layoutButtons.forEach((el, i) => { el.setAttribute('aria-pressed', String(panes.length === [1, 2, 4][i])); el.disabled = expanded; });
        separate.setAttribute('aria-pressed', String(!group)); linked.setAttribute('aria-pressed', String(Boolean(group)));
        separate.disabled = linked.disabled = expanded || Boolean(r?.loading) || !r?.queue;
        linked.disabled ||= r?.file?.kind === 'audio';
        sharedHost.hidden = !group?.player;
        panes.forEach((pane, index) => {
            const current = group || pane;
            bindText(pane.title, () => (index + 1) + '. ' + (current.file?.title || (current.loading ? translate('gallery.loading') : translate('viewer.emptyPane'))));
            bindAttribute(pane.body, 'aria-label', () => translate('viewer.pane', { number: index + 1 }));
            pane.root.setAttribute('aria-busy', String(current.loading));
            pane.root.classList.toggle('has-audio', current.file?.kind === 'audio' && Boolean(current.player));
            pane.zoomOut.disabled = !pane.viewport || pane.state.zoom <= 1;
            pane.zoomIn.disabled = !pane.viewport || pane.state.zoom >= 8;
            pane.reset.disabled = !pane.viewport;
            bindText(pane.scale, () => Math.round(pane.state.zoom * 100) + '%');
        });
    }
    function zoom(pane, direction) { select(pane); pane.viewport?.zoom(direction); focusPane(pane); }
    function magnifier(el, plus, label) {
        el.innerHTML = '<svg viewBox="0 0 24 24" width="19" height="19" aria-hidden="true" fill="none" stroke="currentColor" stroke-width="1.8"><circle cx="10" cy="10" r="6.5"/><path d="m15 15 6 6M6.5 10h7' + (plus ? 'M10 6.5v7' : '') + '"/></svg>';
        bindAttribute(el, 'aria-label', label); bindAttribute(el, 'title', label);
    }
    function newPane(snapshot) {
        const pane = { serial: 0, dead: false, loading: false, mediaId, auto: false, shuffle: false, repeat: false, ...snapshot,
            state: { ...(snapshot?.state || freshState()) }, viewport: null, player: null, detachSurface: null };
        pane.root = node('section', 'sbv-pane');
        const toolbar = node('div', 'sbv-pane-toolbar'); pane.title = node('h3'); toolbar.append(pane.title);
        pane.choose = button(() => translate('viewer.chooseMedia'), () => openPicker(pane), toolbar, 'sbv-choose');
        pane.zoomOut = button(null, () => zoom(pane, -1), toolbar, 'sbv-zoom'); magnifier(pane.zoomOut, false, () => translate('viewer.zoomOut'));
        pane.scale = node('output', 'sbv-scale'); toolbar.append(pane.scale);
        pane.zoomIn = button(null, () => zoom(pane, 1), toolbar, 'sbv-zoom'); magnifier(pane.zoomIn, true, () => translate('viewer.zoomIn'));
        pane.reset = button(() => translate('viewer.fit'), () => zoom(pane, 0), toolbar, 'sbv-fit');
        pane.body = node('div', 'sbv-viewport'); pane.body.tabIndex = 0;
        pane.playback = node('div', 'sbv-video'); pane.status = node('p', 'sbv-pane-status');
        pane.root.append(toolbar, pane.body, pane.playback, pane.status);
        pane.root.addEventListener('pointerdown', event => { select(pane); if (!event.target.closest('button,input,select,textarea')) focusPane(pane); }, true);
        pane.root.addEventListener('focusin', () => { if (active !== pane) select(pane); });
        pane.choose.disabled = !listMedia || !getAlbum;
        stage.append(pane.root); return pane;
    }
    function clearViewport(pane) {
        pane.detachSurface?.(); pane.detachSurface = null; pane.viewport?.destroy(); pane.viewport = null; pane.body.replaceChildren();
    }
    function stop(r) {
        // Auto-next and retries can remove the focused player control or map.
        if (r.player?.root.contains(doc.activeElement) || views(r).some(pane => pane.body !== doc.activeElement && pane.body.contains(doc.activeElement))) focusPane();
        for (const pane of views(r)) clearViewport(pane);
        r.player?.destroy(); r.player = null; r.nativeMedia = null;
    }
    function disposePane(pane) { pane.serial++; pane.dead = true; clearViewport(pane); pane.player?.destroy(); pane.player = null; pane.root.remove(); }
    function capture(pane) {
        return { mediaId: pane.mediaId, sequence: pane.sequence, queue: pane.queue, file: pane.file,
            auto: pane.auto, shuffle: pane.shuffle, repeat: pane.repeat, state: { ...pane.state }, pendingSequence: pane.pendingSequence,
            playbackState: pane.nativeMedia ? { time: pane.nativeMedia.currentTime, rate: pane.nativeMedia.playbackRate, volume: pane.nativeMedia.volume, muted: pane.nativeMedia.muted } : pane.playbackState };
    }
    function createViewport(pane, r, own) {
        clearViewport(pane);
        const file = r.file;
        if (!file || file.kind !== 'image' && file.kind !== 'video') return;
        let source = r.nativeMedia;
        if (file.kind === 'image') {
            source = node('img'); source.alt = file.title || ''; source.draggable = false; source.src = contentURL(file);
        }
        let fallback = false;
        pane.viewport = mountViewerViewport(pane.body, { source, state: pane.state, selected: pane === active,
            width: file.width, height: file.height, poster: thumbnailURL(file) || previewURL(file), onChange: controls,
            onError: () => {
                if (!valid(r, own)) return;
                if (file.kind === 'image') {
                    const url = previewURL(file);
                    if (!fallback && url && url !== source.getAttribute('src')) { fallback = true; source.src = url; }
                    else message(r, () => translate('viewer.cannotDisplayThisImageYouCanMoveToThe'));
                }
            } });
        if (r.player) pane.detachSurface = r.player.attachSurface(pane.viewport.surface, { focus: () => { select(pane); focusPane(pane); } });
    }
    async function show(r, autoplay = false, reset = true) {
        const own = ++r.serial; r.loading = true; r.autoplayRequested = autoplay; stop(r);
        if (reset) { r.playbackState = null; for (const pane of views(r)) Object.assign(pane.state, freshState()); }
        r.file = null; message(r, () => translate('gallery.loading')); controls();
        if (!r.queue?.current) { r.loading = false; message(r, () => translate('viewer.noFilesInTheAlbum')); controls(); return; }
        try {
            const file = await getFile(r.queue.current);
            if (!valid(r, own)) return;
            r.file = file; message(r, () => file.title || translate('gallery.file', { fileId: file.id }));
            if (file.kind === 'video' || file.kind === 'audio' && panes.length === 1 && !group) {
                r.player = mountVideoPlayer(r === group ? sharedHost : r.playback, { src: contentURL(file), kind: file.kind,
                    title: file.title || r.sequence?.title || translate('viewer.open'), poster: thumbnailURL(file) || previewURL(file),
                    keyboardSeek: false, externalScreen: file.kind === 'video', fullscreenAction: toggleExpanded });
                r.nativeMedia = r.player.video;
                if (file.kind === 'video') r.nativeMedia.preload = 'auto';
                if (!reset && r.playbackState) {
                    const media = r.nativeMedia, saved = r.playbackState;
                    media.playbackRate = saved.rate; media.volume = saved.volume; media.muted = saved.muted;
                    const restoreTime = () => { if (valid(r, own) && saved.time > 0) media.currentTime = Math.min(saved.time, Number.isFinite(media.duration) ? media.duration : saved.time); };
                    if (media.readyState >= 1) restoreTime(); else media.addEventListener('loadedmetadata', restoreTime, { once: true });
                }
                r.nativeMedia.addEventListener('ended', () => { if (valid(r, own) && r.auto && r.queue.hasNext) { r.queue.next(); void show(r, true); } });
                r.nativeMedia.addEventListener('error', () => { if (valid(r, own)) message(r, () => translate('viewer.cannotPlayThisFileYouCanMoveToThe')); });
            }
            for (const pane of views(r)) createViewport(pane, r, own);
            if (file.kind === 'audio' && (panes.length > 1 || group)) message(r, () => translate('viewer.audioSingleOnly'));
            else if (!['image', 'video', 'audio'].includes(file.kind)) message(r, () => translate('viewer.thisFileTypeCannotBeDisplayed'));
            if (r.nativeMedia && autoplay) r.nativeMedia.play()?.catch(() => { if (valid(r, own)) message(r, () => translate('viewer.pressPlayToStart')); });
        } catch (error) {
            if (!valid(r, own)) return;
            message(r, () => error.message || translate('viewer.couldNotLoadTheFile'));
            for (const pane of views(r)) button(() => translate('common.retry'), () => { focusPane(pane); void show(r, autoplay, reset); }, pane.body);
        } finally { if (valid(r, own)) { r.loading = false; controls(); } }
    }
    async function loadSequence(r, albumId, startId, autoplay = false) {
        const own = ++r.serial; r.loading = true; stop(r); r.file = null;
        r.pendingSequence = { albumId, startId, autoplay };
        message(r, () => translate('gallery.loading')); controls();
        try {
            const sequence = await getSequence(albumId);
            if (!valid(r, own)) return;
            r.mediaId = albumId; r.sequence = sequence; r.queue = createViewerQueue(sequence.ids, startId);
            r.pendingSequence = null;
            r.queue.setShuffle(Boolean(r.shuffle)); r.queue.setRepeat(Boolean(r.repeat));
            await show(r, autoplay);
        } catch (error) {
            if (!valid(r, own)) return;
            r.loading = false; message(r, () => error.message || translate('gallery.couldNotLoadTheAlbum'));
            for (const pane of views(r)) button(() => translate('common.retry'), () => { focusPane(pane); void loadSequence(r, albumId, startId, autoplay); }, pane.body);
            controls();
        }
    }
    function navigate(direction) {
        const r = resource();
        if (!r?.queue || r.loading || (direction < 0 ? !r.queue.hasPrevious : !r.queue.hasNext)) return;
        const playing = Boolean(r.nativeMedia && !r.nativeMedia.paused);
        focusPane(); if (direction < 0) r.queue.previous(); else r.queue.next();
        void show(r, playing);
    }
    function openPicker(pane) {
        if (!listMedia || !getAlbum) return;
        select(pane); picker?.destroy(); const target = group || pane;
        picker = mountViewerPicker(dialog, { listMedia, getAlbum, mediaId: target.mediaId,
            onClose: () => { picker = null; }, onSelect: selection => {
                if (destroyed || target.dead) return;
                for (const view of views(target)) Object.assign(view.state, freshState());
                focusPane(); void loadSequence(target, selection.mediaId, selection.fileId);
            } });
    }
    function rebuild(snapshots, chosen = 0) {
        for (const pane of panes) disposePane(pane);
        panes = snapshots.map(snapshot => newPane(snapshot)); stage.dataset.layout = String(panes.length);
        active = panes[Math.min(chosen, panes.length - 1)]; select(active); focusPane();
    }
    function setLayout(size) {
        if (destroyed || expanded || panes.length === size) return;
        picker?.destroy();
        const ordered = size < panes.length ? [active, ...panes.filter(pane => pane !== active)] : panes;
        const chosen = size < panes.length ? 0 : panes.indexOf(active);
        const snapshots = ordered.slice(0, size).map(capture);
        while (snapshots.length < size) snapshots.push(group ? { state: freshState() } : undefined);
        if (group) {
            rebuild(snapshots, chosen);
            for (const pane of panes) { bindText(pane.status, group.message); createViewport(pane, group, group.serial); }
        } else {
            const playing = ordered.map(pane => Boolean(pane.nativeMedia && !pane.nativeMedia.paused || pane.loading && pane.autoplayRequested));
            rebuild(snapshots, chosen);
            panes.forEach((pane, index) => {
                if (pane.pendingSequence) { const pending = pane.pendingSequence; void loadSequence(pane, pending.albumId, pending.startId, pending.autoplay); }
                else if (pane.queue) void show(pane, Boolean(playing[index]), false);
                else message(pane, () => translate('viewer.emptyPane'));
            });
        }
        controls();
    }
    function setMode(same) {
        if (destroyed || expanded || same === Boolean(group) || resource()?.loading || !resource()?.queue) return;
        picker?.destroy();
        const size = panes.length;
        if (same) {
            if (active.file?.kind === 'audio') return;
            savedSeparate = { panes: panes.map(capture), chosen: panes.indexOf(active) };
            const snapshot = capture(active), wasPlaying = Boolean(active.nativeMedia && !active.nativeMedia.paused);
            const queue = createViewerQueue(snapshot.sequence.ids, snapshot.queue.current);
            queue.setShuffle(snapshot.shuffle); queue.setRepeat(snapshot.repeat);
            group = { ...snapshot, queue, serial: 0, dead: false, loading: false, file: null, player: null };
            rebuild(Array.from({ length: size }, () => ({ state: { ...snapshot.state } })), savedSeparate.chosen);
            void show(group, wasPlaying, false);
        } else {
            const oldGroup = group; oldGroup.dead = true; oldGroup.serial++; stop(oldGroup); group = null;
            const snapshots = savedSeparate?.panes.slice(0, size) || [];
            while (snapshots.length < size) snapshots.push(undefined);
            rebuild(snapshots, Math.min(savedSeparate?.chosen || 0, size - 1)); savedSeparate = null;
            panes.forEach(pane => {
                if (pane.pendingSequence) { const pending = pane.pendingSequence; void loadSequence(pane, pending.albumId, pending.startId, false); }
                else if (pane.queue) void show(pane, false, false);
                else message(pane, () => translate('viewer.emptyPane'));
            });
        }
        controls();
    }
    function leaveExpanded() {
        expanded = false; dialog.classList.remove('sbv-expanded'); controls(); focusPane();
        for (const pane of panes) pane.viewport?.render();
    }
    function toggleExpanded() {
        if (expanded) { if (doc.fullscreenElement === dialog) doc.exitFullscreen?.()?.catch(() => {}); leaveExpanded(); return; }
        expanded = true; dialog.classList.add('sbv-expanded'); controls(); focusPane();
        dialog.requestFullscreen?.()?.catch(() => {});
        for (const pane of panes) pane.viewport?.render();
    }
    const fullscreenChanged = () => { if (!doc.fullscreenElement && expanded) leaveExpanded(); };
    const cancel = event => { if (picker) return; if (expanded) { event.preventDefault(); toggleExpanded(); } };
    const keydown = event => {
        if (event.defaultPrevented || isComposing(event) || !dialog.contains(event.target) || blockingDialog(doc) !== dialog || isTextInput(event.target)) return;
        // The minimap rectangle owns arrows while it has keyboard focus.
        if (event.target.closest?.('[data-minimap-control]') && !event.ctrlKey && !event.metaKey && !event.altKey && event.key.startsWith('Arrow')) return;
        const item = matchShortcut(event, 'viewer');
        if (item) {
            event.preventDefault(); event.stopImmediatePropagation();
            if (item.id === 'viewer.previous' || item.id === 'viewer.next') navigate(item.id === 'viewer.previous' ? -1 : 1);
            else zoom(active, item.id === 'viewer.zoomIn' ? 1 : item.id === 'viewer.zoomOut' ? -1 : 0);
            return;
        }
        const mediaKey = matchShortcut(event, 'player');
        if (!mediaKey || mediaKey.id === 'player.play' && event.target.closest?.('button')) return;
        const r = resource(); if (mediaKey.id !== 'player.fullscreen' && !r?.player) return;
        event.preventDefault(); event.stopImmediatePropagation();
        if (mediaKey.id === 'player.fullscreen') toggleExpanded(); else r.player.keyboardAction(mediaKey.id);
    };
    doc.addEventListener('keydown', keydown, true); doc.addEventListener('fullscreenchange', fullscreenChanged); dialog.addEventListener('cancel', cancel);
    const stopLanguage = localizeWidget(dialog);
    function destroy() {
        if (destroyed) return; destroyed = true; picker?.destroy(); stopLanguage();
        if (group) { group.dead = true; group.serial++; stop(group); }
        for (const pane of panes) disposePane(pane);
        doc.removeEventListener('keydown', keydown, true); doc.removeEventListener('fullscreenchange', fullscreenChanged); dialog.removeEventListener('cancel', cancel); dialog.removeEventListener('close', closed);
        if (doc.fullscreenElement === dialog) doc.exitFullscreen?.()?.catch(() => {});
        dialog.remove(); doc.body.style.overflow = oldOverflow;
        if (previousFocus?.isConnected) previousFocus.focus({ preventScroll: true });
    }
    function closed() { if (destroyed) return; destroy(); onClose(); }
    dialog.addEventListener('close', closed); dialog.showModal();
    panes = [newPane()]; stage.dataset.layout = '1'; active = panes[0]; select(active); close.focus();
    void loadSequence(active, mediaId, fileId, true);
    return { destroy, dialog, get currentFile() { return resource()?.file; } };
}
