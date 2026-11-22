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
                if (!result.ok) throw new Error(result.error.message)
                if (result.data.revision !== album.revision) throw new Error('앨범이 변경되었습니다. 돌아가서 새로고침해 주세요.')
                setMembers(new Set(result.data.ids))
            } catch (error) {
                if (active) setMemberError(error instanceof Error ? error.message : '앨범에 포함된 파일을 확인하지 못했습니다.')
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
                if (!result.ok) throw new Error(result.error.message)
                if (append && result.data.revision !== sourceRevision.current) throw new Error('목록이 변경되었습니다. 목록으로 돌아가 다시 열어 주세요.')
                const rows = result.data.files.data
                if (!rows.length && result.data.files.hasNextPage) throw new Error('파일 목록을 불러오지 못했습니다. 다시 시도해 주세요.')
                sourceRevision.current = result.data.revision
                offset.current = start + rows.length
                setPage(previous => ({ ...previous, files: unique(append ? [...previous.files, ...rows] : rows), loaded: true, hasNext: result.data.files.hasNextPage }))
            } else {
                const result = await api.list({ limit: PAGE_SIZE, offset: start, filter: { keyword: current.keyword } })
                if (own !== generation.current) return
                if (!result.ok) throw new Error(result.error.message)
                const rows = result.data.data
                if (!rows.length && result.data.hasNextPage) throw new Error('미디어 목록을 불러오지 못했습니다. 다시 시도해 주세요.')
                // Advance by the raw count, even when the current album is excluded.
                offset.current = (result.data.offset ?? start) + rows.length
                setPage(previous => ({ ...previous, groups: unique(append ? [...previous.groups, ...rows] : rows), loaded: true, hasNext: result.data.hasNextPage }))
            }
        } catch (error) {
            if (own === generation.current) setPage(previous => ({ ...previous, error: error instanceof Error ? error.message : '미디어를 불러오지 못했습니다.' }))
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
            if (!result.ok) throw new Error(result.error.message)
            if (result.data.revision !== item.revision) throw new Error('미디어가 변경되었습니다. 검색하거나 목록을 다시 열어 주세요.')
            const ids = [...new Set(result.data.ids)].filter(id => !members.has(id))
            const next = new Set(selected)
            for (const id of ids) { if (checked) next.add(id); else next.delete(id) }
            if (next.size > MAX_SELECTION) throw new Error('한 번에 최대 20,000개까지 선택할 수 있습니다. 개별 파일을 선택해 주세요.')
            setGroupFiles(previous => new Map(previous).set(item.id, ids))
            setSelected(next)
        } catch (error) {
            if (own === selectionRequest.current) setSelectionError(error instanceof Error ? error.message : '파일 선택에 실패했습니다.')
        } finally {
            if (own === selectionRequest.current) { selecting.current = false; setSelectingId(null) }
        }
    }
    const groups = page.groups.filter(item => item.id !== album.id && item.fileCount > 0)
    return <section aria-label="추가할 미디어 선택" className="space-y-3">
        <p className="text-sm"><strong>{album.title}</strong>에 추가할 파일을 선택하세요. 파일 1개는 바로 선택하고, 여러 개는 전체 선택하거나 열어서 고를 수 있습니다.</p>
        <form className="flex gap-2" onSubmit={event => {
            event.preventDefault()
            if (disabled) return
            const value = draft.trim()
            if (!source && value === keyword) void load()
            else { setSource(null); setKeyword(value) }
        }}>
            <input autoFocus aria-label="미디어 검색" placeholder="폴더·앨범·파일 이름 검색" maxLength={2000} className="min-w-0 flex-1 rounded-lg border border-[var(--border-line)] bg-[var(--bg-app)] p-2" value={draft} disabled={disabled} onChange={event => setDraft(event.target.value)} />
            <ActionButton type="submit" icon={<Search />} label="검색" disabled={disabled} />
        </form>
        {source ? <div className="flex flex-wrap items-center gap-2">
            <ActionButton icon={<ArrowLeft />} label="목록으로" disabled={disabled} onClick={() => setSource(null)} />
            <h3 className="min-w-0 flex-1 break-words text-sm font-semibold">{source.title}</h3>
            <ActionButton icon={<CheckCheck />} label="전체 선택" disabled={disabled || !members || selectingId !== null} onClick={() => void selectGroup({ ...source, revision: sourceRevision.current ?? source.revision })} />
            <ActionButton icon={<ListChecks />} label="불러온 파일 선택" disabled={disabled || selectingId !== null || !members || !page.files.some(file => !members.has(file.id) && !selected.has(file.id)) || selected.size >= MAX_SELECTION} onClick={() => setSelected(previous => {
                const next = new Set(previous)
                for (const file of page.files) if (!members?.has(file.id) && next.size < MAX_SELECTION) next.add(file.id)
                return next
            })} />
        </div> : <p className="text-xs text-[var(--text-muted)]">묶음 전체 선택에는 아직 불러오지 않은 파일도 포함됩니다. 이미 앨범에 있는 파일은 제외됩니다.</p>}
        {selectionError && <p role="alert" className="text-sm text-red-500">{selectionError}</p>}
        {selectingId !== null && <p role="status" className="text-sm">묶음의 전체 파일 확인 중…</p>}
        {memberError && <div role="alert" className="text-sm text-red-500"><p>{memberError}</p><ActionButton icon={<RefreshCw />} label="포함 여부 다시 확인" disabled={disabled} onClick={() => setMemberAttempt(value => value + 1)} /></div>}
        {!members && !memberError && <p role="status" className="text-sm">앨범에 포함된 파일 확인 중…</p>}
        <div ref={root} className="max-h-[45dvh] min-h-32 overflow-y-auto overscroll-contain rounded-lg border border-[var(--border-line)] p-2" aria-label="미디어 검색 결과">
            {source ? <div className="grid grid-cols-2 gap-2 sm:grid-cols-3">{page.files.map(file => {
                const included = members?.has(file.id) ?? false, checked = selected.has(file.id), title = file.title || file.relativePath || file.fileKey || `파일 ${file.id}`
                return <label key={file.id} className={'relative flex flex-col gap-2 rounded-lg border p-2 ' + (checked ? 'border-[var(--color-primary)]' : 'border-[var(--border-line)]')}>
                    <MediaPreview path={file.thumbnailPath ?? file.previewPath ?? (file.kind === 'image' ? file.filepath : null)} title={title} className="aspect-square w-full rounded bg-[var(--bg-active)] object-contain" />
                    <span className="flex items-start gap-2 text-sm"><input type="checkbox" aria-label={`${title} 선택`} checked={included || checked} disabled={disabled || selectingId !== null || !members || included || !checked && selected.size >= MAX_SELECTION} onChange={event => select(file.id, event.target.checked)} /><span className="min-w-0 break-words">{title}</span></span>
                    <span className="text-xs text-[var(--text-muted)]">{included ? '이미 포함됨' : file.kind === 'video' ? '동영상' : file.kind === 'audio' ? '오디오' : file.kind === 'image' ? '이미지' : '파일'}</span>
                </label>
            })}</div> : <ul className="space-y-2">{groups.map(item => {
                const ids = groupFiles.get(item.id), included = ids?.length === 0
                const checked = included || Boolean(ids?.length && ids.every(id => selected.has(id)))
                return <li key={item.id} className="flex flex-wrap items-center gap-2 rounded-lg border border-[var(--border-line)] p-2">
                {item.fileCount === 1 && <input type="checkbox" aria-label={`${item.title} 선택`} checked={checked} disabled={disabled || !members || selectingId !== null || included || !checked && selected.size >= MAX_SELECTION} onChange={event => void selectGroup(item, event.target.checked)} />}
                <button type="button" className="flex min-w-0 flex-1 items-center gap-3 text-left disabled:opacity-40" disabled={disabled} aria-label={`${item.title} 파일 보기`} onClick={() => setSource(item)}>
                    <MediaPreview path={item.cover?.path ?? null} title={item.title} className="size-14 shrink-0 rounded bg-[var(--bg-active)] object-contain" />
                    <span className="min-w-0 flex-1"><span className="block break-words text-sm">{item.title}</span><span className="text-xs text-[var(--text-muted)]">{item.isUserAlbum ? '사용자 앨범' : '미디어 묶음'} · {item.fileCount}개</span></span>
                    <span aria-hidden="true">›</span>
                </button>
                {item.fileCount > 1 && <ActionButton icon={<CheckCheck />} label={included ? '이미 포함됨' : checked ? '전체 선택 해제' : '전체 선택'} aria-label={`${item.title} ${checked && !included ? '전체 선택 해제' : '전체 선택'}`} disabled={disabled || !members || selectingId !== null || included} onClick={() => void selectGroup(item, !checked)} />}
                {item.fileCount === 1 && included && <span className="text-xs text-[var(--text-muted)]">이미 포함됨</span>}
            </li>})}</ul>}
            {!page.loading && !page.error && !(source ? page.files.length : groups.length) && <p className="p-4 text-center text-sm text-[var(--text-muted)]">{source ? '파일이 없습니다.' : '추가할 미디어가 없습니다.'}</p>}
            {page.error && <div role="alert" className="space-y-2 p-2 text-sm text-red-500"><p>{page.error}</p><ActionButton icon={<RefreshCw />} label="다시 시도" disabled={disabled} onClick={() => void load(page.loaded)} /></div>}
            {page.loading && <p role="status" className="p-3 text-sm">미디어 불러오는 중…</p>}
            <div ref={sentinel} aria-hidden="true" className="h-px" />
        </div>
        <footer className="flex flex-wrap items-center gap-2">
            <p role="status" className="mr-auto text-sm">선택 {selected.size.toLocaleString()}개{selected.size >= MAX_SELECTION ? ' · 한 번에 최대 20,000개까지 추가할 수 있습니다.' : ''}</p>
            <ActionButton icon={<X />} label="선택 해제" disabled={disabled || selectingId !== null || !selected.size} onClick={() => setSelected(new Set())} />
            <ActionButton icon={<ListPlus />} label="선택한 미디어 추가" disabled={disabled || selectingId !== null || !members || !selected.size} onClick={async () => {
                if (adding.current) return
                adding.current = true
                try { await onAdd([...selected]) } finally { adding.current = false }
            }} />
        </footer>
    </section>
}
