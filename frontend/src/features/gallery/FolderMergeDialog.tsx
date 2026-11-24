import { translateError, translateMessage, translate, useLanguage } from '@renderer/shared/i18n'
import { useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import type { MoveSelection } from '@contracts/file-move-api'
import type { MediaDirectoryDto } from '@contracts/media-v2'
import { getApi } from '@renderer/shared/api'

type Mode = 'album' | 'files' | 'combined'
export function FolderMergeDialog({ selection, onClose, onChanged, onStarted }: {
    selection: MoveSelection[]; onClose(): void; onChanged(): void; onStarted(jobId: string, error?: string): void
}) {
    useLanguage()
    const dialog = useRef<HTMLDialogElement>(null)
    const alive = useRef(true), submitting = useRef(false), read = useRef(0)
    const [mode, setMode] = useState<Mode>('album')
    const [name, setName] = useState('')
    const [operation, setOperation] = useState<'copy' | 'move'>('copy')
    const [folders, setFolders] = useState<MediaDirectoryDto[]>([])
    const [directory, setDirectory] = useState(''), [keyword, setKeyword] = useState('')
    const [more, setMore] = useState(false), [loading, setLoading] = useState(false)
    const [busy, setBusy] = useState(false), [error, setError] = useState(''), [uncertain, setUncertain] = useState(false)
    const api = getApi()
    const input = 'w-full rounded-lg border border-[var(--border-line)] bg-[var(--bg-sidebar)] p-2'
    const button = 'rounded-lg border border-[var(--border-line)] px-3 py-2 text-sm disabled:opacity-40'
    useEffect(() => {
        alive.current = true; dialog.current?.showModal()
        return () => { alive.current = false; read.current++; dialog.current?.close() }
    }, [])
    async function load(append = false) {
        const own = ++read.current; setLoading(true); setError('')
        if (!append) { setDirectory(''); setFolders([]) }
        try {
            const result = await api.media.directories({ keyword, limit: 100, offset: append ? folders.length : 0 })
            if (!alive.current || own !== read.current) return
            if (!result.ok) { setError(translateError(result.error)); return }
            setFolders(previous => append ? [...previous, ...result.data.data] : result.data.data); setMore(result.data.hasNextPage)
        } catch { if (alive.current && own === read.current) setError(translate("gallery.couldNotLoadDestinationFolders")) }
        finally { if (alive.current && own === read.current) setLoading(false) }
    }
    useEffect(() => {
        if (mode !== 'album') void load()
        else { read.current++; setLoading(false); setError('') }
    }, [mode])
    async function start() {
        if (submitting.current || uncertain || !selection.length || (mode !== 'files' && !name.trim()) || (mode !== 'album' && !directory)) return
        submitting.current = true; setBusy(true); setError('')
        let job = ''
        try {
            if (mode === 'album') {
                const result = await api.media.mergeAlbum({ title: name.trim(), selection })
                if (!alive.current) return
                if (!result.ok) { setError(translateError(result.error)); return }
                onChanged(); onClose(); return
            }
            const created = await api.fileMove.create({ selection, directoryId: Number(directory), operation, ...(mode === 'combined' ? { mergeTitle: name.trim() } : {}) })
            if (!alive.current) return
            if (!created.ok) { setError(translateError(created.error)); return }
            job = created.data.jobId
            let message = ''
            try {
                const result = await api.fileMove.run({ jobId: job })
                if (!result.ok) message = translateError(result.error)
            } catch { message = translate("gallery.couldNotConfirmTheStartResultRefreshStatusAnd") }
            if (!alive.current) return
            onChanged(); onStarted(job, message); onClose()
        } catch {
            if (alive.current) {
                setUncertain(true)
                setError(translate("gallery.couldNotConfirmTheResultRefreshTheGalleryAnd"))
            }
        } finally { submitting.current = false; if (alive.current) setBusy(false) }
    }
    return createPortal(<dialog ref={dialog} data-detail-trigger aria-labelledby="folder-merge-title" className="m-auto max-h-[85dvh] w-[min(92vw,640px)] space-y-4 overflow-auto rounded-2xl border border-[var(--border-line)] bg-[var(--bg-app)] p-5 text-[var(--text-main)] shadow-2xl backdrop:bg-black/60" onKeyDown={event => event.stopPropagation()} onCancel={event => { event.preventDefault(); if (!busy) onClose() }}>
        <header className="flex items-center justify-between gap-3"><h2 id="folder-merge-title" className="font-semibold">{translate("gallery.mergeFoldersAndAlbums")}</h2><button type="button" className={button} disabled={busy} onClick={onClose}>{translate("common.close")}</button></header>
        <fieldset disabled={busy} className="space-y-2"><legend className="mb-2 text-sm">{translate("gallery.mergeMethod")}</legend>
            {([['album', translate("gallery.mergeAlbumsOnly")], ['files', translate("gallery.collectFilesOnly")], ['combined', translate("gallery.mergeIntoAnAlbumAndCollectFiles")]] as const).map(([value, label]) => <label key={value} className="flex gap-2 text-sm"><input type="radio" name="folder-merge-mode" value={value} checked={mode === value} onChange={() => setMode(value)} />{label}</label>)}
        </fieldset>
        {mode !== 'files' && <label className="block space-y-1 text-sm">{translate("gallery.newAlbumName")}<input aria-label={translate("gallery.newAlbumName")} className={input} value={name} maxLength={1000} disabled={busy} onChange={event => setName(event.target.value)} /></label>}
        <p className="text-sm text-[var(--text-muted)]">{translate("gallery.originalAlbumsAreKeptEachFileIsLinkedOnly")}</p>
        {mode !== 'album' && <div className="space-y-3">
            <label className="block text-sm">{translate("gallery.fileCollectionMethod")}<select aria-label={translate("gallery.mergedFileHandling")} className={input} disabled={busy} value={operation} onChange={event => setOperation(event.target.value as 'copy' | 'move')}><option value="copy">{translate("gallery.copyKeepOriginals")}</option><option value="move">{translate("gallery.moveChangeFileLocations")}</option></select></label>
            <p className="text-sm text-[var(--text-muted)]">{translate("gallery.filesWithTheSameNameArePreservedWithNumbered", { operationHint: operation === 'copy' ? translate("gallery.copyFilesToTheDestinationFolderAlbumLinksKeep") : translate("gallery.afterVerificationCleanUpOriginalsBothExistingAndNew") })}</p>
            <form className="flex gap-2" onSubmit={event => { event.preventDefault(); if (!busy && !loading) void load() }}><input aria-label={translate("gallery.searchMergeDestinationFolders")} className={input} value={keyword} disabled={busy || loading} onChange={event => setKeyword(event.target.value)} /><button className={button} disabled={busy || loading}>{translate("navigation.search")}</button></form>
            <label className="block text-sm">{translate("gallery.destination.label")}<select aria-label={translate("gallery.mergeDestinationFolder")} className={input} value={directory} disabled={busy || loading} onChange={event => setDirectory(event.target.value)}><option value="">{translate("gallery.selectFolder")}</option>{folders.map(folder => <option key={folder.id} value={folder.id}>{folder.directoryPath}</option>)}</select></label>
            {loading && <p role="status" className="text-sm">{translate("gallery.loadingFolders")}</p>}
            {!loading && !folders.length && <p className="text-sm">{translate("gallery.noRegisteredFoldersImportTheDestinationAsALocal")}</p>}
            {more && <button type="button" className={button} disabled={busy || loading} onClick={() => void load(true)}>{translate("gallery.loadMoreFolders")}</button>}
        </div>}
        {error && <p role="alert" className="text-sm text-red-500">{translateMessage(error)}</p>}
        <footer className="flex justify-end gap-2"><button type="button" className={button} disabled={busy} onClick={onClose}>{translate("common.cancel")}</button><button type="button" className={button} disabled={busy || loading || uncertain || !selection.length || (mode !== 'files' && !name.trim()) || (mode !== 'album' && !directory)} onClick={() => void start()}>{busy ? translate("gallery.requesting") : translate("gallery.startMerge")}</button></footer>
    </dialog>, document.body)
}
