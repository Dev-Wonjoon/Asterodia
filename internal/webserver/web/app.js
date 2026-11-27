import { translate } from './i18n.js';
import { mountMediaViewer } from './viewer.js';
'use strict';
import { mountMediaEditor } from './editor.js';
import { mountSearchSuggestions } from './search.js';
import { mountVideoPlayer } from './player.js';
import { readExpandedAlbums, saveExpandedAlbums } from './album-state.js';
const el = id => document.getElementById(id);
const state = { offset: 0, q: '', total: 0, next: false, busy: false, serial: 0, controller: null, media: null, fileOffset: 0, fileNext: false, detailSerial: 0 };
state.expanded = readExpandedAlbums();
state.items = []; state.moreError = false; state.retryAppend = false;
let videoPlayers = [];
state.tags = []; state.authors = [];
function searchParameters() { const query = new URLSearchParams(); for (const tag of state.tags) query.append('tag', tag); for (const author of state.authors) query.append('author', author); return query; }
async function request(path, signal) {
    let response;
    try {
        response = await fetch(path, { signal, credentials: 'same-origin', headers: { Accept: 'application/json' } });
    }
    catch (error) {
        if (error.name === 'AbortError')
            throw error;
        throw new Error(translate("common.couldNotConnectToTheServerCheckTheServer"));
    }
    if (!response.ok)
        throw new Error(response.status === 404 ? translate("common.theRegisteredMediaCouldNotBeFound") : translate("common.couldNotConnectToTheServerCheckTheServer"));
    return response.json();
}
function text(tag, value) {
    const node = document.createElement(tag);
    node.textContent = value;
    return node;
}
function fileURL(id, variant) {
    if (!Number.isSafeInteger(id) || id <= 0)
        throw new Error(translate("common.checkTheFileId"));
    return '/api/files/' + id + '/' + variant;
}
function updateFeed() {
    el('refresh').disabled = state.busy;
    el('load-more').disabled = state.busy;
    el('load-more').hidden = !state.next && !state.moreError;
    el('load-more').textContent = state.moreError ? translate("common.retry") : translate("gallery.loadMore");
    el('feed-status').textContent = state.busy ? translate("gallery.loading") : translate("web.items", { loaded: state.items.length, total: state.total }) + (!state.next && state.items.length ? translate("common.allItemsLoaded") : '');
    el('gallery').setAttribute('aria-busy', String(state.busy));
    feedObserver?.disconnect();
    if (!state.busy && state.next && !state.moreError && !el('detail').open) feedObserver?.observe(el('gallery-sentinel'));
}
const itemKey = item => item.galleryFile ? 'album:' + item.id + ':file:' + item.galleryFile.id : 'album:' + item.id;
async function load({ append = false, count = Math.max(24, state.offset) } = {}) {
    if (append && (state.busy || !state.next)) return false;
    state.controller?.abort();
    state.controller = new AbortController();
    const serial = ++state.serial;
    const expanded = [...state.expanded];
    let offset = append ? state.offset : 0;
    const rows = append ? [...state.items] : [];
    let total = state.total, hasNext = false;
    state.busy = true;
    el('message').textContent = '';
    updateFeed();
    try {
        do {
            const query = searchParameters(); query.set('limit', '24'); query.set('offset', String(offset)); query.set('q', state.q);
            query.set('gallery', '1'); query.set('expanded', expanded.join(','));
            const page = await request('/api/library?' + query, state.controller.signal);
            if (serial !== state.serial) return null;
            if (append && page.offset !== undefined && page.offset !== offset) return load();
            if (page.hasNextPage && !page.data.length) throw new Error(translate("common.couldNotLoadTheNextItemsPleaseTryAgain"));
            rows.push(...page.data); offset += page.data.length;
            total = page.total; hasNext = page.hasNextPage;
        } while (!append && hasNext && offset < count);
        const existing = new Set(append ? state.items.map(itemKey) : []);
        const unique = [...new Map(rows.map(item => [itemKey(item), item])).values()];
        state.items = unique; state.offset = offset; state.total = total; state.next = hasNext; state.moreError = false;
        el('count').textContent = translate("web.itemsInTotal", { count: total });
        renderGallery(append ? unique.filter(item => !existing.has(itemKey(item))) : unique, append);
        el('message').textContent = unique.length ? '' : state.q ? translate("shortcuts.noSearchResults") : translate("common.noMediaHasBeenRegisteredYet");
        saveExpandedAlbums(expanded);
        return true;
    } catch (error) {
        if (serial === state.serial && error.name !== 'AbortError') {
            el('message').textContent = error.message;
            state.moreError = true; state.retryAppend = append;
        }
        return serial === state.serial ? false : null;
    } finally {
        if (serial === state.serial) { state.busy = false; updateFeed(); }
    }
}
function renderGallery(items, append) {
    if (!append) el('gallery').replaceChildren();
        for (const item of items) {
            const card = document.createElement('article'); card.className = 'album-card'; card.dataset.albumId = String(item.id);
            const button = document.createElement('button');
            button.type = 'button';
            button.className = 'card';
            button.addEventListener('click', () => openDetail(item.id, item.galleryFileOffset));
            const cover = document.createElement('div');
            cover.className = 'cover';
            cover.append(text('span', item.cover?.kind === 'audio' ? translate("media.audio") : item.cover?.kind === 'video' ? translate("media.video") : translate("common.media")));
            const file = item.galleryFile;
            if (file || item.cover) {
                const image = document.createElement('img');
                image.src = fileURL(file?.id ?? item.cover.fileId, 'thumbnail');
                image.alt = '';
                image.loading = 'lazy';
                image.addEventListener('error', () => image.remove(), { once: true });
                cover.append(image);
            }
            const body = document.createElement('div');
            body.className = 'card-body';
            body.append(text('h2', file ? file.title || translate("web.file", { number: item.galleryFileOffset + 1 }) : item.title), text('small', file ? `${item.title} · ${item.galleryFileOffset + 1} / ${item.fileCount}` : translate("web.files", { count: item.fileCount })));
            button.append(cover, body);
            card.append(button);
            const watch = text('button', translate("viewer.open")); watch.type = 'button'; watch.className = 'album-toggle'; watch.disabled = item.fileCount === 0;
            watch.addEventListener('click', () => openViewer(item.id, item.galleryFile?.id)); card.append(watch);

            if (item.fileCount > 1 || file) {
                const toggle = text('button', file ? translate("common.collapseAlbum") : translate("common.expandAlbum")); toggle.type = 'button'; toggle.className = 'album-toggle'; toggle.setAttribute('aria-expanded', String(Boolean(file))); toggle.setAttribute('aria-label', item.title + ' ' + toggle.textContent);
                toggle.addEventListener('click', async () => {
                    if (state.busy) return;
                    const previous = state.expanded;
                    state.expanded = previous.includes(item.id) ? previous.filter(id => id !== item.id) : [...previous, item.id];
                    const top = card.getBoundingClientRect().top;
                    const shown = state.items.filter(row => row.id === item.id && row.galleryFile).length;
                    const count = previous.includes(item.id) ? state.offset - Math.max(0, shown - 1) : state.offset;
                    const loaded = await load({ count });
                    if (loaded === false) state.expanded = previous;
                    if (loaded) {
                        const anchor = el('gallery').querySelector(`[data-album-id="${item.id}"]`);
                        if (anchor) window.scrollBy(0, anchor.getBoundingClientRect().top - top);
                    }
                });
                card.append(toggle);
            }
            el('gallery').append(card);
        }
}
function clearFiles() {
    videoPlayers.forEach(player => player.destroy()); videoPlayers = [];
    for (const player of el('files').querySelectorAll('video,audio')) {
        player.pause();
        player.removeAttribute('src');
        player.load();
    }
    el('files').replaceChildren();
}
async function openDetail(id, fileOffset = 0) {
    if (!Number.isSafeInteger(id) || id <= 0)
        return;
    state.media = id;
    state.fileOffset = fileOffset;
    if (!el('detail').open)
        el('detail').showModal();
    await loadFiles();
}
async function loadFiles() {
    const serial = ++state.detailSerial;
    clearFiles();
    el('detail-message').textContent = translate("web.loadingMedia");
    el('file-previous').disabled = true;
    el('file-next').disabled = true;
    try {
        const detail = await request('/api/library/' + state.media + '?fileOffset=' + state.fileOffset);
        if (serial !== state.detailSerial || !el('detail').open)
            return;
        el('detail-title').textContent = detail.title;
        state.fileNext = detail.files.hasNextPage;
        for (const file of detail.files.data) {
            const figure = document.createElement('figure');
            const kind = ['image', 'video', 'audio'].includes(file.kind) ? file.kind : null;
            if (kind) {
                const player = document.createElement(kind === 'video' ? 'div' : kind === 'image' ? 'img' : kind);
                if (kind === 'video') {
                    videoPlayers.push(mountVideoPlayer(player, { src: fileURL(file.id, 'content'), title: file.title || detail.title, poster: fileURL(file.id, 'thumbnail') }));
                } else {
                    if (kind === 'image') {
                        player.alt = file.title || detail.title;
                        player.loading = 'lazy';
                    }
                    else {
                        player.controls = true;
                        player.setAttribute('controlsList', 'nodownload');
                        player.preload = 'none';

                    }
                    player.src = fileURL(file.id, 'content');
                    let fallback = false;
                    player.addEventListener('error', () => {
                        if (kind === 'image' && !fallback) {
                            fallback = true;
                            player.src = fileURL(file.id, 'preview');
                            return;
                        }
                        player.replaceWith(text('p', translate("common.thisBrowserCannotDisplayOrPlayThisFormat")));
                    });
                }
                figure.append(player);
            }
            else
                figure.append(text('p', translate("common.thisMediaFormatIsNotSupported")));
            figure.append(text('figcaption', file.title || translate("web.file", { number: file.id })));
            const watch = text('button', translate("viewer.open")); watch.type = 'button'; watch.addEventListener('click', () => openViewer(state.media, file.id)); figure.append(watch);

            if (file.kind === 'video'||file.kind==='image'&&['gif','webp'].includes(file.detectedFormat)) {
                const edit = text('button',file.kind==='image'?translate("editor.editAnimation"):translate("editor.createGifOrTrimVideo"));
                edit.type = 'button';
                edit.dataset.edit = 'true';
                edit.hidden = !editAccess.enabled;
                edit.addEventListener('click', () => openEditor({ id: file.id, title: file.title || detail.title, duration: file.duration,kind:file.kind,detectedFormat:file.detectedFormat,nativeGIF:file.detectedFormat==='gif',nativeWebP:file.detectedFormat==='webp', src: fileURL(file.id, 'content') }));
                figure.append(edit);
            }
            el('files').append(figure);
        }
        el('detail-message').textContent = translate("web.fileCount", { count: detail.files.total });
        el('file-page').textContent = translate("web.page", { number: Math.floor(state.fileOffset / 24) + 1 });
        el('file-previous').disabled = state.fileOffset === 0;
        el('file-next').disabled = !state.fileNext;
    }
    catch (error) {
        if (serial === state.detailSerial)
            el('detail-message').textContent = error.message;
    }
}
let searchTimer;
function applySearch() { clearTimeout(searchTimer); state.q = el('keyword').value; state.offset = 0; void load({ count: 24 }); }
function renderSearchFilters() {
    el('search-filters').replaceChildren();
    for (const [field,label] of [['tags',translate("media.tags")],['authors',translate("media.author")]]) for (const value of state[field]) {
        const button = text('button',label + ': ' + value + ' ×'); button.type = 'button'; button.setAttribute('aria-label',label + ' ' + value + translate("common.remove"));
        button.addEventListener('click',()=>{state[field]=state[field].filter(item=>item!==value); renderSearchFilters(); applySearch();}); el('search-filters').append(button);
    }
}
mountSearchSuggestions({ input: el('keyword'), form: el('search'), request, parameters: searchParameters,
    onInput(value,composing) { clearTimeout(searchTimer); if (!composing) searchTimer = setTimeout(applySearch,300); },
    onApply(item) { if (item.kind === 'tag' || item.kind === 'author') { const field=item.kind==='tag'?'tags':'authors'; if(!state[field].includes(item.value))state[field].push(item.value); el('keyword').value=''; renderSearchFilters(); } else el('keyword').value=item.value; applySearch(); }
});
el('refresh').addEventListener('click', () => void load());
el('load-more').addEventListener('click', () => void load({ append: state.moreError ? state.retryAppend : true }));
const feedObserver = typeof IntersectionObserver === 'function' ? new IntersectionObserver(entries => {
    if (entries.some(entry => entry.isIntersecting) && !state.busy && state.next && !state.moreError && !el('detail').open) void load({ append: true });
}, { rootMargin: '0px 0px 300px 0px' }) : null;
el('close').addEventListener('click', () => el('detail').close());
el('detail').addEventListener('close', () => {
    state.detailSerial++;
    clearFiles(); updateFeed();
});
el('file-previous').addEventListener('click', () => {
    state.fileOffset = Math.max(0, state.fileOffset - 24);
    void loadFiles();
});
el('file-next').addEventListener('click', () => {
    state.fileOffset += 24;
    void loadFiles();
});
document.addEventListener('play', event => {
    for (const player of document.querySelectorAll('video,audio'))
        if (player !== event.target)
            player.pause();
}, true);
let editAccess = { enabled: false, csrf: '' };
let openedEditor = null;
async function editRequest(name, value, read = false, attempt = 0) {
    const path = '/api/edit/' + name + (read && value?.id ? '?'+new URLSearchParams(Object.entries(value).map(([k,v])=>[k,String(v)])).toString() : '');
    const response = await fetch(path, read ? { credentials: 'same-origin' } : { method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json', 'X-Asterodia-CSRF': editAccess.csrf }, body: JSON.stringify(value) });
    if(response.status===429&&attempt<5){await new Promise(resolve=>setTimeout(resolve,1200));return editRequest(name,value,read,attempt+1);}
    if (!response.ok && !response.headers.get('Content-Type')?.includes('application/json'))
        throw new Error(await response.text());
    return response.json();
}
const editAPI = { frames: value => editRequest('frames',value), frameStatus: value => editRequest('frames',value,true), selection: value => editRequest('selection',value), resources: value => editRequest('resources',value), prepare: value => editRequest('prepare', value), getSession: value => editRequest('session', value, true), create: value => editRequest('create', value), getJob: value => editRequest('job', value, true), list: () => editRequest('list', null, true), cancel: value => editRequest('cancel', value), release: value => editRequest('release', value) };
function openEditor(file) { if (openedEditor)
    return; openedEditor = mountMediaEditor(el('editor-host'), { api: editAPI, file, onClose: () => { openedEditor = null; } }); }
el('edit-history').addEventListener('click', () => openEditor());
async function loadEditAccess() { try {
    editAccess = await request('/api/edit/status');
    el('access-mode').textContent = editAccess.enabled ? translate("common.editingAllowed") : translate("common.readOnly");
    el('edit-history').hidden = !editAccess.enabled;
    for (const button of document.querySelectorAll('button[data-edit]'))
        button.hidden = !editAccess.enabled;
}
catch {
    editAccess = { enabled: false, csrf: '' };
    el('edit-history').hidden = true;
} }
void loadEditAccess().then(() => load());
setInterval(() => void loadEditAccess(), 5000);

let mediaViewer = null;
function openViewer(mediaId, fileId) {
    mediaViewer?.destroy();
    mediaViewer = mountMediaViewer({ mediaId, fileId,
        getSequence: id => request('/api/library/' + id + '/sequence'),
        getFile: id => request('/api/files/' + id + '/info'),
        listMedia: ({ keyword, offset }) => request('/api/library?' + new URLSearchParams({ q: keyword, offset: String(offset), limit: '50' })),
        getAlbum: (id, offset) => request('/api/library/' + id + '?fileOffset=' + offset),
        contentURL: file => fileURL(file.id, 'content'),
        previewURL: file => fileURL(file.id, 'preview'),
        thumbnailURL: file => fileURL(file.id, 'thumbnail'),
        onClose: () => { mediaViewer = null; },
    });
}
