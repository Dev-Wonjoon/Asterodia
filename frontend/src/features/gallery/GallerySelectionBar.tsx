import { FolderInput, ListPlus, X } from 'lucide-react'
import { GalleryActionMenu } from './GalleryToolbar'
import type { BulkCommand } from './useGalleryBulkActions'

export function GallerySelectionBar({ count, groupCount, fileCount, total, disabled, onAdd, onMove, onClear, onBulk }: {
    count: number; groupCount: number; fileCount: number; total: number | null; disabled: boolean
    onAdd(): void; onMove(): void; onClear(): void; onBulk(command: BulkCommand): void
}) {
    if (!count) return null
    const button = 'inline-flex items-center gap-2 rounded-lg border border-[var(--border-line)] px-3 py-2 text-sm disabled:opacity-40'
    const item = 'block w-full rounded px-3 py-2 text-left text-sm hover:bg-[var(--bg-hover)] focus:bg-[var(--bg-hover)] focus:outline-none disabled:opacity-40'
    return <section data-detail-trigger aria-label="선택 항목 작업" className="motion-selection sticky top-0 z-30 flex flex-wrap items-center gap-2 rounded-xl border border-[var(--color-primary)] bg-[var(--bg-app)] p-3 shadow-lg">
        <div className="mr-auto">
            <p role="status" aria-label={`전체 ${total ?? '확인 중'} / 선택 ${count}`} className="text-sm font-semibold">{count}개 선택</p>
            {fileCount > 0 && groupCount > 0 && <p className="text-xs text-[var(--text-muted)]">묶음 {groupCount}개 · 개별 파일 {fileCount}개</p>}
        </div>
        <button type="button" className={button} disabled={disabled} onClick={onAdd}><ListPlus className="size-4" aria-hidden="true" />앨범에 추가</button>
        <button type="button" className={button} disabled={disabled} onClick={onMove}><FolderInput className="size-4" aria-hidden="true" />한 폴더에 모으기</button>
        {groupCount > 0 && <GalleryActionMenu label="선택 작업" visibleLabel disabled={disabled}>
            {fileCount > 0 && <p className="px-3 py-2 text-xs text-[var(--text-muted)]">아래 작업은 선택한 묶음 {groupCount}개에 적용됩니다.</p>}
            {([['favorite', '즐겨찾기 추가'], ['unfavorite', '즐겨찾기 해제'], ['addTags', '태그 추가'], ['removeTags', '태그 제거'], ['openFolders', '파일 위치 열기'], ['unregister', '등록 해제']] as const).map(([command, label]) => <button type="button" role="menuitem" key={command} className={item + (command === 'unregister' ? ' text-red-500' : '')} onClick={() => onBulk(command)}>{label}</button>)}
        </GalleryActionMenu>}
        <button type="button" className={button} disabled={disabled} onClick={onClear}><X className="size-4" aria-hidden="true" />선택 해제</button>
    </section>
}
