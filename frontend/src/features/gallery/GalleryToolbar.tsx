import { translate, useLanguage } from '@renderer/shared/i18n'
import { useEffect, useId, useRef, useState } from 'react'
import { ChevronDown, ListChecks, Menu as MenuIcon, RefreshCw } from 'lucide-react'
import { ActionButton, ActionContent } from '@renderer/shared/components/ui/ActionButton'
import type { GalleryLayout, GallerySort, GalleryView } from '@contracts/media-v2'

const button = 'rounded-lg border border-[var(--border-line)] px-3 py-2 text-sm disabled:opacity-40'
export function GalleryActionMenu({ label, disabled, children, visibleLabel = false, compact = false }: { label: string; disabled?: boolean; children: React.ReactNode; visibleLabel?: boolean; compact?: boolean }) {
    useLanguage()
    const [open, setOpen] = useState(false)
    const root = useRef<HTMLDivElement>(null), trigger = useRef<HTMLButtonElement>(null), menu = useRef<HTMLDivElement>(null)
    const id = useId()
    useEffect(() => {
        if (!open) return
        menu.current?.querySelector<HTMLButtonElement>('button:not(:disabled)')?.focus()
        const outside = (event: PointerEvent) => { if (event.target instanceof Node && !root.current?.contains(event.target)) setOpen(false) }
        document.addEventListener('pointerdown', outside)
        return () => document.removeEventListener('pointerdown', outside)
    }, [open])
    useEffect(() => { if (disabled) setOpen(false) }, [disabled])
    return <div ref={root} className="relative" data-detail-trigger onKeyDown={event => {
        if (!open) return
        if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); setOpen(false); trigger.current?.focus() }
        if (event.key === 'Tab') setOpen(false)
        if (['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) {
            event.preventDefault()
            const items = [...menu.current?.querySelectorAll<HTMLButtonElement>('button:not(:disabled)') ?? []]
            const index = items.findIndex(item => item === document.activeElement)
            const next = event.key === 'Home' ? 0 : event.key === 'End' ? items.length - 1 : (index + (event.key === 'ArrowDown' ? 1 : -1) + items.length) % items.length
            items[next]?.focus()
        }
    }}>
        <button ref={trigger} type="button" disabled={disabled} aria-label={label} title={label} aria-haspopup="menu" aria-expanded={open} aria-controls={open ? id : undefined} className={compact ? 'grid size-7 place-items-center rounded-md border border-[var(--border-line)] disabled:opacity-40' : button + ' flex items-center gap-1'} onClick={() => setOpen(value => !value)}>{compact ? <><ListChecks className="size-4" aria-hidden="true" /><span className="sr-only">{label}</span></> : <>{visibleLabel ? label : <ActionContent icon={label === translate("gallery.selectionActions") ? <ListChecks /> : <MenuIcon />} label={label} />}<ChevronDown aria-hidden="true" className="size-4" /></>}</button>
        {open && <div ref={menu} id={id} role="menu" aria-label={label} className="motion-menu absolute right-0 top-full z-30 mt-1 w-52 rounded-lg border border-[var(--border-line)] bg-[var(--bg-sidebar)] p-1 shadow-xl" onClick={event => {
            if ((event.target as HTMLElement).closest('button:not(:disabled)')) { setOpen(false); trigger.current?.focus() }
        }}>{children}</div>}
    </div>
}

export function GalleryToolbar({ count, disabled, actionDisabled, importDisabled, canImport, hasImportResult, editorDisabled, onOpenEditor, onImport, onRecentImport, onSelectAll, onClear, onRefresh, galleryView, showAlbumFiles, viewDisabled, onViewChange, onShowAlbumFilesChange, presentation }: {
    presentation?: { layout: GalleryLayout; sort: GallerySort; descending: boolean; onLayout(value: GalleryLayout): void; onSort(value: GallerySort): void; onDescending(value: boolean): void }
    count: number; disabled: boolean; actionDisabled: boolean; importDisabled: boolean; canImport: boolean; hasImportResult: boolean
    onImport: () => void; onRecentImport: () => void; onSelectAll: () => void; onClear: () => void; onRefresh: () => void
    editorDisabled: boolean; onOpenEditor: () => void
    galleryView: GalleryView; showAlbumFiles: boolean; viewDisabled: boolean
    onViewChange: (view: GalleryView) => void; onShowAlbumFilesChange: (show: boolean) => void
}) {
    useLanguage()
    const item = 'block w-full rounded px-3 py-2 text-left text-sm hover:bg-[var(--bg-hover)] focus:bg-[var(--bg-hover)] focus:outline-none disabled:opacity-40'
    return <div className="flex flex-wrap items-center justify-end gap-2">
        <GalleryActionMenu label={{all:translate("gallery.all"),albums:translate("gallery.albumsOnly"),files:translate("gallery.filesOnly")}[galleryView] + translate("gallery.suffix.view")} visibleLabel disabled={viewDisabled}>
            {([['all',translate("gallery.all")],['albums',translate("gallery.albumsOnly")],['files',translate("gallery.filesOnly")]] as const).map(([view, label]) => <button key={view} type="button" role="menuitemradio" aria-checked={galleryView === view} className={item + ' aria-checked:bg-[var(--bg-hover)] aria-checked:font-semibold'} onClick={() => onViewChange(view)}>{label}</button>)}
            <button type="button" role="menuitemcheckbox" aria-checked={showAlbumFiles} disabled={galleryView === 'albums'} className={item + ' border-t border-[var(--border-line)] aria-checked:bg-[var(--bg-hover)] aria-checked:font-semibold'} onClick={() => onShowAlbumFilesChange(!showAlbumFiles)}>{translate("gallery.showHiddenFiles")}</button>
            {presentation && <div className="border-t border-[var(--border-line)] pt-1">{(['cards','details'] as const).map(layout => <button key={layout} type="button" role="menuitemradio" aria-checked={presentation.layout === layout} className={item + ' aria-checked:bg-[var(--bg-hover)]'} onClick={() => presentation.onLayout(layout)}>{translate('gallery.layout.' + layout)}</button>)}</div>}
        </GalleryActionMenu>
        {presentation && <GalleryActionMenu label={translate('gallery.order.title')} visibleLabel disabled={viewDisabled}>
            {(['name','registered','fileCreated','platform','tag'] as const).map(sort => <button key={sort} type="button" role="menuitemradio" aria-checked={presentation.sort === sort} className={item + ' aria-checked:bg-[var(--bg-hover)]'} onClick={() => presentation.onSort(sort)}>{translate('gallery.order.' + sort)}</button>)}
            <div className="border-t border-[var(--border-line)] pt-1">{[false,true].map(descending => <button key={String(descending)} type="button" role="menuitemradio" aria-checked={presentation.descending === descending} className={item + ' aria-checked:bg-[var(--bg-hover)]'} onClick={() => presentation.onDescending(descending)}>{translate(descending ? 'gallery.order.descending' : 'gallery.order.ascending')}</button>)}</div>
        </GalleryActionMenu>}
        <ActionButton icon={<RefreshCw />} label={translate("common.refresh")} disabled={disabled} onClick={onRefresh} />
        <GalleryActionMenu label={translate("gallery.menu")} disabled={disabled}>
            <button type="button" role="menuitem" className={item} disabled={editorDisabled} onClick={onOpenEditor}>{translate("gallery.editor.createAnimation")}</button>
            {canImport && <><button type="button" role="menuitem" className={item} disabled={importDisabled} onClick={onImport}>{translate("gallery.importFolder")}</button>{hasImportResult && <button type="button" role="menuitem" className={item} onClick={onRecentImport}>{translate("gallery.viewRecentImport")}</button>}</>}
            <button type="button" role="menuitem" className={item} disabled={disabled || actionDisabled} onClick={onSelectAll}>{translate("gallery.selection.all")}</button>
            <button type="button" role="menuitem" className={item} disabled={disabled || !count} onClick={onClear}>{translate("gallery.selection.clear")}</button>
        </GalleryActionMenu>
    </div>
}
