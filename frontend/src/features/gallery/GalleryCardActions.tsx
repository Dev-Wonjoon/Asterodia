import { translate, useLanguage } from '@renderer/shared/i18n'
import { useEffect, useId, useRef, useState, useSyncExternalStore, type MouseEvent, type ReactNode } from 'react'
import { shortcutKeys, shortcutAria, formatShortcut, subscribeShortcuts, shortcutVersion } from '../../../../internal/webserver/web/shortcuts.js'
import { Ellipsis, FolderOpen, Pencil, Star, Pin, Tags, Trash2, Info, Play, ListPlus, ChevronDown, ChevronUp, Film, ListMinus } from 'lucide-react'
import { ActionContent } from '@renderer/shared/components/ui/ActionButton'

interface GalleryCardActionsProps {
    compact?: boolean
    keyboardShortcuts?: boolean
    primaryActions?: ReactNode
    title: string
    isFavorite: boolean
    isPinned?: boolean
    onTogglePin?: () => void
    disabled: boolean
    mutationDisabled: boolean
    favoriteDisabledReason?: string
    onToggleFavorite: () => void
    onOpenFolder: () => void
    onEdit: (field: 'title' | 'tags') => void
    onUnregister: () => void
    removalLabel?: string
    onView?: () => void
    onDetails?: () => void
    onAdd?: () => void
    addLabel?: string
    onEditAlbum?: () => void
    onRemoveFromAlbum?: () => void
    fileOnly?: boolean
    onToggleFiles?: (event: MouseEvent<HTMLButtonElement>) => void
    filesLabel?: string
    filesExpanded?: boolean
    onCreateAnimation?: () => void
    editorDisabled?: boolean
    readOnly?: boolean
}

export function GalleryCardActions({
    compact = false,
    keyboardShortcuts = false,
    primaryActions,
    title,
    isFavorite,
    isPinned = false,
    onTogglePin,
    disabled,
    mutationDisabled,
    favoriteDisabledReason,
    onToggleFavorite,
    onOpenFolder,
    onEdit,
    onUnregister, removalLabel = translate('gallery.deleteOrUnregister'), onView, onDetails, onAdd, addLabel = translate("gallery.album.add"), onEditAlbum, onRemoveFromAlbum, fileOnly = false, onToggleFiles, filesLabel, filesExpanded, onCreateAnimation, editorDisabled, readOnly = false
}: GalleryCardActionsProps): React.JSX.Element {
    useLanguage()
    useSyncExternalStore(subscribeShortcuts, shortcutVersion)
    const shortcutHint = (id: string) => keyboardShortcuts && shortcutKeys(id) ? { 'data-shortcut': formatShortcut(shortcutKeys(id)), 'aria-keyshortcuts': shortcutAria(shortcutKeys(id)) } : {}
    const [open, setOpen] = useState(false)
    const [opensUp, setOpensUp] = useState(true)
    const [menuHeight, setMenuHeight] = useState(420)
    const [menuShift, setMenuShift] = useState(0)
    const root = useRef<HTMLDivElement>(null)
    const trigger = useRef<HTMLButtonElement>(null)
    const menu = useRef<HTMLDivElement>(null)
    const menuId = useId()
    const starLabel = title + (isFavorite ? translate("gallery.removeFromFavorites") : translate("gallery.addToFavorites"))
    const iconButton =
        'flex min-h-9 min-w-9 px-2 items-center justify-center rounded-lg transition-colors hover:bg-[var(--bg-hover)] hover:text-[var(--text-main)] focus-visible:outline-2 focus-visible:outline-[var(--color-primary)] disabled:opacity-40 disabled:pointer-events-none'
    const menuItem =
        'keyboard-shortcut-hint flex w-full items-center gap-2 rounded px-3 py-2 text-left text-sm hover:bg-[var(--bg-hover)] focus:bg-[var(--bg-hover)] focus:outline-none disabled:opacity-40'

    function closeMenu(restore = false): void {
        setOpen(false)
        if (restore) trigger.current?.focus({ preventScroll: true })
    }
    function openMenu(toggle: boolean): void {
        const bounds = trigger.current?.getBoundingClientRect()
        const main = root.current?.closest('main')?.getBoundingClientRect()
        if (bounds) {
            const rootBounds = root.current!.getBoundingClientRect()
            const menuWidth = Math.min(keyboardShortcuts ? 288 : 192, window.innerWidth - 16)
            setMenuShift(Math.max(0, Math.max(8, (main?.left ?? 0) + 8) - (rootBounds.right - 8 - menuWidth)))
            const above = bounds.top - Math.max(main?.top ?? 0, 0)
            const below = Math.min(main?.bottom ?? window.innerHeight, window.innerHeight) - bounds.bottom
            const up = above > below
            setOpensUp(up)
            setMenuHeight(Math.max(100, Math.min(420, (up ? above : below) - 8)))
        }
        setOpen(previous => toggle ? !previous : true)
        if (!toggle) menu.current?.querySelector<HTMLButtonElement>('button:not(:disabled)')?.focus({ preventScroll: true })
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
        document.addEventListener('contextmenu', outside, true)
        return () => {
            document.removeEventListener('pointerdown', outside)
            document.removeEventListener('click', outside, true)
            document.removeEventListener('contextmenu', outside, true)
        }
    }, [open])

    return (
        <div
            ref={root}
            data-detail-trigger
            data-action-boundary
            className={compact ? 'gallery-card-utilities relative flex items-center justify-center text-sm [&>label]:hidden [&>button:not([data-card-menu])]:hidden' : 'gallery-card-utilities relative flex flex-nowrap items-center gap-0.5 border-t border-[var(--border-line)] px-2 py-1.5 text-sm'}
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
            {primaryActions}
            {!fileOnly && !readOnly && <button
                type="button"
                className={
                    iconButton + (isFavorite ? ' text-amber-500' : ' text-[var(--text-muted)]')
                }
                disabled={disabled || mutationDisabled || Boolean(favoriteDisabledReason)}
                aria-label={starLabel}
                aria-pressed={isFavorite}
                title={favoriteDisabledReason || (isFavorite ? translate("gallery.favorite.remove") : translate("gallery.favorite.add"))}
                onClick={() => {
                    closeMenu()
                    onToggleFavorite()
                }}
            >
                <ActionContent label={isFavorite ? translate("gallery.favorite.remove") : translate("gallery.favorite.add")} icon={<Star
                    className="size-[18px]"
                    fill={isFavorite ? 'currentColor' : 'none'}
                    aria-hidden="true"
                />} />
            </button>}
            {!readOnly && onTogglePin && <button type="button" className={iconButton + (isPinned ? ' text-[var(--color-primary)]' : ' text-[var(--text-muted)]')}
                disabled={disabled || mutationDisabled} aria-label={title + (isPinned ? translate("gallery.unpin") : translate("gallery.pin"))}
                aria-pressed={isPinned} title={isPinned ? translate("gallery.action.unpin") : translate("gallery.pinToTheTopOfTheGallery")}
                onClick={() => { closeMenu(); onTogglePin() }}>
                <ActionContent icon={<Pin className="size-[18px]" fill={isPinned ? 'currentColor' : 'none'} />} label={isPinned ? translate("gallery.action.unpin") : translate("gallery.pinToTheTopOfTheGallery")} />
            </button>}
            {!readOnly && <button type="button" className={iconButton + ' text-[var(--text-muted)]'}
                disabled={disabled || mutationDisabled} aria-label={title + translate("gallery.editTags")}
                title={fileOnly ? translate("gallery.editParentGroupTags") : translate("gallery.action.editTags")}
                onClick={() => { closeMenu(); onEdit('tags') }}>
                <ActionContent icon={<Tags className="size-[18px]" />} label={fileOnly ? translate("gallery.editParentGroupTags") : translate("gallery.action.editTags")} />
            </button>}
            {!readOnly && onAdd && <button type="button" className={iconButton + ' text-[var(--text-muted)]'}
                disabled={disabled || mutationDisabled} aria-label={title + ' ' + addLabel} title={addLabel}
                onClick={() => { closeMenu(); onAdd() }}>
                <ActionContent icon={<ListPlus className="size-[18px]" />} label={addLabel} />
            </button>}
            {onCreateAnimation && <button type="button" className={iconButton + ' text-[var(--text-muted)]'}
                disabled={disabled || mutationDisabled || editorDisabled} aria-label={title + translate("gallery.createGifOrWebp")} title={translate("gallery.editor.createAnimation")}
                onClick={() => { closeMenu(); onCreateAnimation() }}>
                <ActionContent icon={<Film className="size-[18px]" />} label={translate("gallery.editor.createAnimation")} />
            </button>}
            {!readOnly && <button
                type="button"
                className={iconButton + ' text-[var(--text-muted)]'}
                disabled={disabled}
                aria-label={title + translate("gallery.openFileLocation")}
                title={translate("gallery.openFolder")}
                onClick={() => {
                    closeMenu()
                    onOpenFolder()
                }}
            >
                <ActionContent icon={<FolderOpen className="size-[18px]" />} label={translate("gallery.openFolder")} />
            </button>}
            <button
                ref={trigger}
                data-card-menu
                type="button"
                className="flex size-9 shrink-0 items-center justify-center rounded-lg hover:bg-[var(--bg-hover)] focus-visible:outline-2 focus-visible:outline-[var(--color-primary)] disabled:opacity-40"
                disabled={disabled}
                aria-label={title + translate("gallery.more")}
                title={translate("gallery.action.more")}
                aria-haspopup="menu"
                aria-expanded={open}
                aria-controls={open ? menuId : undefined}
                onClick={() => openMenu(true)}
                onContextMenu={event => {
                    if (disabled) return
                    event.preventDefault()
                    event.stopPropagation()
                    openMenu(false)
                }}
            >
                <Ellipsis className="size-[18px]" aria-hidden="true" />
            </button>
            {open && (
                <div
                    ref={menu}
                    id={menuId}
                    role="menu"
                    aria-label={title + translate("gallery.cardMenu")}
                    style={{ maxHeight: menuHeight, right: 8 - menuShift, maxWidth: 'calc(100vw - 16px)' }}
                    className={
                        'motion-menu absolute right-2 z-30 ' + (keyboardShortcuts ? 'w-72 ' : 'w-48 ') + 'overflow-y-auto overscroll-contain rounded-lg border border-[var(--border-line)] bg-[var(--bg-sidebar)] p-1 shadow-xl ' +
                        (opensUp ? 'motion-menu-up bottom-full mb-1' : 'top-full mt-1')
                    }
                >
                    {onView && <button {...shortcutHint('gallery.view')} type="button" role="menuitem" className={menuItem} disabled={disabled} onClick={() => { closeMenu(); onView() }}><Play className="size-4" aria-hidden="true" />{translate("viewer.open")}</button>}
                    {onDetails && <button {...shortcutHint('gallery.details')} type="button" role="menuitem" className={menuItem} onClick={() => { closeMenu(); onDetails() }}><Info className="size-4" aria-hidden="true" />{translate("gallery.action.details")}</button>}
                    {onCreateAnimation && <button type="button" role="menuitem" className={menuItem} disabled={mutationDisabled || editorDisabled} onClick={() => { closeMenu(); onCreateAnimation() }}><Film className="size-4" aria-hidden="true" />{translate("gallery.editor.createAnimation")}</button>}
                    {onAdd && <button {...shortcutHint('gallery.addToAlbum')} type="button" role="menuitem" className={menuItem} disabled={mutationDisabled} onClick={() => { closeMenu(); onAdd() }}><ListPlus className="size-4" aria-hidden="true" />{translate("gallery.album.add")}</button>}
                    {!fileOnly && onToggleFiles && <button type="button" role="menuitem" className={menuItem} disabled={mutationDisabled} aria-expanded={filesExpanded} onClick={event => { closeMenu(); onToggleFiles(event) }}>{filesExpanded ? <ChevronUp className="size-4" aria-hidden="true" /> : <ChevronDown className="size-4" aria-hidden="true" />}{filesLabel}</button>}
                    {!fileOnly && onEditAlbum && <button type="button" role="menuitem" className={menuItem} disabled={mutationDisabled} onClick={() => { closeMenu(); onEditAlbum() }}><Pencil className="size-4" aria-hidden="true" />{translate("gallery.editAlbum")}</button>}
                    {fileOnly && !readOnly && <button type="button" role="menuitem" className={menuItem} onClick={() => { closeMenu(); onOpenFolder() }}><FolderOpen className="size-4" aria-hidden="true" />{translate("gallery.openFolder")}</button>}
                    {!readOnly && onRemoveFromAlbum && <button type="button" role="menuitem" className={menuItem} disabled={mutationDisabled} onClick={() => { closeMenu(); onRemoveFromAlbum() }}><ListMinus className="size-4" aria-hidden="true" />{translate("gallery.removeFromAlbum")}</button>}
                    {!fileOnly && !readOnly && <>
                    <button {...shortcutHint('gallery.favorite')} type="button" role="menuitem" className={menuItem} disabled={mutationDisabled || Boolean(favoriteDisabledReason)} title={favoriteDisabledReason} onClick={() => { closeMenu(); onToggleFavorite() }}><Star className="size-4" aria-hidden="true" />{isFavorite ? translate("gallery.favorite.remove") : translate("gallery.favorite.add")}</button>
                    <button {...shortcutHint('gallery.folder')} type="button" role="menuitem" className={menuItem} onClick={() => { closeMenu(); onOpenFolder() }}><FolderOpen className="size-4" aria-hidden="true" />{translate("gallery.openFolder")}</button>
                    <button
                        type="button"
                        role="menuitem"
                        className={menuItem}
                        onClick={() => {
                            closeMenu()
                            onEdit('title')
                        }}
                    >
                        <Pencil className="size-4" aria-hidden="true" />{translate("gallery.editTitle")}</button>
                    <button
                        {...shortcutHint('gallery.tags')}
                        type="button"
                        role="menuitem"
                        className={menuItem}
                        onClick={() => {
                            closeMenu()
                            onEdit('tags')
                        }}
                    >
                        <Tags className="size-4" aria-hidden="true" />{translate("gallery.action.editTags")}</button>
                    <button
                        {...shortcutHint('gallery.unregister')}
                        type="button"
                        role="menuitem"
                        className={menuItem + ' border-t border-[var(--border-line)] text-red-500'}
                        disabled={mutationDisabled}
                        onClick={() => {
                            closeMenu(true)
                            onUnregister()
                        }}
                    >
                        <Trash2 className="size-4" aria-hidden="true" />{removalLabel}</button>
                    </>}
                </div>
            )}
        </div>
    )
}
