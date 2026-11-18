'use strict';
const el = id => document.getElementById(id);
const state = { offset: 0, q: '', total: 0, next: false, busy: false, serial: 0, controller: null, media: null, fileOffset: 0, fileNext: false, detailSerial: 0 };
async function request(path, signal) {
    let response;
    try {
        response = await fetch(path, { signal, credentials: 'same-origin', headers: { Accept: 'application/json' } });
    } catch (error) {
        if (error.name === 'AbortError') throw error;
        throw new Error('서버에 연결하지 못했습니다. PC 앱의 서버 상태를 확인해 주세요.');
    }
    if (!response.ok)
        throw new Error(response.status === 404 ? '등록된 미디어를 찾지 못했습니다.' : '서버에 연결하지 못했습니다. PC 앱의 서버 상태를 확인해 주세요.');
    return response.json();
}
function text(tag, value) {
    const node = document.createElement(tag);
    node.textContent = value;
    return node;
}
function fileURL(id, variant) {
    if (!Number.isSafeInteger(id) || id <= 0)
        throw new Error('파일 ID를 확인하세요.');
    return '/api/files/' + id + '/' + variant;
}
function updatePaging() {
    el('previous').disabled = state.busy || state.offset === 0;
    el('next').disabled = state.busy || !state.next;
    el('refresh').disabled = state.busy;
    el('page').textContent = (Math.floor(state.offset / 24) + 1) + ' 페이지';
    el('gallery').setAttribute('aria-busy', String(state.busy));
}
async function load() {
    state.controller?.abort();
    state.controller = new AbortController();
    const serial = ++state.serial;
    state.busy = true;
    el('message').textContent = '라이브러리를 읽고 있습니다…';
    updatePaging();
    try {
        const page = await request('/api/library?limit=24&offset=' + state.offset + '&q=' + encodeURIComponent(state.q), state.controller.signal);
        if (serial !== state.serial)
            return;
        state.next = page.hasNextPage;
        state.total = page.total;
        el('count').textContent = '총 ' + page.total + '개 묶음';
        el('gallery').replaceChildren();
        for (const item of page.data) {
            const button = document.createElement('button');
            button.type = 'button';
            button.className = 'card';
            button.addEventListener('click', () => openDetail(item.id));
            const cover = document.createElement('div');
            cover.className = 'cover';
            cover.append(text('span', item.cover?.kind === 'audio' ? '오디오' : item.cover?.kind === 'video' ? '영상' : '미디어'));
            if (item.cover) {
                const image = document.createElement('img');
                image.src = fileURL(item.cover.fileId, 'thumbnail');
                image.alt = '';
                image.loading = 'lazy';
                image.addEventListener('error', () => image.remove(), { once: true });
                cover.append(image);
            }
            const body = document.createElement('div');
            body.className = 'card-body';
            body.append(text('h2', item.title), text('small', item.fileCount + '개 파일'));
            button.append(cover, body);
            el('gallery').append(button);
        }
        el('message').textContent = page.data.length ? '' : state.q ? '검색 결과가 없습니다.' : '아직 등록된 미디어가 없습니다.';
    }
    catch (error) {
        if (serial === state.serial && error.name !== 'AbortError')
            el('message').textContent = error.message;
    }
    finally {
        if (serial === state.serial) {
            state.busy = false;
            updatePaging();
        }
    }
}
function clearFiles() {
    for (const player of el('files').querySelectorAll('video,audio')) {
        player.pause();
        player.removeAttribute('src');
        player.load();
    }
    el('files').replaceChildren();
}
async function openDetail(id) {
    if (!Number.isSafeInteger(id) || id <= 0)
        return;
    state.media = id;
    state.fileOffset = 0;
    if (!el('detail').open)
        el('detail').showModal();
    await loadFiles();
}
async function loadFiles() {
    const serial = ++state.detailSerial;
    clearFiles();
    el('detail-message').textContent = '미디어를 읽고 있습니다…';
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
                const player = document.createElement(kind === 'image' ? 'img' : kind);
                if (kind === 'image') {
                    player.alt = file.title || detail.title;
                    player.loading = 'lazy';
                }
                else {
                    player.controls = true;
                player.setAttribute('controlsList', 'nodownload');
                    player.preload = 'none';
                    if (kind === 'video')
                        player.playsInline = true;
                }
                player.src = fileURL(file.id, 'content');
                let fallback = false;
                player.addEventListener('error', () => {
                    if (kind === 'image' && !fallback) {
                        fallback = true;
                        player.src = fileURL(file.id, 'preview');
                        return;
                    }
                    player.replaceWith(text('p', '이 브라우저에서 표시하거나 재생할 수 없는 형식입니다.'));
                });
                figure.append(player);
            }
            else
                figure.append(text('p', '표시를 지원하지 않는 미디어입니다.'));
            figure.append(text('figcaption', file.title || '파일 ' + file.id));
            el('files').append(figure);
        }
        el('detail-message').textContent = '파일 ' + detail.files.total + '개';
        el('file-page').textContent = (Math.floor(state.fileOffset / 24) + 1) + ' 페이지';
        el('file-previous').disabled = state.fileOffset === 0;
        el('file-next').disabled = !state.fileNext;
    }
    catch (error) {
        if (serial === state.detailSerial)
            el('detail-message').textContent = error.message;
    }
}
el('search').addEventListener('submit', event => {
    event.preventDefault();
    state.q = el('keyword').value.trim();
    state.offset = 0;
    void load();
});
el('refresh').addEventListener('click', () => void load());
el('previous').addEventListener('click', () => {
    state.offset = Math.max(0, state.offset - 24);
    void load();
});
el('next').addEventListener('click', () => {
    state.offset += 24;
    void load();
});
el('close').addEventListener('click', () => el('detail').close());
el('detail').addEventListener('close', () => {
    state.detailSerial++;
    clearFiles();
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
void load();
