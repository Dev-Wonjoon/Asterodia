import { translate, useLanguage } from '@renderer/shared/i18n'
import { useEffect, useId, useRef, useState, type MouseEvent } from 'react'
import { Ellipsis, FolderOpen, Pencil, Star, Tags, Trash2, Info, Play, ListPlus, ChevronDown, ChevronUp, Film } from 'lucide-react'
import { ActionContent } from '@renderer/shared/components/ui/ActionButton'

interface GalleryCardActionsProps {
    title: string
    isFavorite: boolean
    disabled: boolean
    mutationDisabled: boolean
    favoriteDisabledReason?: string
    onToggleFavorite: () => void
    onOpenFolder: () => void
    onEdit: (field: 'title' | 'tags') => void
    onUnregister: () => void
    onView?: () => void
    onDetails?: () => void
    onAdd?: () => void
    onEditAlbum?: () => void
    fileOnly?: boolean
    onToggleFiles?: (event: MouseEvent<HTMLButtonElement>) => void
    filesLabel?: string
    filesExpanded?: boolean
    onCreateAnimation?: () => void
    editorDisabled?: boolean
    readOnly?: boolean
}

export function GalleryCardActions({
    title,
    isFavorite,
    disabled,
    mutationDisabled,
    favoriteDisabledReason,
    onToggleFavorite,
    onOpenFolder,
    onEdit,
    onUnregister, onView, onDetails, onAdd, onEditAlbum, fileOnly = false, onToggleFiles, filesLabel, filesExpanded, onCreateAnimation, editorDisabled, readOnly = false
}: GalleryCardActionsProps): React.JSX.Element {
    useLanguage()
    const [open, setOpen] = useState(false)
    const [opensUp, setOpensUp] = useState(true)
    const [menuHeight, setMenuHeight] = useState(420)
    const [menuShift, setMenuShift] = useState(0)
    const root = useRef<HTMLDivElement>(null)
    const trigger = useRef<HTMLButtonElement>(null)
    const menu = useRef<HTMLDivElement>(null)
    const menuId = useId()
    const starLabel = title + (isFavorite ? translate(" 즐겨찾기 해제") : translate(" 즐겨찾기 추가"))
    const iconButton =
        'flex min-h-9 min-w-9 px-2 items-center justify-center rounded-lg transition-colors hover:bg-[var(--bg-hover)] hover:text-[var(--text-main)] focus-visible:outline-2 focus-visible:outline-[var(--color-primary)] disabled:opacity-40 disabled:pointer-events-none'
    const menuItem =
        'flex w-full items-center gap-2 rounded px-3 py-2 text-left text-sm hover:bg-[var(--bg-hover)] focus:bg-[var(--bg-hover)] focus:outline-none disabled:opacity-40'

    function closeMenu(restore = false): void {
        setOpen(false)
        if (restore) trigger.current?.focus({ preventScroll: true })
    }
    useEffect(() => {
        if (!open) return
        menu.current?.querySelector<HTMLButtonElement>('button:not(:disabled)')?.focus({ preventScroll: true })
        const outside = (event: Event): void => {
            if (event.target instanceof Node && !root.current?.contains(event.target))
                setOpen(false)
        }
        document.addEventListener('pointerdown', outside)
        document.addEventListener('click', outside, true)
        return () => {
            document.removeEventListener('pointerdown', outside)
            document.removeEventListener('click', outside, true)
        }
    }, [open])

    return (
        <div
            ref={root}
            data-detail-trigger
            data-action-boundary
            className="gallery-card-utilities relative flex flex-nowrap items-center justify-end gap-1 border-t border-[var(--border-line)] px-2 py-1.5 text-sm"
            onClick={(event) => event.stopPropagation()}
            onKeyDown={(event) => {
                if (!open) return
                if (event.key === 'Escape') {
                    event.preventDefault()
                    event.stopPropagation()
                    closeMenu(true)
                }
                if (event.key === 'Tab') closeMenu()
                if (['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) {
                    event.preventDefault()
                    const items = Array.from(
                        menu.current?.querySelectorAll<HTMLButtonElement>(
                            'button:not(:disabled)'
                        ) ?? []
                    )
                    const index = items.findIndex((item) => item === document.activeElement)
                    const next =
                        event.key === 'Home'
                            ? 0
                            : event.key === 'End'
                              ? items.length - 1
                              : (index + (event.key === 'ArrowDown' ? 1 : -1) + items.length) %
                                items.length
                    items[next]?.focus()
                }
            }}
        >
            {!fileOnly && !readOnly && <button
                type="button"
                className={
                    iconButton + (isFavorite ? ' text-amber-500' : ' text-[var(--text-muted)]')
                }
                disabled={disabled || mutationDisabled || Boolean(favoriteDisabledReason)}
                aria-label={starLabel}
                aria-pressed={isFavorite}
                title={favoriteDisabledReason || (isFavorite ? translate("즐겨찾기 해제") : translate("즐겨찾기 추가"))}
                onClick={() => {
                    closeMenu()
                    onToggleFavorite()
                }}
            >
                <ActionContent label={isFavorite ? translate("즐겨찾기 해제") : translate("즐겨찾기 추가")} icon={<Star
                    className="size-[18px]"
                    fill={isFavorite ? 'currentColor' : 'none'}
                    aria-hidden="true"
                />} />
            </button>}
            {!fileOnly && !readOnly && <button
                type="button"
                className={iconButton + ' text-[var(--text-muted)]'}
                disabled={disabled}
                aria-label={title + translate(" 파일 위치 열기")}
                title={translate("파일 위치 열기")}
                onClick={() => {
                    closeMenu()
                    onOpenFolder()
                }}
            >
                <ActionContent icon={<FolderOpen className="size-[18px]" />} label={translate("파일 위치 열기")} />
            </button>}
            <button
                ref={trigger}
                type="button"
                className="flex size-9 shrink-0 items-center justify-center rounded-lg hover:bg-[var(--bg-hover)] focus-visible:outline-2 focus-visible:outline-[var(--color-primary)] disabled:opacity-40"
                disabled={disabled}
                aria-label={title + translate(" 더보기")}
                title={translate("더보기")}
                aria-haspopup="menu"
                aria-expanded={open}
                aria-controls={open ? menuId : undefined}
                onClick={() => {
                    const bounds = trigger.current?.getBoundingClientRect()
                    const main = root.current?.closest('main')?.getBoundingClientRect()
                    if (bounds) {
                        const rootBounds = root.current!.getBoundingClientRect()
                        const menuWidth = Math.min(192, window.innerWidth - 16)
                        setMenuShift(Math.max(0, Math.max(8, (main?.left ?? 0) + 8) - (rootBounds.right - 8 - menuWidth)))
                        const above = bounds.top - Math.max(main?.top ?? 0, 0)
                        const below = Math.min(main?.bottom ?? window.innerHeight, window.innerHeight) - bounds.bottom
                        const up = above > below
                        setOpensUp(up)
                        setMenuHeight(Math.max(100, Math.min(420, (up ? above : below) - 8)))
                    }
                    setOpen((previous) => !previous)
                }}
            >
                <Ellipsis className="size-[18px]" aria-hidden="true" />
            </button>
            {open && (
                <div
                    ref={menu}
                    id={menuId}
                    role="menu"
                    aria-label={title + translate(" 카드 메뉴")}
                    style={{ maxHeight: menuHeight, right: 8 - menuShift, maxWidth: 'calc(100vw - 16px)' }}
                    className={
                        'motion-menu absolute right-2 z-30 w-48 overflow-y-auto overscroll-contain rounded-lg border border-[var(--border-line)] bg-[var(--bg-sidebar)] p-1 shadow-xl ' +
                        (opensUp ? 'motion-menu-up bottom-full mb-1' : 'top-full mt-1')
                    }
                >
                    {onView && <button type="button" role="menuitem" className={menuItem} disabled={disabled} onClick={() => { closeMenu(); onView() }}><Play className="size-4" aria-hidden="true" />{translate("감상하기")}</button>}
                    {onDetails && <button type="button" role="menuitem" className={menuItem} onClick={() => { closeMenu(); onDetails() }}><Info className="size-4" aria-hidden="true" />{translate("상세 정보")}</button>}
                    {onCreateAnimation && <button type="button" role="menuitem" className={menuItem} disabled={mutationDisabled || editorDisabled} onClick={() => { closeMenu(); onCreateAnimation() }}><Film className="size-4" aria-hidden="true" />{translate("GIF, WebP로 만들기")}</button>}
                    {onAdd && <button type="button" role="menuitem" className={menuItem} disabled={mutationDisabled} onClick={() => { closeMenu(); onAdd() }}><ListPlus className="size-4" aria-hidden="true" />{translate("앨범에 추가")}</button>}
                    {onToggleFiles && <button type="button" role="menuitem" className={menuItem} disabled={mutationDisabled} aria-expanded={filesExpanded} onClick={event => { closeMenu(); onToggleFiles(event) }}>{filesExpanded ? <ChevronUp className="size-4" aria-hidden="true" /> : <ChevronDown className="size-4" aria-hidden="true" />}{filesLabel}</button>}
                    {onEditAlbum && <button type="button" role="menuitem" className={menuItem} disabled={mutationDisabled} onClick={() => { closeMenu(); onEditAlbum() }}><Pencil className="size-4" aria-hidden="true" />{translate("앨범 편집")}</button>}
                    {!fileOnly && !readOnly && <>
                    <button type="button" role="menuitem" className={menuItem} disabled={mutationDisabled || Boolean(favoriteDisabledReason)} title={favoriteDisabledReason} onClick={() => { closeMenu(); onToggleFavorite() }}><Star className="size-4" aria-hidden="true" />{isFavorite ? translate("즐겨찾기 해제") : translate("즐겨찾기 추가")}</button>
                    <button type="button" role="menuitem" className={menuItem} onClick={() => { closeMenu(); onOpenFolder() }}><FolderOpen className="size-4" aria-hidden="true" />{translate("파일 위치 열기")}</button>
                    <button
                        type="button"
                        role="menuitem"
                        className={menuItem}
                        onClick={() => {
                            closeMenu()
                            onEdit('title')
                        }}
                    >
                        <Pencil className="size-4" aria-hidden="true" />{translate("제목 편집")}</button>
                    <button
                        type="button"
                        role="menuitem"
                        className={menuItem}
                        onClick={() => {
                            closeMenu()
                            onEdit('tags')
                        }}
                    >
                        <Tags className="size-4" aria-hidden="true" />{translate("태그 편집")}</button>
                    <button
                        type="button"
                        role="menuitem"
                        className={menuItem + ' border-t border-[var(--border-line)] text-red-500'}
                        disabled={mutationDisabled}
                        onClick={() => {
                            closeMenu(true)
                            onUnregister()
                        }}
                    >
                        <Trash2 className="size-4" aria-hidden="true" />{translate("등록 해제")}</button>
                    </>}
                </div>
            )}
        </div>
    )
}
