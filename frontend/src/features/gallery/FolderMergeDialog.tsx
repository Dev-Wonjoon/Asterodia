import { translateError, translateMessage, translate, useLanguage } from '@renderer/shared/i18n'
import { useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import type { MoveSelection } from '@contracts/file-move-api'
import type { MediaDirectoryDto } from '@contracts/media-v2'
import { getApi } from '@renderer/shared/api'

type Mode = 'album' | 'files' | 'combined'
export function FolderMergeDialog({ selection, onClose, onChanged, onStarted }: {
    selection: MoveSelection[]; onClose(): void; onChanged(): void; onStarted(jobId: string, error?: string): void
}) {
    useLanguage()
    const dialog = useRef<HTMLDialogElement>(null)
    const alive = useRef(true), submitting = useRef(false), read = useRef(0)
    const [mode, setMode] = useState<Mode>('album')
    const [name, setName] = useState('')
    const [operation, setOperation] = useState<'copy' | 'move'>('copy')
    const [folders, setFolders] = useState<MediaDirectoryDto[]>([])
    const [directory, setDirectory] = useState(''), [keyword, setKeyword] = useState('')
    const [more, setMore] = useState(false), [loading, setLoading] = useState(false)
    const [busy, setBusy] = useState(false), [error, setError] = useState(''), [uncertain, setUncertain] = useState(false)
    const api = getApi()
    const input = 'w-full rounded-lg border border-[var(--border-line)] bg-[var(--bg-sidebar)] p-2'
    const button = 'rounded-lg border border-[var(--border-line)] px-3 py-2 text-sm disabled:opacity-40'
    useEffect(() => {
        alive.current = true; dialog.current?.showModal()
        return () => { alive.current = false; read.current++; dialog.current?.close() }
    }, [])
    async function load(append = false) {
        const own = ++read.current; setLoading(true); setError('')
        if (!append) { setDirectory(''); setFolders([]) }
        try {
            const result = await api.media.directories({ keyword, limit: 100, offset: append ? folders.length : 0 })
            if (!alive.current || own !== read.current) return
            if (!result.ok) { setError(translateError(result.error)); return }
            setFolders(previous => append ? [...previous, ...result.data.data] : result.data.data); setMore(result.data.hasNextPage)
        } catch { if (alive.current && own === read.current) setError(translate("대상 폴더를 불러오지 못했습니다.")) }
        finally { if (alive.current && own === read.current) setLoading(false) }
    }
    useEffect(() => {
        if (mode !== 'album') void load()
        else { read.current++; setLoading(false); setError('') }
    }, [mode])
    async function start() {
        if (submitting.current || uncertain || !selection.length || (mode !== 'files' && !name.trim()) || (mode !== 'album' && !directory)) return
        submitting.current = true; setBusy(true); setError('')
        let job = ''
        try {
            if (mode === 'album') {
                const result = await api.media.mergeAlbum({ title: name.trim(), selection })
                if (!alive.current) return
                if (!result.ok) { setError(translateError(result.error)); return }
                onChanged(); onClose(); return
            }
            const created = await api.fileMove.create({ selection, directoryId: Number(directory), operation, ...(mode === 'combined' ? { mergeTitle: name.trim() } : {}) })
            if (!alive.current) return
            if (!created.ok) { setError(translateError(created.error)); return }
            job = created.data.jobId
            let message = ''
            try {
                const result = await api.fileMove.run({ jobId: job })
                if (!result.ok) message = translateError(result.error)
            } catch { message = translate("작업 시작 결과를 확인하지 못했습니다. 상태를 새로고침한 뒤 이어하기를 확인하세요.") }
            if (!alive.current) return
            onChanged(); onStarted(job, message); onClose()
        } catch {
            if (alive.current) {
                setUncertain(true)
                setError(translate("요청 결과를 확인하지 못했습니다. 중복 생성을 막기 위해 갤러리와 최근 파일 작업을 새로고침한 뒤 확인하세요."))
            }
        } finally { submitting.current = false; if (alive.current) setBusy(false) }
    }
    return createPortal(<dialog ref={dialog} data-detail-trigger aria-labelledby="folder-merge-title" className="m-auto max-h-[85dvh] w-[min(92vw,640px)] space-y-4 overflow-auto rounded-2xl border border-[var(--border-line)] bg-[var(--bg-app)] p-5 text-[var(--text-main)] shadow-2xl backdrop:bg-black/60" onKeyDown={event => event.stopPropagation()} onCancel={event => { event.preventDefault(); if (!busy) onClose() }}>
        <header className="flex items-center justify-between gap-3"><h2 id="folder-merge-title" className="font-semibold">{translate("폴더·앨범 병합")}</h2><button type="button" className={button} disabled={busy} onClick={onClose}>{translate("닫기")}</button></header>
        <fieldset disabled={busy} className="space-y-2"><legend className="mb-2 text-sm">{translate("병합 방식")}</legend>
            {([['album', translate("앨범만 합치기")], ['files', translate("실제 파일만 모으기")], ['combined', translate("앨범으로 합치고 파일도 모으기")]] as const).map(([value, label]) => <label key={value} className="flex gap-2 text-sm"><input type="radio" name="folder-merge-mode" value={value} checked={mode === value} onChange={() => setMode(value)} />{label}</label>)}
        </fieldset>
        {mode !== 'files' && <label className="block space-y-1 text-sm">{translate("새 앨범 이름")}<input aria-label={translate("새 앨범 이름")} className={input} value={name} maxLength={1000} disabled={busy} onChange={event => setName(event.target.value)} /></label>}
        <p className="text-sm text-[var(--text-muted)]">{translate("원본 앨범은 유지합니다. 새 앨범에는 같은 파일을 한 번만 연결합니다.")}</p>
        {mode !== 'album' && <div className="space-y-3">
            <label className="block text-sm">{translate("파일 모으기 방식")}<select aria-label={translate("병합 파일 처리 방식")} className={input} disabled={busy} value={operation} onChange={event => setOperation(event.target.value as 'copy' | 'move')}><option value="copy">{translate("복사 · 원본 유지")}</option><option value="move">{translate("이동 · 파일 위치 변경")}</option></select></label>
            <p className="text-sm text-[var(--text-muted)]">{translate("{0} 같은 이름은 번호를 붙여 보존합니다.", { "0": operation === 'copy' ? translate("파일을 대상 폴더에 복사합니다. 앨범의 파일 연결은 원본 위치를 유지합니다.") : translate("검증 후 원본을 정리하며, 기존 앨범과 새 앨범 모두 변경된 파일 위치를 사용합니다.") })}</p>
            <form className="flex gap-2" onSubmit={event => { event.preventDefault(); if (!busy && !loading) void load() }}><input aria-label={translate("병합 대상 폴더 검색")} className={input} value={keyword} disabled={busy || loading} onChange={event => setKeyword(event.target.value)} /><button className={button} disabled={busy || loading}>{translate("검색")}</button></form>
            <label className="block text-sm">{translate("대상 폴더")}<select aria-label={translate("병합 대상 폴더")} className={input} value={directory} disabled={busy || loading} onChange={event => setDirectory(event.target.value)}><option value="">{translate("폴더 선택")}</option>{folders.map(folder => <option key={folder.id} value={folder.id}>{folder.directoryPath}</option>)}</select></label>
            {loading && <p role="status" className="text-sm">{translate("폴더를 불러오는 중…")}</p>}
            {!loading && !folders.length && <p className="text-sm">{translate("등록된 폴더가 없습니다. 로컬 폴더 가져오기로 대상 폴더를 먼저 등록하세요.")}</p>}
            {more && <button type="button" className={button} disabled={busy || loading} onClick={() => void load(true)}>{translate("폴더 더 불러오기")}</button>}
        </div>}
        {error && <p role="alert" className="text-sm text-red-500">{translateMessage(error)}</p>}
        <footer className="flex justify-end gap-2"><button type="button" className={button} disabled={busy} onClick={onClose}>{translate("취소")}</button><button type="button" className={button} disabled={busy || loading || uncertain || !selection.length || (mode !== 'files' && !name.trim()) || (mode !== 'album' && !directory)} onClick={() => void start()}>{busy ? translate("요청 중…") : translate("병합 시작")}</button></footer>
    </dialog>, document.body)
}
