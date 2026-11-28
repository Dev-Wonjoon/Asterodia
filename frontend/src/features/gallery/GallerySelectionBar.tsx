import { translate, useLanguage } from '@renderer/shared/i18n'
import { ListPlus, X, RefreshCw } from 'lucide-react'
import { GalleryActionMenu } from './GalleryToolbar'
import type { BulkCommand } from './useGalleryBulkActions'

export function GallerySelectionBar({ count, groupCount, fileCount, total, disabled, removalLabel, onAdd, onMove, onMerge, onClear, onBulk, active = false, onRefresh, recoveryDisabled }: {
    onRefresh?(): void; recoveryDisabled?: boolean
    active?: boolean
    count: number; groupCount: number; fileCount: number; total: number | null; disabled: boolean
    removalLabel: string
    onAdd(): void; onMove(): void; onMerge(): void; onClear(): void; onBulk(command: BulkCommand): void
}) {
    useLanguage()
    if (!count && !active) return null
    const button = 'inline-flex h-9 shrink-0 items-center justify-center gap-1.5 whitespace-nowrap rounded-lg border border-[var(--border-line)] px-2 text-sm disabled:opacity-40'
    const item = 'block w-full rounded px-3 py-2 text-left text-sm hover:bg-[var(--bg-hover)] focus:bg-[var(--bg-hover)] focus:outline-none disabled:opacity-40'
    const label = 'whitespace-nowrap'
    return <section data-action-boundary data-detail-trigger aria-label={translate("gallery.selectedItemActions")} className="flex h-11 w-full min-w-0 items-center gap-2 rounded-lg bg-[var(--bg-sidebar)] px-2">
        <div className="min-w-0 max-w-24 shrink" title={fileCount > 0 && groupCount > 0 ? translate('gallery.collectionsIndividualFiles', { collections: groupCount, files: fileCount }) : undefined}>
            <p role="status" aria-label={translate("gallery.totalSelected", { total: total ?? translate("gallery.checking"), selected: count })} className="truncate text-sm font-semibold">{translate("gallery.selection.count", { count: count })}</p>
            {fileCount > 0 && groupCount > 0 && <span className="sr-only">{translate('gallery.collectionsIndividualFiles', { collections: groupCount, files: fileCount })}</span>}
        </div>
        <div className="flex min-w-0 flex-1 items-center justify-end gap-1">
            <button type="button" className={button} aria-label={translate('gallery.album.add')} title={translate('gallery.album.add')} disabled={disabled} onClick={onAdd}><ListPlus className="size-4" aria-hidden="true" /><span className={label}>{translate("gallery.album.add")}</span></button>
        </div>
        <div className="shrink-0"><GalleryActionMenu label={translate("gallery.selectionActions")} visibleLabel disabled={disabled}>
            <button type="button" role="menuitem" className={item} onClick={onMove}>{translate('gallery.collectInOneFolder')}</button>
            <button type="button" role="menuitem" className={item} onClick={onMerge}>{translate('gallery.merge')}</button>
            <button type="button" role="menuitem" className={item + ' text-red-500'} onClick={() => onBulk('unregister')}>{removalLabel}</button>
            {groupCount > 0 && <>
            {fileCount > 0 && <p className="px-3 py-2 text-xs text-[var(--text-muted)]">{translate("gallery.theActionsBelowApplyToSelectedCollections", { count: groupCount })}</p>}
            {([['favorite', translate("gallery.favorite.add")], ['unfavorite', translate("gallery.favorite.remove")], ['addPlatforms', translate('platforms.add')], ['removePlatforms', translate('platforms.remove')], ['addTags', translate("gallery.addTags")], ['removeTags', translate("gallery.removeTags")], ['openFolders', translate("gallery.openFolder")]] as const).map(([command, label]) => <button type="button" role="menuitem" key={command} className={item} onClick={() => onBulk(command)}>{label}</button>)}
            </>}
        </GalleryActionMenu></div>
        {onRefresh && <button type="button" className={button} aria-label={translate('common.refresh')} title={translate('common.refresh')} disabled={recoveryDisabled ?? disabled} onClick={onRefresh}><RefreshCw className="size-4" aria-hidden="true" /></button>}
        <button type="button" className={button} aria-label={translate('gallery.selection.clear')} title={translate('gallery.selection.clear')} disabled={recoveryDisabled ?? disabled} onClick={onClear}><X className="size-4" aria-hidden="true" /><span className="sr-only">{translate("gallery.selection.clear")}</span></button>
    </section>
}
