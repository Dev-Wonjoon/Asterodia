import { useEffect, useImperativeHandle, useRef, useState, type Ref } from 'react'
import { createPortal } from 'react-dom'
import { getApi } from '@renderer/shared/api'
import type { AlbumRequest, MediaFileDto, MediaSummaryDto } from '@contracts/media-v2'
import type { MoveSelection } from '@contracts/file-move-api'

export interface AlbumControls {
    edit: (album: MediaSummaryDto) => void
    addFiles: (fileIds: number[]) => void
}

export function AlbumManager({ selection, fileIds, disabled, controlRef, onChanged, onClear }: {
    selection: MoveSelection[]; fileIds: number[]; disabled: boolean; controlRef: Ref<AlbumControls>;
    onChanged: (albumId?: number, deleted?: boolean) => void; onClear: () => void
}) {
    const api = getApi().media
    const [deletePending, setDeletePending] = useState(false)
    const [mode, setMode] = useState<'create' | 'add' | 'edit' | null>(null)
    const [album, setAlbum] = useState<MediaSummaryDto | null>(null)
    const [albums, setAlbums] = useState<MediaSummaryDto[]>([]), [moreAlbums, setMoreAlbums] = useState(false)
    const [target, setTarget] = useState(''), [keyword, setKeyword] = useState(''), [name, setName] = useState('')
    const [files, setFiles] = useState<MediaFileDto[]>([]), [moreFiles, setMoreFiles] = useState(false)
    const [overrideFiles, setOverrideFiles] = useState<number[] | null>(null)
    const [busy, setBusy] = useState(false), [error, setError] = useState('')
    const dialog = useRef<HTMLDialogElement>(null), dragging = useRef<number | null>(null), mounted = useRef(true), generation = useRef(0)
    useEffect(() => { mounted.current = true; return () => { mounted.current = false; generation.current++ } }, [])
    const button = 'rounded-lg border border-[var(--border-line)] px-3 py-2 text-sm disabled:opacity-40'
    const input = 'w-full rounded-lg border border-[var(--border-line)] bg-[var(--bg-app)] p-2'
    function close() { if (busy) return; generation.current++; setMode(null); setError(''); setDeletePending(false) }
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
    function openAdd(ids: number[] | null = null) {
        if (disabled || busy) return
        generation.current++; setOverrideFiles(ids); setTarget(''); setKeyword(''); setAlbums([]); setError(''); setMode('add'); void choose()
    }
    function edit(value: MediaSummaryDto) {
        if (disabled || busy) return
        generation.current++; setAlbum(value); setName(value.title); setFiles([]); setError(''); setMode('edit'); void loadFiles(value)
    }
    useImperativeHandle(controlRef, () => ({ edit, addFiles: openAdd }))
    async function save(request: AlbumRequest, dismiss = false) {
        if (busy) return
        const own = generation.current; setBusy(true); setError('')
        try {
            const result = await api.saveAlbum(request)
            if (!mounted.current || own !== generation.current) return
            if (!result.ok) throw new Error(result.error.message)
            onChanged(request.albumId, Boolean(request.delete))
            if (dismiss || request.delete) { generation.current++; setMode(null); onClear() }
            else { setAlbum(result.data); setBusy(false); await loadFiles(result.data) }
        } catch (e) { if (mounted.current && own === generation.current) setError(e instanceof Error ? e.message : '앨범 변경을 저장하지 못했습니다. 새로고침 후 확인하세요.') }
        finally { if (mounted.current) setBusy(false) }
    }
    const addFileIds = overrideFiles ?? fileIds
    const addMediaIds = overrideFiles ? [] : selection.map(item => item.mediaId)
    const hasSelection = fileIds.length > 0 || selection.length > 0
    const move = (fileId: number, beforeFileId = 0) => { if (album && fileId !== beforeFileId) void save({ albumId: album.id, expectedRevision: album.revision, moveFileId: fileId, beforeFileId }) }
    return <>
        <div className="flex flex-wrap gap-2" aria-label="사용자 앨범">
            <button type="button" data-detail-trigger className={button} disabled={disabled || busy} onClick={() => { generation.current++; setOverrideFiles(null); setName(''); setError(''); setMode('create') }}>새 앨범</button>
            <button type="button" data-detail-trigger className={button} disabled={disabled || busy || !hasSelection} onClick={() => openAdd()}>앨범에 추가{hasSelection ? ` (${selection.length + fileIds.length})` : ''}</button>
            {fileIds.length > 0 && <button type="button" className={button} disabled={disabled || busy} onClick={onClear}>파일 선택 해제 ({fileIds.length})</button>}
        </div>
        {mode && createPortal(<dialog ref={dialog} data-detail-trigger aria-labelledby="album-manager-title" className="m-auto max-h-[85dvh] w-[min(92vw,640px)] overflow-y-auto rounded-2xl border border-[var(--border-line)] bg-[var(--bg-app)] p-5 text-[var(--text-main)] shadow-2xl backdrop:bg-black/60" onKeyDown={event => event.stopPropagation()} onCancel={event => { event.preventDefault(); close() }}>
            <header className="mb-4 flex items-center justify-between gap-3"><h2 id="album-manager-title" className="font-semibold">{mode === 'edit' ? '앨범 편집' : mode === 'create' ? '새 앨범' : '앨범에 추가'}</h2><button className={button} disabled={busy} onClick={close}>닫기</button></header>
            <p className="mb-4 text-sm text-[var(--text-muted)]">여러 폴더의 파일을 연결합니다. 앨범에서 빼거나 앨범을 삭제해도 원본은 유지됩니다.</p>
            {error && <p role="alert" className="mb-3 text-sm text-red-500">{error}</p>}
            {mode !== 'add' && <form className="flex gap-2" onSubmit={event => { event.preventDefault(); void save(mode === 'create' ? { title: name, addFileIds, addMediaIds } : { albumId: album!.id, expectedRevision: album!.revision, title: name }, mode === 'create') }}>
                <input autoFocus aria-label="앨범 이름" className={input} value={name} maxLength={1000} onChange={event => setName(event.target.value)} disabled={busy} />
                <button className={button + ' shrink-0'} disabled={busy || !name.trim()}>{mode === 'create' ? '만들기' : '이름 저장'}</button>
            </form>}
            {mode === 'add' && <div className="space-y-3">
                <form className="flex gap-2" onSubmit={event => { event.preventDefault(); setTarget(''); void choose() }}><input aria-label="앨범 검색" className={input} value={keyword} onChange={event => setKeyword(event.target.value)} /><button className={button} disabled={busy}>검색</button></form>
                <select aria-label="추가할 앨범" className={input} value={target} disabled={busy} onChange={event => setTarget(event.target.value)}><option value="">앨범 선택</option>{albums.filter(item => !addMediaIds.includes(item.id)).map(item => <option key={item.id} value={item.id}>{item.title} · {item.fileCount}개</option>)}</select>
                {moreAlbums && <button className={button} disabled={busy} onClick={() => void choose(true)}>앨범 더 불러오기</button>}
                {!busy && !albums.length && <p className="text-sm">사용자 앨범이 없습니다. ‘새 앨범’으로 먼저 만들어 주세요.</p>}
                <button className={button} disabled={busy || !target} onClick={() => { const value = albums.find(item => item.id === Number(target)); if (value) void save({ albumId: value.id, expectedRevision: value.revision, addFileIds, addMediaIds }, true) }}>선택한 앨범에 추가</button>
            </div>}
            {mode === 'edit' && album && <div className="mt-4 space-y-3">
                <div className="flex flex-wrap gap-2"><button className={button} disabled={busy} onClick={() => { setName(album.title); void loadFiles(album) }}>새로고침</button><button className={button} disabled={busy} onClick={() => setDeletePending(true)}>앨범 삭제</button></div>
                {deletePending && <div role="alert" className="rounded-lg border border-red-500 p-3 text-sm"><p>앨범을 삭제할까요? 원본 파일은 유지됩니다.</p><button className={button} disabled={busy} onClick={() => { setDeletePending(false); void save({ albumId: album.id, expectedRevision: album.revision, delete: true }) }}>앨범 삭제 확인</button><button className={button} disabled={busy} onClick={() => setDeletePending(false)}>취소</button></div>}
                <p className="text-sm">{album.fileCount}개 파일 · 드래그하거나 위·아래 버튼으로 순서를 바꿀 수 있습니다.</p>
                <ol className="space-y-2">{files.map((file, index) => <li key={file.id} draggable={!busy} onDragStart={event => { dragging.current = file.id; event.dataTransfer.setData('text/plain', String(file.id)) }} onDragEnd={() => { dragging.current = null }} onDragOver={event => { event.preventDefault() }} onDrop={event => { event.preventDefault(); if (dragging.current) move(dragging.current, file.id); dragging.current = null }} className="flex items-center gap-2 rounded-lg border border-[var(--border-line)] p-2">
                    <span className="min-w-0 flex-1 truncate text-sm" title={file.filepath}>{index + 1}. {file.title || file.relativePath || `파일 ${file.id}`}</span>
                    <button aria-label={`${index + 1}번 파일 위로`} className={button} disabled={busy || index === 0} onClick={() => move(file.id, files[index - 1].id)}>↑</button>
                    <button aria-label={`${index + 1}번 파일 아래로`} className={button} disabled={busy || index === files.length - 1 || moreFiles && index === files.length - 2} onClick={() => move(file.id, files[index + 2]?.id ?? 0)}>↓</button>
                    <button className={button} disabled={busy} onClick={() => void save({ albumId: album.id, expectedRevision: album.revision, removeFileIds: [file.id] })}>빼기</button>
                </li>)}</ol>
                {moreFiles && <button className={button} disabled={busy} onClick={() => void loadFiles(album, true)}>파일 더 불러오기</button>}
                {!files.length && !busy && <p className="text-sm">갤러리나 상세 보기에서 파일을 선택해 이 앨범에 추가하세요.</p>}
            </div>}
            {busy && <p role="status" className="mt-3 text-sm">처리 중…</p>}
        </dialog>, document.body)}
    </>
}
