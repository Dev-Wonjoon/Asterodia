import { translate, translateError, bindText, bindAttribute, localizeWidget } from './i18n.js';

// Physical output selection and logical album/tag classification are independent.
export function mountOutputControls(host, { api, chooseDirectory, onChange = () => {} }) {
    if (typeof api.outputOptions !== 'function') return { request: () => ({}), ready: () => true, setDisabled() {}, destroy() {} };
    const doc = host.ownerDocument;
    const root = doc.createElement('section'); root.className = 'sbe-output'; host.append(root);
    const el = (tag, parent, text) => { const node = doc.createElement(tag); parent?.append(node); if (typeof text === 'function') bindText(node, text); return node; };
    const button = (parent, key, action) => { const node = el('button', parent, () => translate(key)); node.type = 'button'; node.onclick = action; return node; };
    el('h3', root, () => translate('editor.output.title'));
    el('p', root, () => translate('editor.output.help')).className = 'sbe-hint';
    const path = el('p', root); path.className = 'sbe-output-path'; bindAttribute(path, 'aria-label', () => translate('editor.output.directory'));
    let dead = false, loaded = false, blocked = false, reading = false, choosing = false, generation = 0, timer;
    let directory = '', override = '', albums = [], next = false, offset = 0;
    const selected = new Map(), tags = new Set();
    const directoryActions = el('div', root); directoryActions.className = 'sbe-fields';
    directoryActions.hidden = typeof chooseDirectory !== 'function';
    const choose = button(directoryActions, 'editor.output.chooseDirectory', async () => {
        if (choosing || blocked) return; choosing = true; updateDisabled(); onChange(false);
        try { const value = await chooseDirectory(); if (!dead && value) { override = value; updatePath(); onChange(true); } }
        catch { if (!dead) showError('editor.output.directoryError'); }
        finally { choosing = false; if (!dead) { updateDisabled(); onChange(false); } }
    });
    const reset = button(directoryActions, 'editor.output.useDefault', () => { override = ''; updatePath(); onChange(true); updateDisabled(); });
    const albumField = el('fieldset', root); el('legend', albumField, () => translate('editor.output.albums'));
    const searchRow = el('div', albumField); searchRow.className = 'sbe-fields';
    const search = el('input', searchRow); search.type = 'search'; bindAttribute(search, 'aria-label', () => translate('editor.output.searchAlbums'));
    search.oninput = () => { clearTimeout(timer); timer = setTimeout(() => void load(false), 250); };
    search.onkeydown = event => { if (event.key === 'Enter' && !event.isComposing && event.keyCode !== 229) { event.preventDefault(); clearTimeout(timer); void load(false); } };
    const selectedList = el('div', albumField), albumList = el('div', albumField); albumList.className = 'sbe-output-albums';
    const more = button(albumField, 'gallery.loadMoreAlbums', () => void load(true));
    const tagField = el('fieldset', root); el('legend', tagField, () => translate('editor.output.tags'));
    const tagList = el('div', tagField); tagList.className = 'sbe-fields';
    const tagRow = el('div', tagField); tagRow.className = 'sbe-fields';
    const tagInput = el('input', tagRow); tagInput.maxLength = 1000; bindAttribute(tagInput, 'aria-label', () => translate('editor.output.addTags'));
    const suggestions = el('datalist', tagField); suggestions.id = 'output-tags-' + crypto.randomUUID(); tagInput.setAttribute('list', suggestions.id);
    const addTag = button(tagRow, 'editor.output.addTag', addTags);
    el('p', tagField, () => translate('editor.output.tagsHelp')).className = 'sbe-hint';
    tagInput.oninput = () => onChange(true);
    tagInput.onkeydown = event => { if (event.key === 'Enter' && !event.isComposing && event.keyCode !== 229) { event.preventDefault(); addTags(); } };
    const error = el('p', root); error.setAttribute('role', 'alert'); error.hidden = true;
    const retry = button(root, 'common.retry', () => void load(false)); retry.hidden = true;
    function showError(key) { bindText(error, () => translate(key)); error.hidden = false; }
    function updatePath() { bindText(path, () => override || directory || translate('editor.output.pcDirectory')); }
    function updateDisabled() {
        for (const control of root.querySelectorAll('input,button')) control.disabled = blocked;
        choose.disabled = blocked || choosing; reset.disabled = blocked || !override || choosing;
        more.disabled = blocked || reading; search.disabled = blocked; retry.disabled = blocked || reading;
    }
    function tagValues() { return [...new Set([...tags, ...tagInput.value.split(/[,;\n]/).map(value => value.trim()).filter(Boolean)])].sort(); }
    function addTags() {
        const values = tagValues();
        if (values.length > 100 || values.some(value => [...value].length > 100)) { showError('editor.output.invalidTags'); return; }
        for (const value of values) tags.add(value); tagInput.value = ''; renderTags(); onChange(true);
    }
    function renderTags() {
        tagList.replaceChildren();
        for (const name of tags) {
            const remove = el('button', tagList); remove.type = 'button'; remove.textContent = name + ' ×';
            bindAttribute(remove, 'aria-label', () => translate('editor.output.removeTag', { tag: name }));
            remove.onclick = () => { tags.delete(name); renderTags(); onChange(true); };
        }
        updateDisabled();
    }
    function renderAlbums(focusId) {
        const scroll = albumList.scrollTop;
        selectedList.replaceChildren();
        for (const [id, title] of selected) {
            const remove = el('button', selectedList); remove.type = 'button'; remove.textContent = title + ' ×';
            bindAttribute(remove, 'aria-label', () => translate('editor.output.removeAlbum', { title }));
            remove.onclick = () => { selected.delete(id); renderAlbums(id); onChange(true); };
        }
        albumList.replaceChildren();
        for (const item of albums) {
            const label = el('label', albumList), input = el('input', label); input.type = 'checkbox'; input.checked = selected.has(item.id); input.dataset.albumId = String(item.id);
            const title = el('span', label); title.textContent = item.title;
            input.onchange = () => { if (input.checked) selected.set(item.id, item.title); else selected.delete(item.id); renderAlbums(item.id); onChange(true); };
        }
        if (!albums.length) el('p', albumList, () => translate('editor.output.noAlbums'));
        more.hidden = !next; updateDisabled();
        albumList.scrollTop = scroll;
        if (focusId) (albumList.querySelector('input[data-album-id="' + focusId + '"]') || search).focus({ preventScroll: true });
    }
    async function load(append) {
        const own = ++generation; reading = true; retry.hidden = true; error.hidden = true; updateDisabled();
        try {
            const result = await api.outputOptions({ keyword: search.value, offset: append ? offset : 0 });
            if (dead || own !== generation) return;
            if (!result.ok) throw Error(translateError(result.error));
            const value = result.data;
            directory = value.directory || ''; albums = append ? albums.concat(value.albums) : value.albums;
            offset = value.offset + value.albums.length; next = value.hasNextPage; loaded = true;
            suggestions.replaceChildren(); for (const tag of value.tags) { const option = el('option', suggestions); option.value = tag; }
            updatePath(); renderAlbums(); onChange(false);
        } catch {
            if (!dead && own === generation) { showError('editor.output.loadError'); retry.hidden = false; }
        } finally { if (!dead && own === generation) { reading = false; updateDisabled(); onChange(false); } }
    }
    const stopLanguage = localizeWidget(root);
    updatePath(); void load(false);
    return {
        request(preview = false) {
            if (preview) return {};
            const tagNames = tagValues();
            if (tagNames.length > 100 || tagNames.some(value => [...value].length > 100)) throw Error(translate('editor.output.invalidTags'));
            if (selected.size > 100) throw Error(translate('editor.output.invalidAlbums'));
            const value = {};
            if (typeof chooseDirectory === 'function' && (override || directory)) value.outputDirectory = override || directory;
            if (selected.size) value.albumIds = [...selected.keys()].sort((a,b) => a-b);
            if (tagNames.length) value.tagNames = tagNames;
            return value;
        },
        ready: () => loaded && !choosing,
        setDisabled(value) { blocked = value; updateDisabled(); },
        destroy() { dead = true; generation++; clearTimeout(timer); stopLanguage(); root.remove(); },
    };
}
