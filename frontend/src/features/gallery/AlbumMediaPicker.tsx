import { translateMessage, translateError, getLocale, translate, useLanguage } from '@renderer/shared/i18n'
import { useCallback, useEffect, useRef, useState } from 'react'
import { getApi } from '@renderer/shared/api'
import { MediaPreview } from '@renderer/shared/components/media/MediaPreview'
import { useInfiniteScroll } from '@renderer/shared/hooks/useInfiniteScroll'
import type { MediaFileDto, MediaSummaryDto } from '@contracts/media-v2'
import { ActionButton } from '@renderer/shared/components/ui/ActionButton'
import { CheckCheck, ListChecks, ArrowLeft, Search, RefreshCw, X, ListPlus } from 'lucide-react'

const PAGE_SIZE = 50
const MAX_SELECTION = 20000
const emptyPage = () => ({ groups: [] as MediaSummaryDto[], files: [] as MediaFileDto[], loaded: false, loading: true, hasNext: false, error: '' })
const unique = <T extends { id: number },>(items: T[]) => [...new Map(items.map(item => [item.id, item])).values()]

export function AlbumMediaPicker({ album, disabled, onAdd }: {
    album: MediaSummaryDto
    disabled: boolean
    onAdd: (fileIds: number[]) => Promise<boolean>
}) {
    useLanguage()
    const api = getApi().media
    const [draft, setDraft] = useState(''), [keyword, setKeyword] = useState('')
    const [source, setSource] = useState<MediaSummaryDto | null>(null)
    const [page, setPage] = useState(emptyPage)
    const [selected, setSelected] = useState<Set<number>>(() => new Set())
    const [members, setMembers] = useState<Set<number> | null>(null)
    const [memberError, setMemberError] = useState(''), [memberAttempt, setMemberAttempt] = useState(0)
    const [groupFiles, setGroupFiles] = useState<Map<number, number[]>>(() => new Map())
    const [selectingId, setSelectingId] = useState<number | null>(null), [selectionError, setSelectionError] = useState('')
    const selecting = useRef(false), selectionRequest = useRef(0)
    const root = useRef<HTMLDivElement>(null), sentinel = useRef<HTMLDivElement>(null)
    const generation = useRef(0), pending = useRef(false), adding = useRef(false), offset = useRef(0)
    const sourceRevision = useRef<number | null>(null)
    const scope = useRef({ source, keyword })

    useEffect(() => {
        let active = true
        setMembers(null); setMemberError('')
        void (async () => {
            try {
                // Fetch IDs only, including members beyond the album's first file page.
                const result = await api.sequence({ mediaId: album.id })
                if (!active) return
                if (!result.ok) throw new Error(translateError(result.error))
                if (result.data.revision !== album.revision) throw new Error(translate("gallery.theAlbumHasChangedGoBackAndRefresh"))
                setMembers(new Set(result.data.ids))
            } catch (error) {
                if (active) setMemberError(error instanceof Error ? error.message : translate("gallery.couldNotCheckFilesInTheAlbum"))
            }
        })()
        return () => { active = false }
    }, [api, album.id, album.revision, memberAttempt])

    const load = useCallback(async (append = false) => {
        if (pending.current) return
        const own = generation.current, current = scope.current, start = append ? offset.current : 0
        pending.current = true
        setPage(previous => ({ ...previous, loading: true, error: '' }))
        try {
            if (current.source) {
                const result = await api.get({ mediaId: current.source.id, fileLimit: PAGE_SIZE, fileOffset: start })
                if (own !== generation.current) return
                if (!result.ok) throw new Error(translateError(result.error))
                if (append && result.data.revision !== sourceRevision.current) throw new Error(translate("gallery.theListHasChangedGoBackToTheList"))
                const rows = result.data.files.data
                if (!rows.length && result.data.files.hasNextPage) throw new Error(translate("gallery.couldNotLoadFilesTryAgain"))
                sourceRevision.current = result.data.revision
                offset.current = start + rows.length
                setPage(previous => ({ ...previous, files: unique(append ? [...previous.files, ...rows] : rows), loaded: true, hasNext: result.data.files.hasNextPage }))
            } else {
                const result = await api.list({ limit: PAGE_SIZE, offset: start, filter: { keyword: current.keyword } })
                if (own !== generation.current) return
                if (!result.ok) throw new Error(translateError(result.error))
                const rows = result.data.data
                if (!rows.length && result.data.hasNextPage) throw new Error(translate("gallery.couldNotLoadMediaTryAgain"))
                // Advance by the raw count, even when the current album is excluded.
                offset.current = (result.data.offset ?? start) + rows.length
                setPage(previous => ({ ...previous, groups: unique(append ? [...previous.groups, ...rows] : rows), loaded: true, hasNext: result.data.hasNextPage }))
            }
        } catch (error) {
            if (own === generation.current) setPage(previous => ({ ...previous, error: error instanceof Error ? error.message : translate("gallery.couldNotLoadMedia") }))
        } finally {
            if (own === generation.current) {
                pending.current = false
                setPage(previous => ({ ...previous, loading: false }))
            }
        }
    }, [api])

    useEffect(() => {
        generation.current++; pending.current = false; offset.current = 0; sourceRevision.current = null
        selectionRequest.current++; selecting.current = false; setSelectingId(null); setSelectionError('')
        scope.current = { source, keyword }
        setPage(emptyPage())
        if (root.current) root.current.scrollTop = 0
        void load()
        return () => { generation.current++; selectionRequest.current++; pending.current = false }
    }, [load, source, keyword])

    const loadMore = useCallback(() => load(true), [load])
    useInfiniteScroll({ rootRef: root, targetRef: sentinel, enabled: page.hasNext && !page.loading && !page.error && !disabled, onLoadMore: loadMore })

    const select = (id: number, checked: boolean) => {
        if (disabled || selecting.current || !members || members.has(id)) return
        setSelected(previous => {
            const next = new Set(previous)
            if (checked && next.size < MAX_SELECTION) next.add(id)
            else if (!checked) next.delete(id)
            return next
        })
    }
    async function selectGroup(item: MediaSummaryDto, checked = true) {
        if (disabled || !members || selecting.current || adding.current) return
        const own = ++selectionRequest.current
        selecting.current = true; setSelectingId(item.id); setSelectionError('')
        try {
            // IDs include every file, even those beyond the currently loaded page.
            const result = await api.sequence({ mediaId: item.id })
            if (own !== selectionRequest.current) return
            if (!result.ok) throw new Error(translateError(result.error))
            if (result.data.revision !== item.revision) throw new Error(translate("gallery.theMediaHasChangedSearchOrReopenTheList"))
            const ids = [...new Set(result.data.ids)].filter(id => !members.has(id))
            const next = new Set(selected)
            for (const id of ids) { if (checked) next.add(id); else next.delete(id) }
            if (next.size > MAX_SELECTION) throw new Error(translate("gallery.youCanSelectUpTo20000FilesAt"))
            setGroupFiles(previous => new Map(previous).set(item.id, ids))
            setSelected(next)
        } catch (error) {
            if (own === selectionRequest.current) setSelectionError(error instanceof Error ? error.message : translate("gallery.couldNotSelectFiles"))
        } finally {
            if (own === selectionRequest.current) { selecting.current = false; setSelectingId(null) }
        }
    }
    const groups = page.groups.filter(item => item.id !== album.id && item.fileCount > 0)
    return <section aria-label={translate("gallery.selectMediaToAdd")} className="space-y-3">
        <p className="text-sm"><strong>{album.title}</strong>{translate("gallery.selectFilesToAddSelectASingleFileDirectly")}</p>
        <form className="flex gap-2" onSubmit={event => {
            event.preventDefault()
            if (disabled) return
            const value = draft.trim()
            if (!source && value === keyword) void load()
            else { setSource(null); setKeyword(value) }
        }}>
            <input autoFocus aria-label={translate("search.media")} placeholder={translate("gallery.searchFolderAlbumOrFileNames")} maxLength={2000} className="min-w-0 flex-1 rounded-lg border border-[var(--border-line)] bg-[var(--bg-app)] p-2" value={draft} disabled={disabled} onChange={event => setDraft(event.target.value)} />
            <ActionButton type="submit" icon={<Search />} label={translate("navigation.search")} disabled={disabled} />
        </form>
        {source ? <div className="flex flex-wrap items-center gap-2">
            <ActionButton icon={<ArrowLeft />} label={translate("gallery.backToList")} disabled={disabled} onClick={() => setSource(null)} />
            <h3 className="min-w-0 flex-1 break-words text-sm font-semibold">{source.title}</h3>
            <ActionButton icon={<CheckCheck />} label={translate("gallery.selectAll")} disabled={disabled || !members || selectingId !== null} onClick={() => void selectGroup({ ...source, revision: sourceRevision.current ?? source.revision })} />
            <ActionButton icon={<ListChecks />} label={translate("gallery.selectLoadedFiles")} disabled={disabled || selectingId !== null || !members || !page.files.some(file => !members.has(file.id) && !selected.has(file.id)) || selected.size >= MAX_SELECTION} onClick={() => setSelected(previous => {
                const next = new Set(previous)
                for (const file of page.files) if (!members?.has(file.id) && next.size < MAX_SELECTION) next.add(file.id)
                return next
            })} />
        </div> : <p className="text-xs text-[var(--text-muted)]">{translate("gallery.selectingTheEntireCollectionIncludesFilesNotLoadedYet")}</p>}
        {selectionError && <p role="alert" className="text-sm text-red-500">{translateMessage(selectionError)}</p>}
        {selectingId !== null && <p role="status" className="text-sm">{translate("gallery.checkingAllFilesInTheCollection")}</p>}
        {memberError && <div role="alert" className="text-sm text-red-500"><p>{translateMessage(memberError)}</p><ActionButton icon={<RefreshCw />} label={translate("gallery.checkMembershipAgain")} disabled={disabled} onClick={() => setMemberAttempt(value => value + 1)} /></div>}
        {!members && !memberError && <p role="status" className="text-sm">{translate("gallery.checkingFilesAlreadyInTheAlbum")}</p>}
        <div ref={root} className="max-h-[45dvh] min-h-32 overflow-y-auto overscroll-contain rounded-lg border border-[var(--border-line)] p-2" aria-label={translate("gallery.mediaSearchResults")}>
            {source ? <div className="grid grid-cols-2 gap-2 sm:grid-cols-3">{page.files.map(file => {
                const included = members?.has(file.id) ?? false, checked = selected.has(file.id), title = file.title || file.relativePath || file.fileKey || translate("gallery.file", { fileId: file.id })
                return <label key={file.id} className={'relative flex flex-col gap-2 rounded-lg border p-2 ' + (checked ? 'border-[var(--color-primary)]' : 'border-[var(--border-line)]')}>
                    <MediaPreview path={file.thumbnailPath ?? file.previewPath ?? (file.kind === 'image' ? file.filepath : null)} title={title} className="aspect-square w-full rounded bg-[var(--bg-active)] object-contain" />
                    <span className="flex items-start gap-2 text-sm"><input type="checkbox" aria-label={translate("gallery.select", { title: title })} checked={included || checked} disabled={disabled || selectingId !== null || !members || included || !checked && selected.size >= MAX_SELECTION} onChange={event => select(file.id, event.target.checked)} /><span className="min-w-0 break-words">{title}</span></span>
                    <span className="text-xs text-[var(--text-muted)]">{included ? translate("gallery.alreadyIncluded") : file.kind === 'video' ? translate("media.videoTitle") : file.kind === 'audio' ? translate("media.audio") : file.kind === 'image' ? translate("gallery.image") : translate("media.file")}</span>
                </label>
            })}</div> : <ul className="space-y-2">{groups.map(item => {
                const ids = groupFiles.get(item.id), included = ids?.length === 0
                const checked = included || Boolean(ids?.length && ids.every(id => selected.has(id)))
                return <li key={item.id} className="flex flex-wrap items-center gap-2 rounded-lg border border-[var(--border-line)] p-2">
                {item.fileCount === 1 && <input type="checkbox" aria-label={translate("gallery.select", { title: item.title })} checked={checked} disabled={disabled || !members || selectingId !== null || included || !checked && selected.size >= MAX_SELECTION} onChange={event => void selectGroup(item, event.target.checked)} />}
                <button type="button" className="flex min-w-0 flex-1 items-center gap-3 text-left disabled:opacity-40" disabled={disabled} aria-label={translate("gallery.viewFilesIn", { title: item.title })} onClick={() => setSource(item)}>
                    <MediaPreview path={item.cover?.path ?? null} title={item.title} className="size-14 shrink-0 rounded bg-[var(--bg-active)] object-contain" />
                    <span className="min-w-0 flex-1"><span className="block break-words text-sm">{item.title}</span><span className="text-xs text-[var(--text-muted)]">{translate("gallery.files", { kind: item.isUserAlbum ? translate("gallery.userAlbums") : translate("gallery.mediaCollection"), files: item.fileCount })}</span></span>
                    <span aria-hidden="true">›</span>
                </button>
                {item.fileCount > 1 && <ActionButton icon={<CheckCheck />} label={included ? translate("gallery.alreadyIncluded") : checked ? translate("gallery.deselectAll") : translate("gallery.selectAll")} aria-label={`${item.title} ${checked && !included ? translate("gallery.deselectAll") : translate("gallery.selectAll")}`} disabled={disabled || !members || selectingId !== null || included} onClick={() => void selectGroup(item, !checked)} />}
                {item.fileCount === 1 && included && <span className="text-xs text-[var(--text-muted)]">{translate("gallery.alreadyIncluded")}</span>}
            </li>})}</ul>}
            {!page.loading && !page.error && !(source ? page.files.length : groups.length) && <p className="p-4 text-center text-sm text-[var(--text-muted)]">{source ? translate("gallery.noFiles") : translate("gallery.noMediaToAdd")}</p>}
            {page.error && <div role="alert" className="space-y-2 p-2 text-sm text-red-500"><p>{translateMessage(page.error)}</p><ActionButton icon={<RefreshCw />} label={translate("common.retry")} disabled={disabled} onClick={() => void load(page.loaded)} /></div>}
            {page.loading && <p role="status" className="p-3 text-sm">{translate("gallery.loadingMedia")}</p>}
            <div ref={sentinel} aria-hidden="true" className="h-px" />
        </div>
        <footer className="flex flex-wrap items-center gap-2">
            <p role="status" className="mr-auto text-sm">{translate("gallery.selection.countWithLimit", { count: selected.size.toLocaleString(getLocale()), limitHint: selected.size >= MAX_SELECTION ? translate("gallery.youCanAddUpTo20000FilesAt") : '' })}</p>
            <ActionButton icon={<X />} label={translate("gallery.selection.clear")} disabled={disabled || selectingId !== null || !selected.size} onClick={() => setSelected(new Set())} />
            <ActionButton icon={<ListPlus />} label={translate("gallery.addSelectedMedia")} disabled={disabled || selectingId !== null || !members || !selected.size} onClick={async () => {
                if (adding.current) return
                adding.current = true
                try { await onAdd([...selected]) } finally { adding.current = false }
            }} />
        </footer>
    </section>
}
