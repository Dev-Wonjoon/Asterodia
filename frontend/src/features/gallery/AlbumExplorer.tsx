import { BatchConvertPanel } from '../mediaedit/BatchConvertPanel'
import { getApi } from '@renderer/shared/api'
import type { MoveSelection } from '@contracts/file-move-api'
import { useCallback, useEffect, useId, useLayoutEffect, useRef, useState, type KeyboardEvent, type MouseEvent, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { ArrowLeft, ArrowRight, ArrowUp, ChevronDown, Folder, Menu, RefreshCw } from 'lucide-react'
import { translate, getLocale, useLanguage } from '@renderer/shared/i18n'
import { MediaPreview } from '@renderer/shared/components/media/MediaPreview'
import { ContentFilterNotice } from '@renderer/shared/components/ui/ContentFilterNotice'
import { useContentVisibility } from '@renderer/shared/hooks/useContentVisibility'
import { formatBytes } from '@renderer/shared/utils/formatBytes'
import { SearchControls } from '../search/SearchPage'
import { useSearchViewModel } from '../search/useSearchViewModel'
import { MediaViewer } from './MediaViewer'
import { AlbumExplorerDialog, type ExplorerAction } from './AlbumExplorerDialog'
import { entryKey, entryTitle, useAlbumExplorer, useExplorerPreferences, type ExplorerSnapshot, type ExplorerColumn, type ExplorerMode } from './useAlbumExplorer'
import type { AlbumEntry, AlbumExplorerSort } from '@contracts/media-v2'

const button = 'rounded-lg border border-[var(--border-line)] px-3 py-2 text-sm disabled:opacity-40'
const columns: ExplorerColumn[] = ['kind', 'size', 'date', 'duration']
const duration = (seconds: number | null) => seconds == null ? '—' : `${Math.floor(seconds / 60)}:${String(Math.floor(seconds % 60)).padStart(2, '0')}`
const date = (time: number | null) => time == null ? '—' : new Date(time).toLocaleDateString(getLocale())

function ExplorerMenu({ disabled, children }: { disabled: boolean; children: (run: (action: () => void) => void) => ReactNode }) {
    const [open, setOpen] = useState(false)
    const [position, setPosition] = useState({ top: 0, left: 0, width: 360, maxHeight: 480 })
    const trigger = useRef<HTMLButtonElement>(null), panel = useRef<HTMLDivElement>(null)
    const id = useId()
    const close = (restoreFocus = false) => { setOpen(false); if (restoreFocus) trigger.current?.focus({ preventScroll: true }) }
    function place() {
        const rect = trigger.current?.getBoundingClientRect()
        if (!rect) return
        const width = Math.min(360, window.innerWidth - 16), top = Math.min(rect.bottom + 6, Math.max(8, window.innerHeight - 104))
        setPosition({ top, left: Math.max(8, Math.min(rect.right - width, window.innerWidth - width - 8)), width, maxHeight: Math.max(96, window.innerHeight - top - 8) })
    }
    function show() { if (!disabled) { place(); setOpen(true) } }
    useLayoutEffect(() => {
        if (!open) return
        panel.current?.querySelector<HTMLElement>('button:not(:disabled),input:not(:disabled),select:not(:disabled)')?.focus({ preventScroll: true })
        const outside = (event: PointerEvent) => { if (event.target instanceof Node && !panel.current?.contains(event.target) && !trigger.current?.contains(event.target)) close() }
        const escape = (event: globalThis.KeyboardEvent) => { if (event.key === 'Escape' && !event.isComposing) { event.preventDefault(); event.stopPropagation(); close(true) } }
        document.addEventListener('pointerdown', outside); document.addEventListener('keydown', escape); window.addEventListener('resize', place)
        return () => { document.removeEventListener('pointerdown', outside); document.removeEventListener('keydown', escape); window.removeEventListener('resize', place) }
    }, [open])
    useEffect(() => { if (disabled) setOpen(false) }, [disabled])
    return <>
        <button ref={trigger} type="button" className={button + ' flex shrink-0 items-center gap-2'} disabled={disabled} aria-label={translate('explorer.menu')} aria-haspopup="dialog" aria-expanded={open} aria-controls={open ? id : undefined}
            onClick={() => open ? close() : show()} onKeyDown={event => { if (event.key === 'ArrowDown') { event.preventDefault(); event.stopPropagation(); show() } }}>
            <Menu className="size-4 sm:hidden" aria-hidden="true" /><span className="hidden sm:inline">{translate('explorer.menu')}</span><ChevronDown className="size-4" aria-hidden="true" />
        </button>
        {open && createPortal(<div ref={panel} id={id} role="dialog" aria-label={translate('explorer.menu')} style={position} className="fixed z-50 overflow-y-auto overscroll-contain rounded-xl border border-[var(--border-line)] bg-[var(--bg-app)] p-3 text-[var(--text-main)] shadow-xl"
            onBlur={event => { const next = event.relatedTarget; if (next instanceof Node && !event.currentTarget.contains(next) && !trigger.current?.contains(next)) close() }}
            onKeyDown={event => { event.stopPropagation(); if (event.key === 'Escape' && !event.nativeEvent.isComposing) { event.preventDefault(); close(true) } }}>
            {children(action => { close(true); action() })}
        </div>, document.body)}
    </>
}

export function AlbumExplorer({ mediaId, onClose, onChanged, allowReveal = true, allowConversion = true }: { mediaId: number; onClose: () => void; onChanged: () => void; allowReveal?: boolean; allowConversion?: boolean }) {
    const visibility = useContentVisibility()
    const [history, setHistory] = useState([{ id: mediaId, key: 0 }]), [index, setIndex] = useState(0)
    const sequence = useRef(0), snapshots = useRef(new Map<number, ExplorerSnapshot>())
    const visit = history[index]
    const navigate = (id: number) => { if (id === visit.id) return; setHistory(previous => [...previous.slice(0, index + 1), { id, key: ++sequence.current }]); setIndex(index + 1) }
    const back = () => { if (index) setIndex(index - 1); else onClose() }
    return <AlbumExplorerView key={visit.key} visibility={visibility} mediaId={visit.id} initial={snapshots.current.get(visit.key)} remember={snapshot => snapshots.current.set(visit.key, snapshot)}
        allowReveal={allowReveal} allowConversion={allowConversion} onNavigate={navigate} onBack={back} canForward={index < history.length - 1} onForward={() => setIndex(index + 1)} onClose={onClose} onChanged={onChanged} />
}

function AlbumExplorerView({ visibility, mediaId, initial, remember, onNavigate, onBack, canForward, onForward, onClose, onChanged, allowReveal, allowConversion }: {
    allowReveal: boolean; allowConversion: boolean
    visibility: ReturnType<typeof useContentVisibility>
    mediaId: number; initial?: ExplorerSnapshot; remember: (snapshot: ExplorerSnapshot) => void
    onNavigate: (id: number) => void; onBack: () => void; canForward: boolean; onForward: () => void; onClose: () => void; onChanged: () => void
}) {
    useLanguage()
    const search = useSearchViewModel(initial?.filter)
    const [preferences, updatePreferences] = useExplorerPreferences(mediaId)
    const [recursive, setRecursive] = useState(initial?.recursive ?? false)
    const reader = useAlbumExplorer({ mediaId, filter: { ...search.filter, showHidden: visibility.showHidden }, recursive, sort: preferences.sort, descending: preferences.descending }, visibility.key, initial?.count)
    const { page } = reader
    const [selected, setSelected] = useState(new Set(initial?.selected ?? [])), [viewer, setViewer] = useState<{ mediaId: number; fileId: number } | null>(null)
    const [conversion, setConversion] = useState<MoveSelection[] | null>(null)
    const [conversionLoading, setConversionLoading] = useState(false)
    const [conversionError, setConversionError] = useState('')
    const [action, setAction] = useState<ExplorerAction | null>(null)
    const root = useRef<HTMLDivElement>(null), list = useRef<HTMLDivElement>(null), anchor = useRef<string | null>(null), restore = useRef(initial), previousKey = useRef(reader.key), focused = useRef(initial?.focused)
    const snapshot = useRef<ExplorerSnapshot>({ scrollTop: initial?.scrollTop ?? 0, selected: [], count: 0, filter: search.filter, recursive })
    snapshot.current = { scrollTop: root.current?.scrollTop ?? initial?.scrollTop ?? 0, selected: [...selected], count: page?.data.length ?? initial?.count ?? 0, filter: search.filter, recursive, focused: focused.current }
    useEffect(() => () => remember({ ...snapshot.current, focused: focused.current }), [])
    useEffect(() => {
        if (previousKey.current === reader.key) return
        previousKey.current = reader.key
        restore.current = undefined; setSelected(new Set()); setViewer(null); anchor.current = null
        if (root.current) root.current.scrollTop = 0
    }, [reader.key])
    useLayoutEffect(() => {
        if (!page || !restore.current) return
        const value = restore.current; restore.current = undefined
        if (root.current) root.current.scrollTop = value.scrollTop
        const node = [...list.current?.querySelectorAll<HTMLElement>('[data-explorer-key]') ?? []].find(node => node.dataset.explorerKey === value.focused)
        node?.focus({ preventScroll: true })
    }, [page])
    useEffect(() => { if (page) setSelected(previous => new Set([...previous].filter(key => page.data.some(entry => entryKey(entry) === key)))) }, [page])
    const closeViewer = useCallback(() => setViewer(null), [])
    const entries = page?.data ?? [], targets = entries.filter(entry => selected.has(entryKey(entry)))
    const editable = targets.length === 1 && targets[0].kind === 'album' && targets[0].album.isUserAlbum ? targets[0].album : targets.length === 0 && page?.album.isUserAlbum ? page.album : null
    const removableFiles = page?.album.isUserAlbum && targets.length > 0 && targets.every(entry => entry.kind === 'file' && entry.albumId === mediaId)
    const blocked = reader.loading || reader.loadingMore || !!action || !!viewer || !!conversion || conversionLoading
    async function openConverter() {
        if (blocked) return
        setConversionLoading(true); setConversionError('')
        try {
            const grouped = new Map<number, MoveSelection>()
            for (const entry of targets) {
                if (entry.kind === 'album') { grouped.set(entry.album.id, { mediaId: entry.album.id, revision: entry.album.revision }); continue }
                const owner = entry.file.mediaId
                const existing = grouped.get(owner)
                if (existing) { if (existing.fileIds) existing.fileIds.push(entry.file.id); continue }
                const detail = await getApi().media.get({ mediaId: owner, fileLimit: 1, fileOffset: 0 })
                if (!detail.ok) throw new Error(translate('conversion.requestFailed'))
                grouped.set(owner, { mediaId: owner, revision: detail.data.revision, fileIds: [entry.file.id] })
            }
            setConversion([...grouped.values()])
        } catch { setConversionError(translate('conversion.requestFailed')) }
        finally { setConversionLoading(false) }
    }
    function select(entry: AlbumEntry, event: Pick<MouseEvent, 'ctrlKey' | 'metaKey' | 'shiftKey'>) {
        const key = entryKey(entry)
        setSelected(previous => {
            if (event.shiftKey && anchor.current) {
                const start = entries.findIndex(item => entryKey(item) === anchor.current), end = entries.indexOf(entry)
                if (start >= 0) return new Set([...(event.ctrlKey || event.metaKey ? previous : []), ...entries.slice(Math.min(start, end), Math.max(start, end) + 1).map(entryKey)])
            }
            anchor.current = key
            if (!event.ctrlKey && !event.metaKey) return new Set([key])
            const next = new Set(previous); if (next.has(key)) next.delete(key); else next.add(key); return next
        })
    }
    function open(entry: AlbumEntry) {
        if (blocked) return
        if (entry.kind === 'album') onNavigate(entry.album.id)
        else setViewer({ mediaId: entry.albumId, fileId: entry.file.id })
    }
    function remove() {
        if (removableFiles) setAction({ kind: 'remove', fileIds: targets.flatMap(entry => entry.kind === 'file' ? [entry.file.id] : []) })
        else if (editable) setAction({ kind: 'delete', album: editable })
    }
    function keydown(event: KeyboardEvent) {
        if (event.defaultPrevented || event.nativeEvent.isComposing || blocked || (event.target as Element).closest('dialog')) return
        if (event.altKey && ['ArrowLeft', 'ArrowRight', 'ArrowUp'].includes(event.key)) {
            event.preventDefault(); event.stopPropagation()
            if (event.key === 'ArrowLeft') onBack()
            else if (event.key === 'ArrowRight' && canForward) onForward()
            else if (event.key === 'ArrowUp') { const parent = page?.breadcrumbs[page.breadcrumbs.length - 2]; if (parent) onNavigate(parent.id); else onClose() }
            return
        }
        if ((event.target as Element).closest('input,textarea,select,[contenteditable=true]')) return
        if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'a') { event.preventDefault(); event.stopPropagation(); setSelected(new Set(entries.map(entryKey))); return }
        if (event.key === 'Delete' && (editable || removableFiles)) { event.preventDefault(); event.stopPropagation(); remove(); return }
        const nodes = [...list.current?.querySelectorAll<HTMLElement>('[data-explorer-key]') ?? []]
        const current = nodes.findIndex(node => node === document.activeElement || node.contains(document.activeElement))
        if (event.key === 'Enter' && current >= 0) { event.preventDefault(); event.stopPropagation(); open(entries[current]); return }
        if (event.key === ' ' && current >= 0) { event.preventDefault(); select(entries[current], { ctrlKey: true, metaKey: false, shiftKey: event.shiftKey }); return }
        if (!['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown', 'Home', 'End'].includes(event.key) || !nodes.length) return
        event.preventDefault(); event.stopPropagation()
        let next = Math.max(0, current)
        if (event.key === 'Home') next = 0
        else if (event.key === 'End') next = nodes.length - 1
        else {
            const width = preferences.mode === 'icons' ? Math.max(1, Math.floor((list.current?.clientWidth ?? 0) / preferences.iconSize)) : 1
            next += event.key === 'ArrowUp' ? -width : event.key === 'ArrowDown' ? width : event.key === 'ArrowLeft' ? -1 : 1
            next = Math.max(0, Math.min(nodes.length - 1, next))
        }
        nodes[next].focus(); nodes[next].scrollIntoView?.({ block: 'nearest' })
        select(entries[next], { ctrlKey: false, metaKey: false, shiftKey: event.shiftKey })
    }
    const selectedEntry = targets[targets.length - 1]
    function cell(entry: AlbumEntry, column: ExplorerColumn) {
        if (entry.kind === 'album') return column === 'kind' ? translate('gallery.album') : column === 'date' ? date(entry.album.createdAt) : '—'
        return column === 'kind' ? translate(entry.file.kind === 'image' ? 'gallery.image' : entry.file.kind === 'video' ? 'media.video' : entry.file.kind === 'audio' ? 'media.audio' : 'media.file') : column === 'size' ? entry.file.filesize == null ? '—' : formatBytes(entry.file.filesize) : column === 'date' ? date(entry.file.createdAt) : duration(entry.file.duration)
    }
    const preview = (entry: AlbumEntry, large = false) => entry.kind === 'album' ? <Folder aria-hidden="true" className={large ? 'mx-auto h-24 w-24 text-amber-400' : 'size-5 shrink-0 text-amber-400'} /> : <MediaPreview path={entry.file.thumbnailPath ?? entry.file.previewPath ?? (entry.file.kind === 'image' ? entry.file.filepath : null)} title={entryTitle(entry)} className={large ? 'aspect-video w-full rounded object-contain' : 'size-8 shrink-0 rounded object-cover'} />
    return <section aria-label={translate('explorer.title')} onKeyDown={keydown} className="flex h-full min-h-0 flex-col">
        {conversion && <BatchConvertPanel selection={conversion} allowReveal={allowReveal} onClose={() => setConversion(null)} onChanged={() => { reader.refresh(); onChanged() }} />}
        {conversionError && <p role="alert">{conversionError}</p>}
        {viewer && <MediaViewer {...viewer} onClose={closeViewer} />}
        {action && page && <AlbumExplorerDialog action={action} current={page.album} onClose={() => setAction(null)} onSaved={(album, deleted) => {
            setAction(null); setSelected(new Set()); onChanged()
            if (deleted && album.id === mediaId) { const parent = page.breadcrumbs[page.breadcrumbs.length - 2]; if (parent) onNavigate(parent.id); else onClose() }
            else reader.refresh()
        }} />}
        <header className="shrink-0 border-b border-[var(--border-line)] px-3 py-2">
            <div className="flex items-center gap-2">
                <button type="button" className={button} aria-label={translate('explorer.back')} disabled={blocked} onClick={onBack}><ArrowLeft className="size-4" /></button>
                <button type="button" className={button} aria-label={translate('explorer.forward')} disabled={blocked || !canForward} onClick={onForward}><ArrowRight className="size-4" /></button>
                <button type="button" className={button} aria-label={translate('explorer.up')} disabled={blocked} onClick={() => { const parent = page?.breadcrumbs[page.breadcrumbs.length - 2]; if (parent) onNavigate(parent.id); else onClose() }}><ArrowUp className="size-4" /></button>
                <nav aria-label={translate('explorer.path')} className="flex min-w-0 flex-1 items-center gap-2 overflow-x-auto whitespace-nowrap text-sm"><button type="button" disabled={blocked} className="shrink-0 underline" onClick={onClose}>{translate('navigation.gallery')}</button>{page?.breadcrumbs.map((item, index) => <span key={item.id} className="inline-flex shrink-0 items-center gap-2"><span aria-hidden="true">/</span><button type="button" className="max-w-56 truncate" title={item.title} aria-current={index === page.breadcrumbs.length - 1 ? 'page' : undefined} disabled={blocked || item.id === mediaId} onClick={() => onNavigate(item.id)}>{item.title}</button></span>)}</nav>
                <button type="button" className={button} aria-label={translate('common.refresh')} disabled={blocked} onClick={reader.refresh}><RefreshCw className="size-4" /></button>
                {allowConversion && <button type="button" className={button} disabled={blocked} onClick={() => void openConverter()}>{translate('conversion.title')}</button>}
                <ExplorerMenu disabled={!!action || !!viewer}>{run => <div className="divide-y divide-[var(--border-line)]">
                    <fieldset className="space-y-3 pb-3">
                        <legend className="mb-2 text-xs font-semibold text-[var(--text-muted)]">{translate('explorer.view')}</legend>
                        <label className="flex items-center justify-between gap-3 text-sm">{translate('explorer.view')}<select aria-label={translate('explorer.view')} className={button + ' bg-[var(--bg-app)]'} value={preferences.mode} onChange={event => updatePreferences({ mode: event.target.value as ExplorerMode })}>{(['icons', 'list', 'details'] as const).map(mode => <option key={mode} value={mode}>{translate('explorer.' + mode)}</option>)}</select></label>
                        {preferences.mode === 'icons' && <label className="flex items-center justify-between gap-3 text-sm">{translate('explorer.iconSize')}<input aria-label={translate('explorer.iconSize')} type="range" min="80" max="240" step="20" value={preferences.iconSize} onChange={event => updatePreferences({ iconSize: Number(event.target.value) })} /></label>}
                        {preferences.mode === 'details' && <fieldset><legend className="mb-2 text-sm">{translate('explorer.columns')}</legend><div className="grid grid-cols-2 gap-2">{columns.map(column => <label key={column} className="flex gap-2 text-sm"><input type="checkbox" checked={preferences.columns.includes(column)} onChange={event => updatePreferences({ columns: event.target.checked ? [...preferences.columns, column] : preferences.columns.filter(item => item !== column) })} />{translate('explorer.' + column)}</label>)}</div></fieldset>}
                        <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={preferences.preview} onChange={event => updatePreferences({ preview: event.target.checked })} />{translate('explorer.preview')}</label>
                    </fieldset>
                    <fieldset className="space-y-3 py-3">
                        <legend className="pt-3 text-xs font-semibold text-[var(--text-muted)]">{translate('explorer.sort')}</legend>
                        <div className="flex items-center justify-between gap-2"><select aria-label={translate('explorer.sort')} className={button + ' min-w-0 flex-1 bg-[var(--bg-app)]'} value={preferences.sort} disabled={blocked} onChange={event => updatePreferences({ sort: event.target.value as AlbumExplorerSort })}>{(['name', ...columns] as const).map(column => <option key={column} value={column}>{translate('explorer.' + column)}</option>)}</select><button type="button" className={button} disabled={blocked} aria-pressed={preferences.descending} onClick={() => updatePreferences({ descending: !preferences.descending })}>{translate(preferences.descending ? 'explorer.descending' : 'explorer.ascending')}</button></div>
                    </fieldset>
                    <fieldset className="py-3">
                        <legend className="pt-3 text-xs font-semibold text-[var(--text-muted)]">{translate('explorer.albumActions')}</legend>
                        <div className="mt-2 grid grid-cols-2 gap-2">
                            <button type="button" className={button} disabled={blocked || !page} onClick={() => run(() => setAction({ kind: 'create' }))}>{translate('explorer.create')}</button>
                            <button type="button" className={button} disabled={blocked || !editable} onClick={() => editable && run(() => setAction({ kind: 'rename', album: editable }))}>{translate('explorer.rename')}</button>
                            <button type="button" className={button} disabled={blocked || !editable} onClick={() => editable && run(() => setAction({ kind: 'move', album: editable }))}>{translate('explorer.move')}</button>
                            <button type="button" className={button + ' text-red-500'} disabled={blocked || (!editable && !removableFiles)} onClick={() => run(remove)}>{translate(removableFiles ? 'explorer.remove' : 'explorer.delete')}</button>
                        </div>
                    </fieldset>
                    <fieldset className="space-y-3 pt-3">
                        <legend className="pt-3 text-xs font-semibold text-[var(--text-muted)]">{translate('explorer.selectionAndSearch')}</legend>
                        <div className="flex flex-wrap gap-2"><button type="button" className={button} disabled={blocked || !entries.length} onClick={() => run(() => setSelected(new Set(entries.map(entryKey))))}>{translate('explorer.selectLoaded')}</button>{selected.size > 0 && <button type="button" className={button} disabled={blocked} onClick={() => run(() => setSelected(new Set()))}>{translate('gallery.selection.clear')}</button>}</div>
                        <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={recursive} disabled={blocked} onChange={event => setRecursive(event.target.checked)} />{translate('explorer.recursive')}</label>
                        <ContentFilterNotice visibility={visibility} disabled={blocked} />
                    </fieldset>
                </div>}</ExplorerMenu>
            </div>
        </header>
        <details className="shrink-0 border-b border-[var(--border-line)]" open><summary className="px-4 py-2 text-sm">{translate('explorer.search')}</summary><SearchControls vm={search} /></details>
        <div className="flex min-h-0 flex-1">
            <div ref={root} data-explorer-scroll data-keyboard-region tabIndex={-1} onScroll={() => { if (root.current) snapshot.current.scrollTop = root.current.scrollTop }} className="min-h-0 min-w-0 flex-1 overflow-auto p-4">
                {(reader.loading || reader.loadingMore) && <p role="status">{translate('gallery.loading')}</p>}
                {reader.error && <div role="alert"><p>{reader.error}</p><button type="button" className={button} onClick={reader.refresh}>{translate('common.retry')}</button></div>}
                {page && !page.total && <p className="py-12 text-center text-[var(--text-muted)]">{translate('explorer.empty')}</p>}
                <div ref={list} aria-busy={reader.loading || reader.loadingMore}>
                    {preferences.mode === 'details' ? <table className="w-full text-left text-sm"><thead><tr><th className="p-2">{translate('explorer.selection')}</th>{(['name', ...preferences.columns] as const).map(column => <th key={column} className="p-2" aria-sort={preferences.sort === column ? preferences.descending ? 'descending' : 'ascending' : undefined}><button type="button" disabled={blocked} onClick={() => updatePreferences({ sort: column, descending: preferences.sort === column ? !preferences.descending : false })}>{translate('explorer.' + column)}</button></th>)}</tr></thead><tbody>{entries.map(entry => {
                        const key = entryKey(entry)
                        return <tr key={key} data-explorer-key={key} tabIndex={0} aria-selected={selected.has(key)} onFocus={() => { focused.current = key }} onClick={event => !blocked && select(entry, event)} onDoubleClick={event => { if (!(event.target as Element).closest('input,label')) open(entry) }} className="border-t border-[var(--border-line)] aria-selected:bg-[var(--bg-active)]">
                            <td className="p-2"><input type="checkbox" aria-label={translate('explorer.select', { name: entryTitle(entry) })} checked={selected.has(key)} disabled={blocked} onClick={event => event.stopPropagation()} onChange={() => select(entry, { ctrlKey: true, metaKey: false, shiftKey: false })} /></td>
                            <td className="p-2"><button type="button" className="flex items-center gap-2 text-left" disabled={blocked} onClick={event => { if (!event.detail) open(entry) }}>{preview(entry)}<span className="break-all">{entryTitle(entry)}</span></button></td>{preferences.columns.map(column => <td key={column} className="whitespace-nowrap p-2">{cell(entry, column)}</td>)}
                        </tr>
                    })}</tbody></table> : <div role="listbox" aria-label={translate('explorer.contents')} aria-multiselectable="true" className={preferences.mode === 'icons' ? 'grid items-start gap-3' : 'space-y-1'} style={preferences.mode === 'icons' ? { gridTemplateColumns: `repeat(auto-fill,minmax(min(${preferences.iconSize}px,100%),1fr))` } : undefined}>
                        {entries.map(entry => { const key = entryKey(entry); return <div key={key} role="option" data-explorer-key={key} tabIndex={0} aria-selected={selected.has(key)} onFocus={() => { focused.current = key }} onClick={event => !blocked && select(entry, event)} onDoubleClick={event => { if (!(event.target as Element).closest('input,label')) open(entry) }} className={'relative rounded-lg border border-[var(--border-line)] p-2 aria-selected:border-[var(--color-primary)] aria-selected:bg-[var(--bg-active)] ' + (preferences.mode === 'list' ? 'flex items-center gap-3' : 'space-y-2')}>
                            <input type="checkbox" className={preferences.mode === 'icons' ? 'absolute left-2 top-2 z-10' : ''} aria-label={translate('explorer.select', { name: entryTitle(entry) })} checked={selected.has(key)} disabled={blocked} onClick={event => event.stopPropagation()} onChange={() => select(entry, { ctrlKey: true, metaKey: false, shiftKey: false })} />
                            {preview(entry, preferences.mode === 'icons')}<span className="block min-w-0 break-all text-sm">{entryTitle(entry)}</span>
                        </div> })}
                    </div>}
                </div>
                {reader.moreError && <p role="alert">{reader.moreError}</p>}
                {page?.hasNextPage && <button type="button" className={button + ' mt-4'} disabled={blocked} onClick={() => void reader.more()}>{translate(reader.moreError ? 'common.retry' : 'gallery.loadMore')}</button>}
            </div>
            {preferences.preview && <aside aria-label={translate('explorer.preview')} className="w-64 max-w-[40%] shrink-0 space-y-3 overflow-auto border-l border-[var(--border-line)] p-4 text-sm">{selectedEntry ? <>{preview(selectedEntry, true)}<h2 className="break-all font-semibold">{entryTitle(selectedEntry)}</h2>{columns.map(column => <p key={column}>{translate('explorer.' + column)}: {cell(selectedEntry, column)}</p>)}<button type="button" className={button} disabled={blocked} onClick={() => open(selectedEntry)}>{translate('explorer.open')}</button></> : <p>{translate('explorer.selectForPreview')}</p>}</aside>}
        </div>
        <footer className="shrink-0 border-t border-[var(--border-line)] px-4 py-2 text-sm text-[var(--text-muted)]">{translate('explorer.status', { total: page?.total ?? 0, loaded: entries.length, selected: selected.size })}</footer>
    </section>
}
