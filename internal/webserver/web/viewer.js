import { translate, bindText, bindAttribute, localizeWidget } from './i18n.js'
import { mountVideoPlayer } from './player.js';
import { createViewerQueue } from './viewer-queue.js';
import { isTextInput, isComposing, matchShortcut } from './shortcuts.js';

// Adapter methods return data, throwing on failure. This keeps desktop paths out of the browser API.
export function mountMediaViewer({ mediaId, fileId, getSequence, getFile, contentURL, previewURL, thumbnailURL, onClose = () => {} }) {
    const doc = document, previousFocus = doc.activeElement, oldOverflow = doc.body.style.overflow;
    for (const media of doc.querySelectorAll('audio,video')) media.pause();
    const dialog = doc.createElement('dialog'); dialog.className = 'sb-viewer'; dialog.dataset.detailTrigger = 'true';
    bindAttribute(dialog, 'aria-label', () => translate("viewer.mediaViewer"));
    const node = (tag, cls, value) => { const n = doc.createElement(tag); if (cls) n.className = cls; if (typeof value === 'function') bindText(n, value); else if (value) bindText(n, () => value); return n; };
    const header = node('header', 'sbv-header'), title = node('h2', '', () => (translate("viewer.open"))), close = node('button', '', () => (translate("viewer.closeEsc"))); close.type = 'button';
    header.append(title, close);
    const stage = node('div', 'sbv-stage'), status = node('p', 'sbv-status', () => (translate("gallery.loading"))), footer = node('footer', 'sbv-footer'); status.setAttribute('role', 'status');
    const previous = node('button', '', () => (translate("viewer.previous"))), next = node('button', '', () => (translate("viewer.next"))), count = node('span', 'sbv-count');
    previous.type = next.type = 'button';
    const options = node('div', 'sbv-options');
    function option(label) { const wrap = node('label'), input = node('input'), text = node('span', '', label); input.type = 'checkbox'; wrap.append(input, text); options.append(wrap); return input; }
    const auto = option(() => (translate("viewer.autoNext"))), shuffle = option(() => (translate("viewer.shuffle"))), repeat = option(() => (translate("viewer.repeatAlbum")));
    footer.append(previous, count, next, options); dialog.append(header, stage, status, footer); doc.body.append(dialog); doc.body.style.overflow = 'hidden';
    let destroyed = false, serial = 0, queue, player, nativeMedia, currentFile, loading = false;
    const stop = () => { player?.destroy(); player = null; if (nativeMedia) { nativeMedia.pause(); nativeMedia.removeAttribute('src'); nativeMedia.load(); nativeMedia = null; } stage.replaceChildren(); };
    function controls() {
        previous.disabled = !queue?.hasPrevious || loading; next.disabled = !queue?.hasNext || loading;
        shuffle.disabled = repeat.disabled = !queue?.count;
        bindText(count, () => queue?.count ? `${queue.index + 1} / ${queue.count}` : '0 / 0');
    }
    async function show(autoplay = false) {
        const own = ++serial; loading = true; stop(); bindText(status, () => translate("gallery.loading")); controls();
        if (!queue?.current) { loading = false; bindText(status, () => translate("viewer.noFilesInTheAlbum")); controls(); return; }
        try {
            const file = await getFile(queue.current);
            if (destroyed || serial !== own) return;
            currentFile = file; bindText(status, () => file.title || translate("gallery.file", { fileId: file.id }));
            if (file.kind === 'video') {
                const host = node('div', 'sbv-video'); stage.append(host);
                player = mountVideoPlayer(host, { src: contentURL(file), title: file.title || title.textContent, poster: thumbnailURL(file), keyboardSeek: false });
                nativeMedia = player.video;
            } else if (file.kind === 'audio') {
                const host = node('div', 'sbv-audio'), artwork = node('div', 'sbv-artwork', '♫');
                const cover = thumbnailURL(file);
                if (cover) { const img = node('img'); img.alt = ''; img.src = cover; img.addEventListener('error', () => img.remove(), { once: true }); artwork.append(img); }
                nativeMedia = node('audio'); nativeMedia.controls = true; nativeMedia.preload = 'metadata'; nativeMedia.src = contentURL(file); bindAttribute(nativeMedia, 'aria-label', () => file.title || translate("media.audio"));
                host.append(artwork, node('h3', '', () => (file.title || translate("media.audio"))), nativeMedia); stage.append(host);
            } else if (file.kind === 'image') {
                const img = node('img', 'sbv-image'); img.alt = file.title || ''; img.src = contentURL(file);
                let fallback = false;
                img.addEventListener('error', () => { const url = previewURL(file); if (!fallback && url && url !== img.src) { fallback = true; img.src = url; } else bindText(status, () => translate("viewer.cannotDisplayThisImageYouCanMoveToThe")); }); stage.append(img);
            } else stage.append(node('p', '', () => (translate("viewer.thisFileTypeCannotBeDisplayed"))));
            if (nativeMedia) {
                const media = nativeMedia;
                media.addEventListener('ended', () => { if (!destroyed && serial === own && auto.checked && queue.hasNext) { queue.next(); void show(true); } });
                media.addEventListener('error', () => { if (serial === own) bindText(status, () => translate("viewer.cannotPlayThisFileYouCanMoveToThe")); });
                if (autoplay) media.play()?.catch(() => { if (!destroyed && serial === own) bindText(status, () => translate("viewer.pressPlayToStart")); });
            }
        } catch (error) { if (!destroyed && serial === own) { bindText(status, () => error.message || translate("viewer.couldNotLoadTheFile")); const retry = node('button', '', () => (translate("common.retry"))); retry.onclick = () => void show(autoplay); stage.append(retry); } }
        finally { if (!destroyed && serial === own) { loading = false; controls(); } }
    }
    function navigate(direction) {
        if (!queue || loading || (direction < 0 ? !queue.hasPrevious : !queue.hasNext)) return;
        const playing = nativeMedia && !nativeMedia.paused;
        if (direction < 0) queue.previous(); else queue.next();
        void show(Boolean(playing));
    }
    previous.onclick = () => navigate(-1); next.onclick = () => navigate(1);
    shuffle.onchange = () => { queue?.setShuffle(shuffle.checked); controls(); }; repeat.onchange = () => { queue?.setRepeat(repeat.checked); controls(); };
    const keydown = event => {
        if (event.defaultPrevented || isComposing(event) || !dialog.contains(event.target) || [...doc.querySelectorAll('dialog[open]')].at(-1) !== dialog) return;
        // Let text/range inputs retain their own editing controls. Buttons and the media surface navigate files.
        if (isTextInput(event.target)) return;
        const item = matchShortcut(event, 'viewer');
        if (item) { event.preventDefault(); event.stopImmediatePropagation(); navigate(item.id === 'viewer.previous' ? -1 : 1); return; }
        const mediaKey = matchShortcut(event, 'player');
        if (!mediaKey || mediaKey.id === 'player.play' && event.target.closest?.('button')) return;
        if (mediaKey.id === 'player.fullscreen' && !player) {
            event.preventDefault(); event.stopImmediatePropagation();
            if (doc.fullscreenElement === dialog) doc.exitFullscreen?.()?.catch(() => {});
            else dialog.requestFullscreen?.()?.catch(() => {});
            return;
        }
        if (!nativeMedia) return;
        event.preventDefault(); event.stopImmediatePropagation();
        if (player) player.keyboardAction(mediaKey.id);
        else {
            if (mediaKey.id === 'player.play' || mediaKey.id === 'player.playAlternative') { if (nativeMedia.paused) nativeMedia.play()?.catch(() => {}); else nativeMedia.pause(); }
            if (mediaKey.id === 'player.mute') nativeMedia.muted = !nativeMedia.muted;
            if (mediaKey.id === 'player.backward' || mediaKey.id === 'player.forward') nativeMedia.currentTime = Math.max(0, Math.min(Number.isFinite(nativeMedia.duration) ? nativeMedia.duration : Infinity, nativeMedia.currentTime + (mediaKey.id === 'player.backward' ? -5 : 5)));
        }
    };
    doc.addEventListener('keydown', keydown, true);
    const stopLanguage = localizeWidget(dialog);
    function destroy() {
        if (destroyed) return; stopLanguage(); destroyed = true; serial++; stop(); doc.removeEventListener('keydown', keydown, true); dialog.removeEventListener('close', closed); dialog.remove(); doc.body.style.overflow = oldOverflow;
        if (previousFocus?.isConnected) previousFocus.focus({ preventScroll: true });
    }
    // Effect cleanup (including React StrictMode's setup/cleanup check) only
    // releases resources. Notify the parent only when the user closes the dialog.
    function closed() { if (destroyed) return; destroy(); onClose(); }
    close.onclick = () => dialog.close(); dialog.addEventListener('close', closed); dialog.showModal(); close.focus();
    void (async () => {
        try { const sequence = await getSequence(mediaId); if (destroyed) return; bindText(title, () => sequence.title); queue = createViewerQueue(sequence.ids, fileId); controls(); await show(); }
        catch (error) { if (!destroyed) { bindText(status, () => error.message || translate("gallery.couldNotLoadTheAlbum")); controls(); } }
    })();
    controls();
    return { destroy, dialog, get currentFile() { return currentFile; } };
}
