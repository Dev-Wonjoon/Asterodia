import { useEffect, useRef, useState, type DragEvent } from 'react'
import { FolderPlus, RefreshCw, Search } from 'lucide-react'
import type { MediaSummaryDto } from '@contracts/media-v2'
import { getApi } from '@renderer/shared/api'
import { translate, translateError, translateMessage, useLanguage } from '@renderer/shared/i18n'
import { ActionButton } from '@renderer/shared/components/ui/ActionButton'
import { albumDragType, type AlbumDragSelection } from './useGallerySelection'

export function AlbumDropPanel({ drag, disabled, refreshKey, onDragEnd, onBusyChange, onChanged }: {
    drag: AlbumDragSelection | null
    disabled: boolean
    refreshKey: number
    onDragEnd(): void
    onBusyChange(busy: boolean): void
    onChanged(): void
}) {
    useLanguage()
    const api = getApi().media
    const [albums, setAlbums] = useState<MediaSummaryDto[]>([])
    const [keyword, setKeyword] = useState(''), query = useRef('')
    const [hasMore, setHasMore] = useState(false), [loading, setLoading] = useState(true)
    const [busy, setBusy] = useState(false), saving = useRef(false)
    const [loadError, setLoadError] = useState(''), [saveError, setSaveError] = useState(''), [notice, setNotice] = useState('')
    const [hovered, setHovered] = useState<number | null>(null)
    const mounted = useRef(false), request = useRef(0), offset = useRef(0)

    async function load(search: string, append = false) {
        const own = ++request.current
        setLoading(true); setLoadError(''); setHovered(null)
        try {
            const result = await api.albums({ keyword: search, limit: 50, offset: append ? offset.current : 0 })
            if (!mounted.current || own !== request.current) return
            if (!result.ok) throw new Error(translateError(result.error))
            const page = result.data
            offset.current = (append ? offset.current : 0) + page.data.length
            setAlbums(previous => [...new Map((append ? [...previous, ...page.data] : page.data).map(album => [album.id, album])).values()])
            setHasMore(page.hasNextPage)
        } catch (error) {
            if (mounted.current && own === request.current) setLoadError(error instanceof Error ? error.message : translate("앨범을 불러오지 못했습니다."))
        } finally {
            if (mounted.current && own === request.current) setLoading(false)
        }
    }

    useEffect(() => {
        mounted.current = true
        void load(query.current)
        return () => { mounted.current = false; request.current++ }
    }, [api, refreshKey])
    useEffect(() => { if (!drag) setHovered(null) }, [drag])

    function accepts(album: MediaSummaryDto, event: DragEvent) {
        return Boolean(drag && !disabled && !saving.current && !loading && !loadError &&
            !drag.mediaIds.includes(album.id) && event.dataTransfer.types.includes(albumDragType))
    }
    async function drop(album: MediaSummaryDto, event: DragEvent) {
        event.preventDefault(); event.stopPropagation(); setHovered(null)
        if (!accepts(album, event) || !drag) return
        // Capture the session before dragend or a busy-state render clears it.
        const selection = drag
        saving.current = true; setBusy(true); setSaveError(''); setNotice('')
        onDragEnd(); onBusyChange(true)
        try {
            const result = await api.saveAlbum({
                albumId: album.id, expectedRevision: album.revision,
                addFileIds: selection.fileIds, addMediaIds: selection.mediaIds,
            })
            if (!mounted.current) return
            if (!result.ok) throw new Error(translateError(result.error))
            setAlbums(previous => previous.map(value => value.id === album.id ? result.data : value))
            setNotice(translate("‘{0}’ 앨범에 추가했습니다.", { "0": album.title }))
            onChanged()
        } catch (error) {
            if (mounted.current) setSaveError(error instanceof Error ? error.message : translate("앨범 변경을 저장하지 못했습니다. 새로고침 후 확인하세요."))
        } finally {
            saving.current = false
            if (mounted.current) { setBusy(false); onBusyChange(false) }
        }
    }

    return <aside data-action-boundary data-detail-trigger aria-label={translate("드래그해서 앨범에 추가")}
        onDrop={event => event.preventDefault()}
        className="sticky top-24 z-20 w-full shrink-0 space-y-3 rounded-xl border border-[var(--border-line)] bg-[var(--bg-app)] p-3 lg:w-56">
        <header className="flex items-center gap-2">
            <FolderPlus aria-hidden="true" className="size-4 shrink-0" />
            <h2 className="min-w-0 flex-1 text-sm font-semibold">{translate("앨범에 놓기")}</h2>
            <ActionButton icon={<RefreshCw />} label={translate("드롭할 앨범 새로고침")} disabled={disabled || busy || loading} onClick={() => void load(query.current)} />
        </header>
        <p className="text-xs text-[var(--text-muted)]">{translate("파일을 하나 또는 여러 개 선택해 앨범 위에 놓으세요. 원본은 유지됩니다.")}</p>
        <form className="flex gap-1" onSubmit={event => { event.preventDefault(); if (disabled || busy || loading) return; query.current = keyword; void load(keyword) }}>
            <input aria-label={translate("드롭할 앨범 검색")} placeholder={translate("앨범 검색")} value={keyword} disabled={disabled || busy}
                onChange={event => setKeyword(event.target.value)} className="min-w-0 flex-1 rounded-lg border border-[var(--border-line)] bg-[var(--bg-app)] px-2 py-1 text-sm" />
            <ActionButton type="submit" icon={<Search />} label={translate("드롭할 앨범 찾기")} disabled={disabled || busy || loading} />
        </form>
        {drag && <p role="status" className="text-xs font-semibold text-[var(--color-primary)]">{translate("묶음 {0}개 · 개별 파일 {1}개", { "0": drag.mediaIds.length, "1": drag.fileIds.length })}</p>}
        {loadError && <p role="alert" className="text-xs text-red-500">{translateMessage(loadError)}</p>}
        {saveError && <p role="alert" className="text-xs text-red-500">{translateMessage(saveError)} {translate("앨범 목록을 새로고침한 뒤 다시 놓아 주세요.")}</p>}
        {notice && <p role="status" className="text-xs">{translateMessage(notice)}</p>}
        <ul className="flex max-h-40 gap-2 overflow-auto lg:max-h-[45dvh] lg:flex-col">
            {albums.map(album => {
                const available = Boolean(drag && !disabled && !busy && !loading && !loadError && !drag.mediaIds.includes(album.id))
                return <li key={album.id} data-album-drop-id={album.id} data-drop-active={hovered === album.id || undefined}
                    onDragOver={event => {
                        if (!accepts(album, event)) { event.dataTransfer.dropEffect = 'none'; return }
                        event.preventDefault(); event.dataTransfer.dropEffect = 'copy'; setHovered(album.id)
                    }}
                    onDragLeave={event => { if (!(event.relatedTarget instanceof Node) || !event.currentTarget.contains(event.relatedTarget)) setHovered(previous => previous === album.id ? null : previous) }}
                    onDrop={event => { void drop(album, event) }}
                    className={'min-w-32 shrink-0 rounded-lg border p-3 text-sm lg:min-w-0 ' + (hovered === album.id ? 'border-[var(--color-primary)] bg-[var(--bg-active)] ring-2 ring-[var(--color-primary)]' : available ? 'border-dashed border-[var(--color-primary)]' : 'border-[var(--border-line)]') + (drag && !available ? ' opacity-40' : '')}>
                    <p className="max-w-44 truncate font-medium" title={album.title}>{album.title}</p>
                    <p className="text-xs text-[var(--text-muted)]">{translate("파일 {0}개", { "0": album.fileCount })}</p>
                </li>
            })}
        </ul>
        {loading && <p className="text-xs text-[var(--text-muted)]">{translate("불러오는 중…")}</p>}
        {busy && <p role="status" className="text-xs">{translate("처리 중…")}</p>}
        {!loading && !loadError && !albums.length && <p className="text-xs text-[var(--text-muted)]">{query.current ? translate("표시할 앨범이 없습니다.") : translate("사용자 앨범이 없습니다. ‘새 앨범’으로 먼저 만들어 주세요.")}</p>}
        {hasMore && <button type="button" disabled={disabled || busy || loading} onClick={() => void load(query.current, true)} className="text-xs underline disabled:opacity-40">{translate("앨범 더 불러오기")}</button>}
    </aside>
}
