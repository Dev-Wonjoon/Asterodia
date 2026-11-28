import { useEffect, useRef, useState, type DragEvent, type MouseEvent, type RefObject } from 'react'
import { translate } from '@renderer/shared/i18n'
import { isComposing } from '../../../../internal/webserver/web/shortcuts.js'
import type { MoveSelection } from '@contracts/file-move-api'
import type { MediaSummaryDto } from '@contracts/media-v2'
import type { GalleryGeometry } from './useVirtualGallery'

export const gallerySelectionKey = (item: MediaSummaryDto) => item.galleryFile ? `file:${item.galleryFile.id}` : `media:${item.id}`
export const galleryDisplayKey = (item: MediaSummaryDto) => `${item.id}:${gallerySelectionKey(item)}`
const displayKey = galleryDisplayKey
const target = (item: MediaSummaryDto): MoveSelection => ({ mediaId: item.id, revision: item.revision, ...(item.galleryFile ? { fileIds: [item.galleryFile.id] } : {}) })
export const albumDragType = 'application/x-asterodia-album-selection'
export type AlbumDragSelection = { fileIds: number[]; mediaIds: number[] }
type Modifiers = Pick<MouseEvent, 'ctrlKey' | 'metaKey' | 'shiftKey'>
type Rectangle = { left: number; top: number; width: number; height: number }
type Gesture = { id: number; x: number; y: number; clientX: number; clientY: number; base: Map<string, MoveSelection>; additive: boolean; active: boolean; fromCard: boolean }

export function useGallerySelection(items: MediaSummaryDto[], blocked: RefObject<boolean>, scrollRef: RefObject<HTMLDivElement | null>, resetKey: unknown, geometry?: RefObject<GalleryGeometry | null>) {
    const [entries, setEntries] = useState(new Map<string, MoveSelection>())
    const selected = useRef(entries)
    const gridRef = useRef<HTMLDivElement>(null)
    const anchor = useRef<string | null>(null)
    const rangeBase = useRef(new Map<string, MoveSelection>())
    const gesture = useRef<Gesture | null>(null)
    const suppressClick = useRef(false), suppressRepeatedClick = useRef(false)
    const frame = useRef(0)
    const [rectangle, setRectangle] = useState<Rectangle | null>(null)
    const [dragging, setDragging] = useState(false)
    const [dragBase, setDragBase] = useState<Map<string, MoveSelection> | null>(null)
    const [albumDrag, setAlbumDrag] = useState<AlbumDragSelection | null>(null)
    const albumDragRef = useRef<AlbumDragSelection | null>(null)
    const latest = useRef({ items }); latest.current = { items }
    function replace(next: Map<string, MoveSelection>) { selected.current = next; setEntries(next) }
    function clear() { anchor.current = null; rangeBase.current = new Map(); replace(new Map()) }
    function endAlbumDrag() { albumDragRef.current = null; setAlbumDrag(null); setDragBase(null) }
    function startAlbumDrag(item: MediaSummaryDto, event: DragEvent<HTMLElement>) {
        if (blocked.current || item.fileCount === 0) { event.preventDefault(); return }
        stop()
        // Keep the action bar's layout unchanged until the native drag ends.
        setDragBase(new Map(selected.current))
        const key = gallerySelectionKey(item)
        if (!selected.current.has(key)) {
            const next = new Map([[key, target(item)]])
            anchor.current = displayKey(item); rangeBase.current = new Map(next); replace(next)
        }
        const values = [...selected.current.values()]
        const drag = {
            fileIds: [...new Set(values.flatMap(value => value.fileIds ?? []))],
            mediaIds: [...new Set(values.filter(value => !value.fileIds).map(value => value.mediaId))],
        }
        event.dataTransfer.effectAllowed = 'copy'
        event.dataTransfer.setData(albumDragType, JSON.stringify(drag))
        // Use a compact count badge instead of a large card as the native drag image.
        const badge = document.createElement('div')
        badge.textContent = translate("gallery.collectionsIndividualFiles", { collections: drag.mediaIds.length, files: drag.fileIds.length })
        badge.style.cssText = 'position:fixed;top:-1000px;left:0;padding:10px 14px;border-radius:8px;background:#252532;color:white;font:14px sans-serif;white-space:nowrap'
        document.body.append(badge)
        event.dataTransfer.setDragImage(badge, 12, 12)
        setTimeout(() => badge.remove(), 0)
        suppressClick.current = true; suppressRepeatedClick.current = true
        albumDragRef.current = drag; setAlbumDrag(drag)
    }
    function select(item: MediaSummaryDto, event: Modifiers, toggle = false) {
        if (blocked.current) return
        const rows = latest.current.items, additive = event.ctrlKey || event.metaKey
        if (event.shiftKey && anchor.current) {
            const from = rows.findIndex(row => displayKey(row) === anchor.current), to = rows.findIndex(row => displayKey(row) === displayKey(item))
            if (from >= 0 && to >= 0) {
                const next = additive ? new Map(rangeBase.current) : new Map<string, MoveSelection>()
                for (const row of rows.slice(Math.min(from, to), Math.max(from, to) + 1)) next.set(gallerySelectionKey(row), target(row))
                replace(next); return
            }
        }
        const next = additive || toggle ? new Map(selected.current) : new Map<string, MoveSelection>()
        const key = gallerySelectionKey(item)
        if ((additive || toggle) && next.has(key)) next.delete(key)
        else next.set(key, target(item))
        anchor.current = displayKey(item); rangeBase.current = new Map(next); replace(next)
    }
    function all() {
        if (blocked.current) return
        anchor.current = null
        replace(new Map(latest.current.items.map(item => [gallerySelectionKey(item), target(item)])))
    }
    function removeMedia(ids: number[], includeFiles = false) {
        replace(new Map([...selected.current].filter(([, value]) => (!includeFiles && value.fileIds) || !ids.includes(value.mediaId))))
        anchor.current = null
    }
    function stop(cancel = false) {
        const own = gesture.current
        if (cancel && own) replace(own.base)
        gesture.current = null
        if (own) { try { scrollRef.current?.releasePointerCapture(own.id) } catch { /* capture may already be released */ } }
        cancelAnimationFrame(frame.current); setDragging(false); setDragBase(null); setRectangle(null)
    }
    function updateRectangle() {
        const own = gesture.current, root = scrollRef.current, grid = gridRef.current
        if (!own || !root || !grid) return
        const startX = own.x - root.scrollLeft, startY = own.y - root.scrollTop
        const box = { left: Math.min(startX, own.clientX), top: Math.min(startY, own.clientY), width: Math.abs(startX - own.clientX), height: Math.abs(startY - own.clientY) }
        setRectangle(box)
        const next = own.additive ? new Map(own.base) : new Map<string, MoveSelection>()
        const origin = grid.getBoundingClientRect()
        const rows = geometry?.current ? null : new Map(latest.current.items.map(item => [gallerySelectionKey(item), item]))
        const candidates = geometry?.current
            ? latest.current.items.map(item => ({ item, bounds: geometry.current!.bounds(displayKey(item), origin) }))
            : [...grid.querySelectorAll<HTMLElement>('[data-selection-key]')].map(card => ({ item: rows!.get(card.dataset.selectionKey!), bounds: card.getBoundingClientRect() }))
        for (const { item, bounds } of candidates) {
            if (!bounds) continue
            if (item && bounds.width > 0 && bounds.height > 0 && bounds.left < box.left + box.width && bounds.right > box.left && bounds.top < box.top + box.height && bounds.bottom > box.top) next.set(gallerySelectionKey(item), target(item))
        }
        anchor.current = null; replace(next)
    }
    function autoScroll() {
        const own = gesture.current, root = scrollRef.current
        if (!own?.active || !root) return
        if (blocked.current) { stop(true); return }
        const bounds = root.getBoundingClientRect(), previous = root.scrollTop
        if (own.clientY < bounds.top + 36) root.scrollTop = Math.max(0, previous - 14)
        else if (own.clientY > bounds.bottom - 36) root.scrollTop = Math.min(root.scrollHeight - root.clientHeight, previous + 14)
        if (root.scrollTop !== previous) updateRectangle()
        frame.current = requestAnimationFrame(autoScroll)
    }
    function onPointerDown(event: globalThis.PointerEvent) {
        if (event.button !== 0 || (event.pointerType && event.pointerType !== 'mouse')) return
        suppressClick.current = false
        if (blocked.current || event.defaultPrevented || !(event.target instanceof Element) || event.target.closest('input,select,textarea,a,label,[contenteditable="true"],[data-action-boundary]')) return
        const button = event.target.closest('button'), card = event.target.closest('[data-selection-key]')
        if (card?.hasAttribute('data-album-draggable')) return
        if (button && !button.hasAttribute('data-card-select') || event.target.closest('article') && !card) return
        if (card && !gridRef.current?.contains(card)) return
        const root = scrollRef.current
        if (!root || !root.contains(event.target)) return
        const bounds = root.getBoundingClientRect()
        if (root.clientWidth > 0 && (event.clientX < bounds.left + root.clientLeft || event.clientX >= bounds.left + root.clientLeft + root.clientWidth)) return
        if (root.clientHeight > 0 && (event.clientY < bounds.top + root.clientTop || event.clientY >= bounds.top + root.clientTop + root.clientHeight)) return
        gesture.current = { id: event.pointerId, x: event.clientX + root.scrollLeft, y: event.clientY + root.scrollTop, clientX: event.clientX, clientY: event.clientY, base: new Map(selected.current), additive: event.ctrlKey || event.metaKey, active: false, fromCard: Boolean(card) }
    }
    useEffect(() => {
        clear(); stop(); endAlbumDrag()
    }, [resetKey])
    // Keep revision snapshots for unchanged selections, and reject stale visible rows.
    useEffect(() => {
        const revisions = new Map(items.map(item => [item.id, item.revision]))
        const next = new Map([...selected.current].filter(([, value]) => !revisions.has(value.mediaId) || revisions.get(value.mediaId) === value.revision))
        if (next.size !== selected.current.size) { stop(); endAlbumDrag(); replace(next); anchor.current = null }
    }, [items])
    useEffect(() => {
        if (blocked.current && gesture.current) stop(true)
        if (blocked.current && albumDragRef.current) endAlbumDrag()
    })
    useEffect(() => {
        function move(event: globalThis.PointerEvent) {
            const own = gesture.current
            if (!own || event.pointerId !== own.id || blocked.current) return
            own.clientX = event.clientX; own.clientY = event.clientY
            const root = scrollRef.current!
            if (!own.active && Math.hypot(own.x - root.scrollLeft - event.clientX, own.y - root.scrollTop - event.clientY) < 6) return
            if (!own.active) {
                own.active = true; suppressClick.current = true; suppressRepeatedClick.current = true
                root.focus({ preventScroll: true })
                try { root.setPointerCapture?.(own.id) } catch { /* native capture can be unavailable in synthetic events */ }
                setDragBase(own.base); setDragging(true); frame.current = requestAnimationFrame(autoScroll)
            }
            event.preventDefault(); updateRectangle()
        }
        function up(event: globalThis.PointerEvent) {
            const own = gesture.current
            if (!own || event.pointerId !== own.id) return
            if (own.active) { own.clientX = event.clientX; own.clientY = event.clientY; updateRectangle() }
            else if (!own.fromCard && !own.additive) clear()
            stop()
        }
        function click(event: globalThis.MouseEvent) {
            const root = scrollRef.current
            if (!event.detail || !(event.target instanceof Node) || !root?.contains(event.target)) return
            if (suppressClick.current || suppressRepeatedClick.current && event.detail > 1) {
                event.preventDefault(); event.stopImmediatePropagation(); return
            }
            if (event.type === 'click' && event.detail === 1) suppressRepeatedClick.current = false
        }
        // Native HTML dragging ends the pointer stream with pointercancel after
        // dragstart. Only cancel a marquee here; the album drag is still active.
        function cancelPointer() { if (gesture.current) stop(true) }
        function lost(event: globalThis.PointerEvent) { if (gesture.current?.active && gesture.current.id === event.pointerId && event.target === scrollRef.current) stop(true) }
        function escape(event: KeyboardEvent) {
            if (event.defaultPrevented || isComposing(event)) return
            if (event.key === 'Escape' && albumDragRef.current) { event.preventDefault(); event.stopPropagation(); endAlbumDrag(); return }
            if (event.key !== 'Escape' || blocked.current || document.querySelector('dialog[open],[aria-modal="true"],[data-confirm-dialog],[role="menu"],[role="listbox"],[data-keyboard-detail]') || (event.target instanceof Element && event.target.closest('input:not([type="checkbox"]),textarea,select,[contenteditable="true"]'))) return
            if (gesture.current || selected.current.size) {
                event.preventDefault(); event.stopPropagation()
                if (gesture.current) stop(true); else clear()
            }
        }
        window.addEventListener('pointermove', move, { passive: false }); window.addEventListener('pointerup', up)
        // Windows native dragging can temporarily move focus away from the
        // WebView. Native dragend also covers a drop outside the gallery.
        window.addEventListener('pointercancel', cancelPointer); window.addEventListener('blur', cancelPointer); window.addEventListener('keydown', escape, true)
        window.addEventListener('dragend', endAlbumDrag)
        window.addEventListener('lostpointercapture', lost)
        // Resolve the current scroll pane for each gesture, including after a
        // layout refresh replaces its DOM node. Only events inside it can start selection.
        window.addEventListener('pointerdown', onPointerDown)
        window.addEventListener('click', click, true); window.addEventListener('dblclick', click, true)
        return () => {
            cancelAnimationFrame(frame.current); gesture.current = null
            window.removeEventListener('pointermove', move); window.removeEventListener('pointerup', up)
            window.removeEventListener('pointercancel', cancelPointer); window.removeEventListener('blur', cancelPointer); window.removeEventListener('keydown', escape, true)
            window.removeEventListener('dragend', endAlbumDrag)
            window.removeEventListener('lostpointercapture', lost)
            window.removeEventListener('pointerdown', onPointerDown)
            window.removeEventListener('click', click, true); window.removeEventListener('dblclick', click, true)
        }
    }, [])
    // Keep the action bar's layout fixed while previews change under a marquee.
    const committed = (dragging || albumDrag) && dragBase ? dragBase : entries
    const groups = [...committed.values()].filter(value => !value.fileIds)
    const fileSelections = [...committed.values()].filter(value => value.fileIds)
    return { entries, groups, fileSelections, fileIds: fileSelections.flatMap(value => value.fileIds!), gridRef, rectangle, dragging, select, clear, all, removeMedia, albumDrag, getAlbumDrag: () => albumDragRef.current, startAlbumDrag, endAlbumDrag }
}
