import { useEffect, useRef, useState } from 'react'
import { getApi } from '@renderer/shared/api'
import { translate, translateError, useLanguage } from '@renderer/shared/i18n'
import type { AlbumRequest, MediaSummaryDto } from '@contracts/media-v2'

export type ExplorerAction = { kind: 'create' } | { kind: 'rename' | 'move' | 'delete'; album: MediaSummaryDto } | { kind: 'remove'; fileIds: number[] }
const button = 'rounded-lg border border-[var(--border-line)] px-3 py-2 text-sm disabled:opacity-40'
export function AlbumExplorerDialog({ action, current, onClose, onSaved }: { action: ExplorerAction; current: MediaSummaryDto; onClose: () => void; onSaved: (album: MediaSummaryDto, deleted: boolean) => void }) {
    useLanguage()
    const dialog = useRef<HTMLDialogElement>(null), live = useRef(false), pending = useRef(false), serial = useRef(0)
    const [name, setName] = useState(action.kind === 'rename' ? action.album.title : '')
    const [busy, setBusy] = useState(false), [error, setError] = useState('')
    const [keyword, setKeyword] = useState(''), [targets, setTargets] = useState<MediaSummaryDto[]>([]), [more, setMore] = useState(false), [reading, setReading] = useState(false)
    const [target, setTarget] = useState<number | null>(null)
    const title = translate('explorer.' + action.kind)
    useEffect(() => {
        live.current = true; dialog.current?.showModal()
        return () => { live.current = false; ++serial.current }
    }, [])
    async function load(append = false) {
        const own = ++serial.current; setReading(true); setError('')
        try {
            const result = await getApi().media.list({ filter: { keyword }, limit: 50, offset: append ? targets.length : 0 })
            if (!live.current || own !== serial.current) return
            if (!result.ok) throw new Error(translateError(result.error))
            setTargets(previous => append ? [...previous, ...result.data.data] : result.data.data); setMore(result.data.hasNextPage)
        } catch (failure) { if (live.current && own === serial.current) setError(failure instanceof Error ? failure.message : String(failure)) }
        finally { if (live.current && own === serial.current) setReading(false) }
    }
    useEffect(() => { if (action.kind === 'move') void load() }, [])
    async function save() {
        if (pending.current) return
        let request: AlbumRequest
        if (action.kind === 'create') request = { title: name.trim(), parentId: current.id }
        else if (action.kind === 'remove') request = { albumId: current.id, expectedRevision: current.revision, removeFileIds: action.fileIds }
        else request = { albumId: action.album.id, expectedRevision: action.album.revision, ...(action.kind === 'rename' ? { title: name.trim() } : action.kind === 'move' ? { parentId: target! } : { delete: true }) }
        if ((action.kind === 'create' || action.kind === 'rename') && !name.trim() || action.kind === 'move' && target === null) return
        pending.current = true; setBusy(true); setError('')
        try {
            const result = await getApi().media.saveAlbum(request)
            if (!live.current) return
            if (!result.ok) throw new Error(translateError(result.error))
            onSaved(result.data, action.kind === 'delete')
        } catch (failure) { if (live.current) setError(failure instanceof Error ? failure.message : String(failure)) }
        finally { pending.current = false; if (live.current) setBusy(false) }
    }
    return <dialog ref={dialog} aria-label={title} onCancel={event => { event.preventDefault(); if (!pending.current) onClose() }} className="m-auto max-h-[85dvh] w-[min(36rem,94vw)] overflow-auto rounded-xl border border-[var(--border-line)] bg-[var(--bg-app)] p-5 text-[var(--text-main)] backdrop:bg-black/50">
        <form className="space-y-4" onSubmit={event => { event.preventDefault(); void save() }}>
            <h2 className="text-lg font-semibold">{title}</h2>
            {(action.kind === 'create' || action.kind === 'rename') && <label className="block space-y-1"><span>{translate('explorer.name')}</span><input autoFocus aria-label={translate('explorer.name')} className="w-full rounded-lg border border-[var(--border-line)] bg-[var(--bg-hover)] p-2" maxLength={1000} value={name} disabled={busy} onChange={event => setName(event.target.value)} /></label>}
            {action.kind === 'move' && <div className="space-y-3">
                <p className="break-words">{action.album.title}</p>
                <p className="text-sm text-[var(--text-muted)]">{translate('explorer.moveHelp')}</p>
                <div className="flex gap-2"><input aria-label={translate('explorer.findDestination')} className="min-w-0 flex-1 rounded-lg border border-[var(--border-line)] bg-[var(--bg-hover)] p-2" maxLength={200} value={keyword} disabled={busy || reading} onChange={event => setKeyword(event.target.value)} /><button type="button" className={button} disabled={busy || reading} onClick={() => void load()}>{translate('explorer.findDestination')}</button></div>
                <div role="group" aria-label={translate('explorer.destination')} className="max-h-64 space-y-1 overflow-auto">
                    <button type="button" className={button + ' w-full text-left'} disabled={busy} aria-pressed={target === 0} onClick={() => setTarget(0)}>{translate('navigation.gallery')}</button>
                    {targets.filter(item => item.id !== action.album.id).map(item => <button key={item.id} type="button" className={button + ' w-full truncate text-left aria-pressed:border-[var(--color-primary)]'} disabled={busy} aria-pressed={target === item.id} onClick={() => setTarget(item.id)}>{item.title}</button>)}
                </div>
                {more && <button type="button" className={button} disabled={busy || reading} onClick={() => void load(true)}>{translate('gallery.loadMore')}</button>}
                {reading && <p role="status">{translate('gallery.loading')}</p>}
            </div>}
            {action.kind === 'delete' && <><p className="break-words">{action.album.title}</p><p>{translate('explorer.deleteHelp')}</p></>}
            {action.kind === 'remove' && <p>{translate('explorer.removeHelp', { count: action.fileIds.length })}</p>}
            {error && <p role="alert" className="text-sm text-red-500">{error}</p>}
            <footer className="flex justify-end gap-2"><button type="button" className={button} disabled={busy} onClick={onClose}>{translate('common.cancel')}</button><button type="submit" className={button} disabled={busy || ((action.kind === 'create' || action.kind === 'rename') && !name.trim()) || action.kind === 'move' && target === null}>{busy ? translate('download.saving') : translate('explorer.apply')}</button></footer>
        </form>
    </dialog>
}
