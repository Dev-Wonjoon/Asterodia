import { translate, useLanguage } from '@renderer/shared/i18n'
import { useEffect, useId, useRef, useState } from 'react'
import { ChevronDown, ListChecks, Menu as MenuIcon, RefreshCw } from 'lucide-react'
import { ActionButton, ActionContent } from '@renderer/shared/components/ui/ActionButton'
import type { GalleryView } from '@contracts/media-v2'

const button = 'rounded-lg border border-[var(--border-line)] px-3 py-2 text-sm disabled:opacity-40'
export function GalleryActionMenu({ label, disabled, children, visibleLabel = false }: { label: string; disabled?: boolean; children: React.ReactNode; visibleLabel?: boolean }) {
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
        <button ref={trigger} type="button" disabled={disabled} aria-label={label} title={label} aria-haspopup="menu" aria-expanded={open} aria-controls={open ? id : undefined} className={button + ' flex items-center gap-1'} onClick={() => setOpen(value => !value)}>{visibleLabel ? label : <ActionContent icon={label === translate("선택 작업") ? <ListChecks /> : <MenuIcon />} label={label} />}<ChevronDown aria-hidden="true" className="size-4" /></button>
        {open && <div ref={menu} id={id} role="menu" aria-label={label} className="motion-menu absolute right-0 top-full z-30 mt-1 w-52 rounded-lg border border-[var(--border-line)] bg-[var(--bg-sidebar)] p-1 shadow-xl" onClick={event => {
            if ((event.target as HTMLElement).closest('button:not(:disabled)')) { setOpen(false); trigger.current?.focus() }
        }}>{children}</div>}
    </div>
}

export function GalleryToolbar({ count, disabled, actionDisabled, importDisabled, canImport, hasImportResult, editorDisabled, onOpenEditor, onImport, onRecentImport, onSelectAll, onClear, onRefresh, galleryView, showAlbumFiles, viewDisabled, onViewChange, onShowAlbumFilesChange }: {
    count: number; disabled: boolean; actionDisabled: boolean; importDisabled: boolean; canImport: boolean; hasImportResult: boolean
    onImport: () => void; onRecentImport: () => void; onSelectAll: () => void; onClear: () => void; onRefresh: () => void
    editorDisabled: boolean; onOpenEditor: () => void
    galleryView: GalleryView; showAlbumFiles: boolean; viewDisabled: boolean
    onViewChange: (view: GalleryView) => void; onShowAlbumFilesChange: (show: boolean) => void
}) {
    useLanguage()
    const item = 'block w-full rounded px-3 py-2 text-left text-sm hover:bg-[var(--bg-hover)] focus:bg-[var(--bg-hover)] focus:outline-none disabled:opacity-40'
    return <div className="flex flex-wrap items-center justify-end gap-2">
        <GalleryActionMenu label={{all:translate("전체"),albums:translate("앨범만"),files:translate("파일만")}[galleryView] + translate(" 보기")} visibleLabel disabled={viewDisabled}>
            {([['all',translate("전체")],['albums',translate("앨범만")],['files',translate("파일만")]] as const).map(([view, label]) => <button key={view} type="button" role="menuitemradio" aria-checked={galleryView === view} className={item + ' aria-checked:bg-[var(--bg-hover)] aria-checked:font-semibold'} onClick={() => onViewChange(view)}>{label}</button>)}
            <button type="button" role="menuitemcheckbox" aria-checked={showAlbumFiles} disabled={galleryView === 'albums'} className={item + ' border-t border-[var(--border-line)] aria-checked:bg-[var(--bg-hover)] aria-checked:font-semibold'} onClick={() => onShowAlbumFilesChange(!showAlbumFiles)}>{translate("숨긴 파일 표시")}</button>
        </GalleryActionMenu>
        <ActionButton icon={<RefreshCw />} label={translate("새로고침")} disabled={disabled} onClick={onRefresh} />
        <GalleryActionMenu label={translate("메뉴")} disabled={disabled}>
            <button type="button" role="menuitem" className={item} disabled={editorDisabled} onClick={onOpenEditor}>{translate("GIF, WebP로 만들기")}</button>
            {canImport && <><button type="button" role="menuitem" className={item} disabled={importDisabled} onClick={onImport}>{translate("폴더 가져오기")}</button>{hasImportResult && <button type="button" role="menuitem" className={item} onClick={onRecentImport}>{translate("최근 가져오기 보기")}</button>}</>}
            <button type="button" role="menuitem" className={item} disabled={disabled || actionDisabled} onClick={onSelectAll}>{translate("현재 목록 전체 선택")}</button>
            <button type="button" role="menuitem" className={item} disabled={disabled || !count} onClick={onClear}>{translate("선택 해제")}</button>
        </GalleryActionMenu>
    </div>
}
