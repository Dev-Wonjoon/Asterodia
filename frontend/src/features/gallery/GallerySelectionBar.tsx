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
    return <section data-detail-trigger aria-label={translate("선택 항목 작업")} className="motion-selection order-last flex w-full min-w-0 flex-wrap items-center gap-2 rounded-xl border border-[var(--color-primary)] bg-[var(--bg-app)] px-3 py-2 xl:order-none xl:w-auto xl:flex-1 xl:basis-0">
        <div className="mr-auto shrink-0">
            <p role="status" aria-label={translate("전체 {0} / 선택 {1}", { "0": total ?? translate("확인 중"), "1": count })} className="text-sm font-semibold">{translate("{0}개 선택", { "0": count })}</p>
            {fileCount > 0 && groupCount > 0 && <p className="text-xs text-[var(--text-muted)]">{translate("묶음 {0}개 · 개별 파일 {1}개", { "0": groupCount, "1": fileCount })}</p>}
        </div>
        <button type="button" className={button} disabled={disabled} onClick={onAdd}><ListPlus className="size-4" aria-hidden="true" />{translate("앨범에 추가")}</button>
        <button type="button" className={button} disabled={disabled} onClick={onMove}><FolderInput className="size-4" aria-hidden="true" />{translate("한 폴더에 모으기")}</button>
        <button type="button" className={button} disabled={disabled} onClick={onMerge}><Merge className="size-4" aria-hidden="true" />{translate("병합")}</button>
        {groupCount > 0 && <GalleryActionMenu label={translate("선택 작업")} visibleLabel disabled={disabled}>
            {fileCount > 0 && <p className="px-3 py-2 text-xs text-[var(--text-muted)]">{translate("아래 작업은 선택한 묶음 {0}개에 적용됩니다.", { "0": groupCount })}</p>}
            {([['favorite', translate("즐겨찾기 추가")], ['unfavorite', translate("즐겨찾기 해제")], ['addTags', translate("태그 추가")], ['removeTags', translate("태그 제거")], ['openFolders', translate("파일 위치 열기")], ['unregister', translate("등록 해제")]] as const).map(([command, label]) => <button type="button" role="menuitem" key={command} className={item + (command === 'unregister' ? ' text-red-500' : '')} onClick={() => onBulk(command)}>{label}</button>)}
        </GalleryActionMenu>}
        <button type="button" className={button} disabled={disabled} onClick={onClear}><X className="size-4" aria-hidden="true" />{translate("선택 해제")}</button>
    </section>
}
