import { translate, useLanguage } from '@renderer/shared/i18n'
import { FolderInput, ListPlus, Merge, X } from 'lucide-react'
import { GalleryActionMenu } from './GalleryToolbar'
import type { BulkCommand } from './useGalleryBulkActions'

export function GallerySelectionBar({ count, groupCount, fileCount, total, disabled, onAdd, onMove, onMerge, onClear, onBulk }: {
    count: number; groupCount: number; fileCount: number; total: number | null; disabled: boolean
    onAdd(): void; onMove(): void; onMerge(): void; onClear(): void; onBulk(command: BulkCommand): void
}) {
    useLanguage()
    if (!count) return null
    const button = 'inline-flex shrink-0 items-center gap-2 whitespace-nowrap rounded-lg border border-[var(--border-line)] px-3 py-2 text-sm disabled:opacity-40'
    const item = 'block w-full rounded px-3 py-2 text-left text-sm hover:bg-[var(--bg-hover)] focus:bg-[var(--bg-hover)] focus:outline-none disabled:opacity-40'
    return <section data-detail-trigger aria-label={translate("gallery.selectedItemActions")} className="motion-selection order-last flex w-full min-w-0 flex-wrap items-center gap-2 rounded-xl border border-[var(--color-primary)] bg-[var(--bg-app)] px-3 py-2 xl:order-none xl:w-auto xl:flex-1 xl:basis-0">
        <div className="mr-auto shrink-0">
            <p role="status" aria-label={translate("gallery.totalSelected", { total: total ?? translate("gallery.checking"), selected: count })} className="text-sm font-semibold">{translate("gallery.selection.count", { count: count })}</p>
            {fileCount > 0 && groupCount > 0 && <p className="text-xs text-[var(--text-muted)]">{translate("gallery.collectionsIndividualFiles", { collections: groupCount, files: fileCount })}</p>}
        </div>
        <button type="button" className={button} disabled={disabled} onClick={onAdd}><ListPlus className="size-4" aria-hidden="true" />{translate("gallery.album.add")}</button>
        <button type="button" className={button} disabled={disabled} onClick={onMove}><FolderInput className="size-4" aria-hidden="true" />{translate("gallery.collectInOneFolder")}</button>
        <button type="button" className={button} disabled={disabled} onClick={onMerge}><Merge className="size-4" aria-hidden="true" />{translate("gallery.merge")}</button>
        {groupCount > 0 && <GalleryActionMenu label={translate("gallery.selectionActions")} visibleLabel disabled={disabled}>
            {fileCount > 0 && <p className="px-3 py-2 text-xs text-[var(--text-muted)]">{translate("gallery.theActionsBelowApplyToSelectedCollections", { count: groupCount })}</p>}
            {([['favorite', translate("gallery.favorite.add")], ['unfavorite', translate("gallery.favorite.remove")], ['addTags', translate("gallery.addTags")], ['removeTags', translate("gallery.removeTags")], ['openFolders', translate("gallery.openFolder")], ['unregister', translate("gallery.unregister")]] as const).map(([command, label]) => <button type="button" role="menuitem" key={command} className={item + (command === 'unregister' ? ' text-red-500' : '')} onClick={() => onBulk(command)}>{label}</button>)}
        </GalleryActionMenu>}
        <button type="button" className={button} disabled={disabled} onClick={onClear}><X className="size-4" aria-hidden="true" />{translate("gallery.selection.clear")}</button>
    </section>
}
