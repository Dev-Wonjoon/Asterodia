import { mountVideoPlayer } from './player.js';
import { createViewerQueue } from './viewer-queue.js';

// Adapter methods return data, throwing on failure. This keeps desktop paths out of the browser API.
export function mountMediaViewer({ mediaId, fileId, getSequence, getFile, contentURL, previewURL, thumbnailURL, onClose = () => {} }) {
    const doc = document, previousFocus = doc.activeElement, oldOverflow = doc.body.style.overflow;
    for (const media of doc.querySelectorAll('audio,video')) media.pause();
    const dialog = doc.createElement('dialog'); dialog.className = 'sb-viewer'; dialog.dataset.detailTrigger = 'true';
    dialog.setAttribute('aria-label', '미디어 감상');
    const node = (tag, cls, value) => { const n = doc.createElement(tag); if (cls) n.className = cls; if (value) n.textContent = value; return n; };
    const header = node('header', 'sbv-header'), title = node('h2', '', '감상하기'), close = node('button', '', '닫기 · Esc'); close.type = 'button';
    header.append(title, close);
    const stage = node('div', 'sbv-stage'), status = node('p', 'sbv-status', '불러오는 중…'), footer = node('footer', 'sbv-footer'); status.setAttribute('role', 'status');
    const previous = node('button', '', '← 이전'), next = node('button', '', '다음 →'), count = node('span', 'sbv-count');
    previous.type = next.type = 'button';
    const options = node('div', 'sbv-options');
    function option(label) { const wrap = node('label'), input = node('input'); input.type = 'checkbox'; wrap.append(input, doc.createTextNode(label)); options.append(wrap); return input; }
    const auto = option('자동 다음'), shuffle = option('셔플'), repeat = option('앨범 반복');
    footer.append(previous, count, next, options); dialog.append(header, stage, status, footer); doc.body.append(dialog); doc.body.style.overflow = 'hidden';
    let destroyed = false, serial = 0, queue, player, nativeMedia, currentFile, loading = false;
    const stop = () => { player?.destroy(); player = null; if (nativeMedia) { nativeMedia.pause(); nativeMedia.removeAttribute('src'); nativeMedia.load(); nativeMedia = null; } stage.replaceChildren(); };
    function controls() {
        previous.disabled = !queue?.hasPrevious || loading; next.disabled = !queue?.hasNext || loading;
        shuffle.disabled = repeat.disabled = !queue?.count;
        count.textContent = queue?.count ? `${queue.index + 1} / ${queue.count}` : '0 / 0';
    }
    async function show(autoplay = false) {
        const own = ++serial; loading = true; stop(); status.textContent = '불러오는 중…'; controls();
        if (!queue?.current) { loading = false; status.textContent = '앨범에 파일이 없습니다.'; controls(); return; }
        try {
            const file = await getFile(queue.current);
            if (destroyed || serial !== own) return;
            currentFile = file; status.textContent = file.title || `파일 ${file.id}`;
            if (file.kind === 'video') {
                const host = node('div', 'sbv-video'); stage.append(host);
                player = mountVideoPlayer(host, { src: contentURL(file), title: file.title || title.textContent, poster: thumbnailURL(file), keyboardSeek: false });
                nativeMedia = player.video;
            } else if (file.kind === 'audio') {
                const host = node('div', 'sbv-audio'), artwork = node('div', 'sbv-artwork', '♫');
                const cover = thumbnailURL(file);
                if (cover) { const img = node('img'); img.alt = ''; img.src = cover; img.addEventListener('error', () => img.remove(), { once: true }); artwork.append(img); }
                nativeMedia = node('audio'); nativeMedia.controls = true; nativeMedia.preload = 'metadata'; nativeMedia.src = contentURL(file); nativeMedia.setAttribute('aria-label', file.title || '오디오');
                host.append(artwork, node('h3', '', file.title || '오디오'), nativeMedia); stage.append(host);
            } else if (file.kind === 'image') {
                const img = node('img', 'sbv-image'); img.alt = file.title || ''; img.src = contentURL(file);
                let fallback = false;
                img.addEventListener('error', () => { const url = previewURL(file); if (!fallback && url && url !== img.src) { fallback = true; img.src = url; } else status.textContent = '이 이미지를 표시할 수 없습니다. 이전·다음으로 이동할 수 있습니다.'; }); stage.append(img);
            } else stage.append(node('p', '', '표시를 지원하지 않는 파일입니다.'));
            if (nativeMedia) {
                const media = nativeMedia;
                media.addEventListener('ended', () => { if (!destroyed && serial === own && auto.checked && queue.hasNext) { queue.next(); void show(true); } });
                media.addEventListener('error', () => { if (serial === own) status.textContent = '이 파일을 재생할 수 없습니다. 이전·다음으로 이동할 수 있습니다.'; });
                if (autoplay) media.play()?.catch(() => { if (!destroyed && serial === own) status.textContent = '재생 버튼을 눌러 시작해 주세요.'; });
            }
        } catch (error) { if (!destroyed && serial === own) { status.textContent = error.message || '파일을 불러오지 못했습니다.'; const retry = node('button', '', '다시 시도'); retry.onclick = () => void show(autoplay); stage.append(retry); } }
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
        // Let text/range inputs retain their own editing controls. Buttons and the media surface navigate files.
        if (event.target.closest?.('input,textarea,select,[contenteditable="true"]')) return;
        if (event.key === 'ArrowLeft' || event.key === 'ArrowRight') { event.preventDefault(); event.stopImmediatePropagation(); navigate(event.key === 'ArrowLeft' ? -1 : 1); }
    };
    doc.addEventListener('keydown', keydown, true);
    function destroy() {
        if (destroyed) return; destroyed = true; serial++; stop(); doc.removeEventListener('keydown', keydown, true); dialog.removeEventListener('close', closed); dialog.remove(); doc.body.style.overflow = oldOverflow;
        if (previousFocus?.isConnected) previousFocus.focus({ preventScroll: true });
    }
    // Effect cleanup (including React StrictMode's setup/cleanup check) only
    // releases resources. Notify the parent only when the user closes the dialog.
    function closed() { if (destroyed) return; destroy(); onClose(); }
    close.onclick = () => dialog.close(); dialog.addEventListener('close', closed); dialog.showModal(); close.focus();
    void (async () => {
        try { const sequence = await getSequence(mediaId); if (destroyed) return; title.textContent = sequence.title; queue = createViewerQueue(sequence.ids, fileId); controls(); await show(); }
        catch (error) { if (!destroyed) { status.textContent = error.message || '앨범을 불러오지 못했습니다.'; controls(); } }
    })();
    controls();
    return { destroy, dialog, get currentFile() { return currentFile; } };
}
