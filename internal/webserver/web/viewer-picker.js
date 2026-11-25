import { translate, bindText, bindAttribute, localizeWidget } from './i18n.js';

export function mountViewerPicker(parent, { listMedia, getAlbum, mediaId, onSelect, onClose = () => {} }) {
    const doc = parent.ownerDocument, previousFocus = doc.activeElement;
    const dialog = doc.createElement('dialog'); dialog.className = 'sbv-picker';
    bindAttribute(dialog, 'aria-label', () => translate('viewer.chooseMedia'));
    const node = (tag, cls, text) => { const el = doc.createElement(tag); if (cls) el.className = cls; if (text) bindText(el, text); return el; };
    const header = node('header'), title = node('h2', '', () => translate('viewer.chooseMedia'));
    const button = (label, action, host) => { const el = node('button', '', label); el.type = 'button'; el.onclick = action; host.append(el); return el; };
    header.append(title); button(() => translate('viewer.closeEsc'), () => dialog.close(), header);
    const form = node('form'), search = node('input'); search.type = 'search'; search.maxLength = 2000;
    bindAttribute(search, 'aria-label', () => translate('search.media'));
    form.append(search); const submit = node('button', '', () => translate('navigation.search')); submit.type = 'submit'; form.append(submit);
    const navigation = node('nav'), collection = node('p', 'sbv-picker-collection'), rows = node('div', 'sbv-picker-rows'), status = node('p'); status.setAttribute('role', 'status');
    const library = button(() => translate('gallery.backToList'), () => { albumId = null; void load(); }, navigation);
    const current = button(() => translate('viewer.currentAlbum'), () => { albumId = mediaId; void load(); }, navigation); current.hidden = !mediaId;
    const more = button(() => translate('gallery.loadMore'), () => void load(true), dialog);
    dialog.prepend(header, form, navigation, collection, status, rows); parent.append(dialog);
    let albumId = mediaId || null, keyword = '', offset = 0, serial = 0, destroyed = false, loading = false, appendRetry = false, revision;
    async function load(append = false) {
        if (destroyed || append && loading) return;
        const own = ++serial, start = append ? offset : 0, sourceId = albumId;
        loading = true; appendRetry = append; more.disabled = true; library.hidden = !sourceId;
        bindText(status, () => translate('gallery.loading'));
        if (!append) { offset = 0; revision = undefined; rows.replaceChildren(); rows.scrollTop = 0; }
        try {
            const response = sourceId ? await getAlbum(sourceId, start) : await listMedia({ keyword, offset: start });
            if (destroyed || own !== serial) return;
            const page = sourceId ? response.files : response;
            if (sourceId && append && revision !== response.revision) throw new Error(translate('gallery.theListHasChangedGoBackToTheList'));
            revision = response.revision;
            if (!page.data.length && page.hasNextPage) throw new Error(translate('gallery.couldNotLoadFilesTryAgain'));
            offset = (page.offset ?? start) + page.data.length;
            bindText(collection, () => sourceId ? response.title : translate('navigation.gallery'));
            for (const item of page.data) {
                if (sourceId) {
                    if (item.kind !== 'image' && item.kind !== 'video') continue;
                    const row = button(() => item.title || item.relativePath || item.fileKey || translate('gallery.file', { fileId: item.id }), () => {
                        if (destroyed) return;
                        destroy(false); onSelect({ mediaId: sourceId, fileId: item.id });
                    }, rows);
                    row.dataset.fileId = String(item.id);
                    row.dataset.kind = item.kind;
                } else if (item.fileCount > 0) {
                    button(() => item.title, () => { albumId = item.id; void load(); }, rows);
                }
            }
            bindText(status, () => rows.children.length ? '' : translate('viewer.noVisualMedia'));
            more.hidden = !page.hasNextPage; more.onclick = () => void load(true);
            bindText(more, () => translate('gallery.loadMore'));
        } catch (error) {
            if (destroyed || own !== serial) return;
            bindText(status, () => error.message || translate('gallery.couldNotLoadMedia'));
            more.hidden = false; bindText(more, () => translate('common.retry')); more.onclick = () => void load(appendRetry);
        } finally { if (!destroyed && own === serial) { loading = false; more.disabled = false; } }
    }
    form.onsubmit = event => { event.preventDefault(); keyword = search.value.trim(); albumId = null; void load(); };
    const stopLanguage = localizeWidget(dialog);
    function destroy(restore = true) {
        if (destroyed) return; destroyed = true; serial++; stopLanguage(); dialog.removeEventListener('close', closed); dialog.remove();
        if (restore && previousFocus?.isConnected) previousFocus.focus({ preventScroll: true });
        onClose();
    }
    function closed() { destroy(); }
    dialog.addEventListener('close', closed); dialog.showModal(); search.focus(); void load();
    return { dialog, destroy };
}
