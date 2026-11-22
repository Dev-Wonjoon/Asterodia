import { useEffect, useId, useRef, useState } from 'react'
import { ChevronDown } from 'lucide-react'
import type { BulkCommand } from './useGalleryBulkActions'

const button = 'rounded-lg border border-[var(--border-line)] px-3 py-2 text-sm disabled:opacity-40'
function Menu({ label, disabled, children }: { label: string; disabled?: boolean; children: React.ReactNode }) {
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
        <button ref={trigger} type="button" disabled={disabled} aria-haspopup="menu" aria-expanded={open} aria-controls={open ? id : undefined} className={button + ' flex items-center gap-1'} onClick={() => setOpen(value => !value)}>{label}<ChevronDown aria-hidden="true" className="size-4" /></button>
        {open && <div ref={menu} id={id} role="menu" aria-label={label} className="motion-menu absolute right-0 top-full z-30 mt-1 w-52 rounded-lg border border-[var(--border-line)] bg-[var(--bg-sidebar)] p-1 shadow-xl" onClick={event => {
            if ((event.target as HTMLElement).closest('button:not(:disabled)')) { setOpen(false); trigger.current?.focus() }
        }}>{children}</div>}
    </div>
}

export function GalleryToolbar({ total, count, disabled, actionDisabled, importDisabled, canImport, hasImportResult, onImport, onRecentImport, onSelectAll, onClear, onRefresh, onMove, onBulk }: {
    total: number | null; count: number; disabled: boolean; actionDisabled: boolean; importDisabled: boolean; canImport: boolean; hasImportResult: boolean
    onImport: () => void; onRecentImport: () => void; onSelectAll: () => void; onClear: () => void; onRefresh: () => void; onMove: () => void; onBulk: (command: BulkCommand) => void
}) {
    const item = 'block w-full rounded px-3 py-2 text-left text-sm hover:bg-[var(--bg-hover)] focus:bg-[var(--bg-hover)] focus:outline-none disabled:opacity-40'
    return <div className="flex flex-wrap items-center justify-end gap-2">
        {count > 0 && <div className="motion-selection flex items-center gap-2">
            <Menu label="선택 작업" disabled={disabled || actionDisabled}>
                <button type="button" role="menuitem" className={item} onClick={onMove}>한 폴더에 모으기</button>
                {([['favorite', '즐겨찾기 추가'], ['unfavorite', '즐겨찾기 해제'], ['addTags', '태그 추가'], ['removeTags', '태그 제거'], ['openFolders', '파일 위치 열기'], ['unregister', '등록 해제']] as const).map(([command, label]) => <button type="button" role="menuitem" key={command} className={item + (command === 'unregister' ? ' text-red-500' : '')} onClick={() => onBulk(command)}>{label}</button>)}
            </Menu>
            <span className="whitespace-nowrap text-right text-sm tabular-nums text-[var(--text-muted)]" style={{ minWidth: `${String(total ?? 0).length * 2 + 3}ch` }} role="status" aria-label={`전체 ${total ?? '확인 중'} / 선택 ${count}`} title="전체 수 / 선택한 수">{total ?? '–'} / {count}</span>
        </div>}
        <button type="button" className={button} disabled={disabled} onClick={onRefresh}>새로고침</button>
        <Menu label="메뉴" disabled={disabled}>
            {canImport && <><button type="button" role="menuitem" className={item} disabled={importDisabled} onClick={onImport}>폴더 가져오기</button>{hasImportResult && <button type="button" role="menuitem" className={item} onClick={onRecentImport}>최근 가져오기 보기</button>}</>}
            <button type="button" role="menuitem" className={item} disabled={disabled || actionDisabled} onClick={onSelectAll}>현재 목록 전체 선택</button>
            <button type="button" role="menuitem" className={item} disabled={disabled || !count} onClick={onClear}>선택 해제</button>
        </Menu>
    </div>
}
