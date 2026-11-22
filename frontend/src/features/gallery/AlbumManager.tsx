import { useEffect, useImperativeHandle, useRef, useState, type Ref } from 'react'
import { createPortal } from 'react-dom'
import { getApi } from '@renderer/shared/api'
import type { AlbumRequest, MediaFileDto, MediaSummaryDto } from '@contracts/media-v2'
import type { MoveSelection } from '@contracts/file-move-api'
import { AlbumMediaPicker } from './AlbumMediaPicker'
import { ActionButton } from '@renderer/shared/components/ui/ActionButton'
import { FolderPlus, ListPlus, X, ArrowLeft, Search, Save, Plus, RefreshCw, Trash2, ArrowUp, ArrowDown, Minus, MoreHorizontal } from 'lucide-react'

export interface AlbumControls {
    edit: (album: MediaSummaryDto) => void
    addFiles: (fileIds: number[]) => void
    addMedia: (mediaId: number) => void
    addSelection: () => void
}

export function AlbumManager({ selection, fileIds, disabled, controlRef, onChanged, onClear, showSelectionActions = true }: {
    selection: MoveSelection[]; fileIds: number[]; disabled: boolean; controlRef: Ref<AlbumControls>;
    onChanged: (albumId?: number, deleted?: boolean) => void; onClear: () => void
    showSelectionActions?: boolean
}) {
    const api = getApi().media
    const [deletePending, setDeletePending] = useState(false)
    const [mode, setMode] = useState<'create' | 'add' | 'edit' | 'pick' | null>(null)
    const [album, setAlbum] = useState<MediaSummaryDto | null>(null)
    const [albums, setAlbums] = useState<MediaSummaryDto[]>([]), [moreAlbums, setMoreAlbums] = useState(false)
    const [target, setTarget] = useState(''), [keyword, setKeyword] = useState(''), [name, setName] = useState('')
    const [files, setFiles] = useState<MediaFileDto[]>([]), [moreFiles, setMoreFiles] = useState(false)
    const [overrideFiles, setOverrideFiles] = useState<number[] | null>(null)
    const [overrideMedia, setOverrideMedia] = useState<number[]>([])
    const [busy, setBusy] = useState(false), [error, setError] = useState('')
    const dialog = useRef<HTMLDialogElement>(null), dragging = useRef<number | null>(null), mounted = useRef(true), generation = useRef(0)
    useEffect(() => { mounted.current = true; return () => { mounted.current = false; generation.current++ } }, [])
    const button = 'rounded-lg border border-[var(--border-line)] px-3 py-2 text-sm disabled:opacity-40'
    const input = 'w-full rounded-lg border border-[var(--border-line)] bg-[var(--bg-app)] p-2'
    function close() { if (busy) return; generation.current++; setMode(mode === 'pick' ? 'edit' : null); setError(''); setDeletePending(false) }
    useEffect(() => {
        const node = dialog.current
        if (mode && node && !node.open) node.showModal()
    }, [mode])
    async function choose(append = false) {
        const own = generation.current; setBusy(true); setError('')
        try {
            const result = await api.albums({ limit: 50, offset: append ? albums.length : 0, keyword })
            if (!mounted.current || own !== generation.current) return
            if (!result.ok) throw new Error(result.error.message)
            setAlbums(previous => append ? [...previous, ...result.data.data] : result.data.data); setMoreAlbums(result.data.hasNextPage)
        } catch (e) { if (mounted.current && own === generation.current) setError(e instanceof Error ? e.message : '앨범을 불러오지 못했습니다.') }
        finally { if (mounted.current && own === generation.current) setBusy(false) }
    }
    async function loadFiles(value: MediaSummaryDto, append = false) {
        const own = generation.current; setBusy(true); setError('')
        try {
            const result = await api.get({ mediaId: value.id, fileLimit: 50, fileOffset: append ? files.length : 0 })
            if (!mounted.current || own !== generation.current) return
            if (!result.ok) throw new Error(result.error.message)
            // A concurrent edit requires a fresh order, never an append to a stale list.
            if (append && result.data.revision !== value.revision) { setError('앨범이 변경되었습니다. 새로고침해 주세요.'); setMoreFiles(false); return }
            setAlbum(result.data); setFiles(previous => append ? [...previous, ...result.data.files.data] : result.data.files.data); setMoreFiles(result.data.files.hasNextPage)
        } catch (e) { if (mounted.current && own === generation.current) setError(e instanceof Error ? e.message : '앨범을 불러오지 못했습니다.') }
        finally { if (mounted.current && own === generation.current) setBusy(false) }
    }
    function openAdd(ids: number[] | null = null, mediaIds: number[] = []) {
        if (disabled || busy) return
        generation.current++; setOverrideFiles(ids); setOverrideMedia(mediaIds); setTarget(''); setKeyword(''); setAlbums([]); setError(''); setMode('add'); void choose()
    }
    function edit(value: MediaSummaryDto) {
        if (disabled || busy) return
        generation.current++; setAlbum(value); setName(value.title); setFiles([]); setError(''); setMode('edit'); void loadFiles(value)
    }
    useImperativeHandle(controlRef, () => ({ edit, addFiles: openAdd, addMedia: id => openAdd([], [id]), addSelection: () => openAdd() }))
    async function save(request: AlbumRequest, dismiss = false): Promise<boolean> {
        if (busy) return false
        const own = generation.current; setBusy(true); setError('')
        try {
            const result = await api.saveAlbum(request)
            if (!mounted.current || own !== generation.current) return false
            if (!result.ok) throw new Error(result.error.message)
            onChanged(request.albumId, Boolean(request.delete))
            if (dismiss || request.delete) { generation.current++; setMode(null); onClear() }
            else { setAlbum(result.data); setBusy(false); await loadFiles(result.data) }
            return true
        } catch (e) { if (mounted.current && own === generation.current) setError(e instanceof Error ? e.message : '앨범 변경을 저장하지 못했습니다. 새로고침 후 확인하세요.') }
        finally { if (mounted.current) setBusy(false) }
        return false
    }
    const addFileIds = overrideFiles ?? fileIds
    const addMediaIds = overrideFiles !== null ? overrideMedia : selection.map(item => item.mediaId)
    const hasSelection = fileIds.length > 0 || selection.length > 0
    const move = (fileId: number, beforeFileId = 0) => { if (album && fileId !== beforeFileId) void save({ albumId: album.id, expectedRevision: album.revision, moveFileId: fileId, beforeFileId }) }
    return <>
        <div className="flex flex-wrap gap-2" aria-label="사용자 앨범">
            <ActionButton icon={<FolderPlus />} label="새 앨범" data-detail-trigger disabled={disabled || busy} onClick={() => { generation.current++; setOverrideFiles(null); setOverrideMedia([]); setName(''); setError(''); setMode('create') }} />
            {showSelectionActions && <ActionButton icon={<ListPlus />} label={'앨범에 추가' + (hasSelection ? ` (${selection.length + fileIds.length})` : '')} data-detail-trigger disabled={disabled || busy || !hasSelection} onClick={() => openAdd()} />}
            {showSelectionActions && fileIds.length > 0 && <ActionButton icon={<X />} label={`파일 선택 해제 (${fileIds.length})`} disabled={disabled || busy} onClick={onClear} />}
        </div>
        {mode && createPortal(<dialog ref={dialog} data-detail-trigger aria-labelledby="album-manager-title" className="m-auto max-h-[85dvh] w-[min(92vw,640px)] overflow-y-auto rounded-2xl border border-[var(--border-line)] bg-[var(--bg-app)] p-5 text-[var(--text-main)] shadow-2xl backdrop:bg-black/60" onKeyDown={event => event.stopPropagation()} onCancel={event => { event.preventDefault(); close() }}>
            <header className="mb-4 flex items-center justify-between gap-3"><h2 id="album-manager-title" className="font-semibold">{mode === 'pick' ? '미디어 추가' : mode === 'edit' ? '앨범 편집' : mode === 'create' ? '새 앨범' : '앨범에 추가'}</h2><ActionButton icon={mode === 'pick' ? <ArrowLeft /> : <X />} label={mode === 'pick' ? '돌아가기' : '닫기'} disabled={busy} onClick={close} /></header>
            <p className="mb-4 text-sm text-[var(--text-muted)]">여러 폴더의 파일을 연결합니다. 앨범에서 빼거나 앨범을 삭제해도 원본은 유지됩니다.</p>
            {error && <p role="alert" className="mb-3 text-sm text-red-500">{error}</p>}
            {(mode === 'create' || mode === 'edit') && <form className="flex gap-2" onSubmit={event => { event.preventDefault(); void save(mode === 'create' ? { title: name, addFileIds, addMediaIds } : { albumId: album!.id, expectedRevision: album!.revision, title: name }, mode === 'create') }}>
                <input autoFocus aria-label="앨범 이름" className={input} value={name} maxLength={1000} onChange={event => setName(event.target.value)} disabled={busy} />
                <ActionButton type="submit" icon={mode === 'create' ? <Plus /> : <Save />} label={mode === 'create' ? '만들기' : '이름 저장'} className="shrink-0" disabled={busy || !name.trim()} />
            </form>}
            {mode === 'add' && <div className="space-y-3">
                <form className="flex gap-2" onSubmit={event => { event.preventDefault(); setTarget(''); void choose() }}><input aria-label="앨범 검색" className={input} value={keyword} onChange={event => setKeyword(event.target.value)} /><ActionButton type="submit" icon={<Search />} label="검색" disabled={busy} /></form>
                <select aria-label="추가할 앨범" className={input} value={target} disabled={busy} onChange={event => setTarget(event.target.value)}><option value="">앨범 선택</option>{albums.filter(item => !addMediaIds.includes(item.id)).map(item => <option key={item.id} value={item.id}>{item.title} · {item.fileCount}개</option>)}</select>
                {moreAlbums && <ActionButton icon={<MoreHorizontal />} label="앨범 더 불러오기" disabled={busy} onClick={() => void choose(true)} />}
                {!busy && !albums.length && <p className="text-sm">사용자 앨범이 없습니다. ‘새 앨범’으로 먼저 만들어 주세요.</p>}
                <ActionButton icon={<ListPlus />} label="선택한 앨범에 추가" disabled={busy || !target} onClick={() => { const value = albums.find(item => item.id === Number(target)); if (value) void save({ albumId: value.id, expectedRevision: value.revision, addFileIds, addMediaIds }, true) }} />
            </div>}
            {mode === 'pick' && album && <AlbumMediaPicker album={album} disabled={disabled || busy} onAdd={async ids => {
                const saved = await save({ albumId: album.id, expectedRevision: album.revision, addFileIds: ids })
                if (saved && mounted.current) setMode('edit')
                return saved
            }} />}
            {mode === 'edit' && album && <div className="mt-4 space-y-3">
                <div className="flex flex-wrap gap-2"><ActionButton icon={<ListPlus />} label="미디어 추가" disabled={disabled || busy} onClick={() => { generation.current++; setDeletePending(false); setError(''); setMode('pick') }} /><ActionButton icon={<RefreshCw />} label="새로고침" disabled={busy} onClick={() => { setName(album.title); void loadFiles(album) }} /><ActionButton icon={<Trash2 />} label="앨범 삭제" disabled={busy} onClick={() => setDeletePending(true)} /></div>
                {deletePending && <div role="alert" className="rounded-lg border border-red-500 p-3 text-sm"><p>앨범을 삭제할까요? 원본 파일은 유지됩니다.</p><button className={button} disabled={busy} onClick={() => { setDeletePending(false); void save({ albumId: album.id, expectedRevision: album.revision, delete: true }) }}>앨범 삭제 확인</button><button className={button} disabled={busy} onClick={() => setDeletePending(false)}>취소</button></div>}
                <p className="text-sm">{album.fileCount}개 파일 · 드래그하거나 위·아래 버튼으로 순서를 바꿀 수 있습니다.</p>
                <ol className="space-y-2">{files.map((file, index) => <li key={file.id} draggable={!busy} onDragStart={event => { dragging.current = file.id; event.dataTransfer.setData('text/plain', String(file.id)) }} onDragEnd={() => { dragging.current = null }} onDragOver={event => { event.preventDefault() }} onDrop={event => { event.preventDefault(); if (dragging.current) move(dragging.current, file.id); dragging.current = null }} className="flex items-center gap-2 rounded-lg border border-[var(--border-line)] p-2">
                    <span className="min-w-0 flex-1 truncate text-sm" title={file.filepath}>{index + 1}. {file.title || file.relativePath || `파일 ${file.id}`}</span>
                    <ActionButton icon={<ArrowUp />} label="위로" aria-label={`${index + 1}번 파일 위로`} disabled={busy || index === 0} onClick={() => move(file.id, files[index - 1].id)} />
                    <ActionButton icon={<ArrowDown />} label="아래로" aria-label={`${index + 1}번 파일 아래로`} disabled={busy || index === files.length - 1 || moreFiles && index === files.length - 2} onClick={() => move(file.id, files[index + 2]?.id ?? 0)} />
                    <ActionButton icon={<Minus />} label="빼기" disabled={busy} onClick={() => void save({ albumId: album.id, expectedRevision: album.revision, removeFileIds: [file.id] })} />
                </li>)}</ol>
                {moreFiles && <ActionButton icon={<MoreHorizontal />} label="파일 더 불러오기" disabled={busy} onClick={() => void loadFiles(album, true)} />}
                {!files.length && !busy && <p className="text-sm">‘미디어 추가’로 이 앨범에 담을 파일을 선택하세요.</p>}
            </div>}
            {busy && <p role="status" className="mt-3 text-sm">처리 중…</p>}
        </dialog>, document.body)}
    </>
}
