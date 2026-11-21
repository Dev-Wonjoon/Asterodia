// The mobile adapter uses the same paged Go search API as the desktop combobox.
export function mountSearchSuggestions({ input, form, request, parameters, onApply, onInput }) {
    const labels = { title: '제목', tag: '태그', author: '작성자', platform: '플랫폼' };
    const wrapper = document.createElement('div');
    wrapper.className = 'search-input';
    input.before(wrapper); wrapper.append(input);
    const popup = document.createElement('div');
    popup.className = 'search-popup'; popup.hidden = true;
    const list = document.createElement('div'); list.className = 'search-options'; list.id = 'search-options'; list.setAttribute('role', 'listbox'); list.setAttribute('aria-label', '검색 추천');
    const status = document.createElement('p'); status.setAttribute('role', 'status');
    const more = document.createElement('button'); more.type = 'button'; more.textContent = '추천 더 보기';
    const retry = document.createElement('button'); retry.type = 'button'; retry.textContent = '다시 시도'; retry.hidden = true;
    popup.append(list, status, more, retry); wrapper.append(popup);
    input.setAttribute('role', 'combobox'); input.setAttribute('aria-autocomplete', 'list'); input.setAttribute('aria-controls', list.id); input.setAttribute('aria-expanded', 'false');
    let serial = 0, timer, controller, composing = false, items = [], active = -1, next = false, revision = null, busy = false, failure = false;
    const cache = new Map();
    function close() { ++serial; clearTimeout(timer); controller?.abort(); busy = false; popup.hidden = true; input.setAttribute('aria-expanded', 'false'); input.removeAttribute('aria-activedescendant'); active = -1; }
    function choose(item) { input.focus({ preventScroll: true }); close(); onApply(item); }
    function render() {
        list.replaceChildren();
        items.forEach((item, index) => {
            const button = document.createElement('button'); button.type = 'button'; button.id = list.id + '-' + index; button.tabIndex = -1; button.setAttribute('role', 'option'); button.setAttribute('aria-selected', String(index === active));
            const kind = document.createElement('small'); kind.textContent = labels[item.kind];
            const value = document.createElement('span'); value.textContent = item.value;
            button.append(kind, value); button.addEventListener('mousedown', e => e.preventDefault()); button.addEventListener('click', () => choose(item)); list.append(button);
        });
        status.textContent = failure ? '추천 목록을 불러오지 못했습니다.' : busy ? '추천을 찾고 있습니다…' : items.length ? '' : '일치하는 후보가 없습니다.';
        more.hidden = !next || busy || failure; retry.hidden = !failure;
        list.setAttribute('aria-busy', String(busy));
        if (active >= 0 && items[active]) input.setAttribute('aria-activedescendant', list.id + '-' + active);
        else input.removeAttribute('aria-activedescendant');
    }
    function queryKey() { return JSON.stringify([input.value, parameters().toString()]); }
    async function read(offset = 0, own = serial) {
        if (busy || composing || popup.hidden || !input.value.trim()) return;
        busy = true; failure = false; render();
        controller?.abort(); controller = new AbortController();
        const key = queryKey(), query = parameters(); query.set('q', input.value); query.set('offset', String(offset)); query.set('limit', '10');
        try {
            const page = await request('/api/search-suggestions?' + query, controller.signal);
            if (own !== serial || key !== queryKey()) return;
            if (offset && revision !== page.revision) { busy = false; await read(0, own); return; }
            items = offset ? items.concat(page.data) : page.data;
            next = page.hasNextPage; revision = page.revision;
            if (!offset) { cache.delete(key); cache.set(key, { page, at: Date.now() }); if (cache.size > 32) cache.delete(cache.keys().next().value); }
        } catch (error) { if (own === serial && error.name !== 'AbortError') failure = true; }
        finally { if (own === serial) { busy = false; render(); } }
    }
    function suggest(delay = 50) {
        close(); items = []; next = false; revision = null; failure = false;
        if (composing || !input.value.trim()) return;
        const saved = cache.get(queryKey());
        if (saved && Date.now() - saved.at < 30000) { items = saved.page.data; next = saved.page.hasNextPage; revision = saved.page.revision; }
        popup.hidden = false; input.setAttribute('aria-expanded', 'true'); render();
        const own = serial; timer = setTimeout(() => { void read(0, own); }, delay);
    }
    input.addEventListener('input', () => { suggest(); onInput(input.value, composing); });
    input.addEventListener('focus', () => suggest());
    input.addEventListener('compositionstart', () => { composing = true; close(); onInput(input.value, true); });
    input.addEventListener('compositionend', () => { composing = false; suggest(0); onInput(input.value, false); });
    input.addEventListener('keydown', event => {
        if (event.isComposing || event.keyCode === 229 || composing) return;
        if (event.key === 'Escape') { event.preventDefault(); close(); }
        else if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
            event.preventDefault(); if (popup.hidden) { suggest(); return; }
            if (items.length) { active = event.key === 'ArrowDown' ? Math.min(active + 1, items.length - 1) : Math.max(0, active - 1); render(); list.children[active]?.scrollIntoView?.({ block: 'nearest' }); if (active === items.length - 1 && next) void read(items.length); }
        } else if (event.key === 'Enter' && !popup.hidden && active >= 0 && items[active]) { event.preventDefault(); choose(items[active]); }
    });
    wrapper.addEventListener('focusout', event => { if (!wrapper.contains(event.relatedTarget)) close(); });
    form.addEventListener('submit', event => { event.preventDefault(); if (composing) return; close(); onApply({ kind: 'title', value: input.value }); });
    list.addEventListener('scroll', () => { if (next && !failure && list.scrollHeight - list.scrollTop - list.clientHeight < 48) void read(items.length); });
    more.addEventListener('mousedown', event => event.preventDefault()); retry.addEventListener('mousedown', event => event.preventDefault());
    more.addEventListener('click', () => { void read(items.length); }); retry.addEventListener('click', () => { void read(0); });
    return { close };
}
