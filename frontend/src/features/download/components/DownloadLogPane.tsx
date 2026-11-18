import { useEffect, useRef, useState } from 'react'
import type { DownloadLogPage } from '@contracts/download-api'
import { getApi } from '@renderer/shared/api'
import { formatDownloadLog, pollDownloadLogs } from '../downloadLogs'

export function DownloadLogPane({ jobId }: { jobId: string }) {
    const [open, setOpen] = useState(false)
    const [page, setPage] = useState<DownloadLogPage | null>(null)
    const [error, setError] = useState<string | null>(null)
    const [loading, setLoading] = useState(false)
    const [attempt, setAttempt] = useState('')
    const [before, setBefore] = useState(0)
    const [revision, setRevision] = useState(0)
    const [follow, setFollow] = useState(true)
    const [copyNotice, setCopyNotice] = useState<string | null>(null)
    const box = useRef<HTMLPreElement>(null)
    useEffect(() => {
        if (!open) return
        let disposed = false
        let timer: ReturnType<typeof setTimeout> | undefined
        setPage(null); setError(null); setLoading(true); setCopyNotice(null)
        async function load() {
            try {
                const result = await getApi().downloads.logs({ jobId, attemptId: attempt, before, limit: 100 })
                if (disposed) return
                if (!result.ok) { setError(result.error.message); return }
                setPage(result.data); setError(null)
                if (pollDownloadLogs(result.data, before, attempt)) timer = setTimeout(() => void load(), 1000)
            } catch {
                if (!disposed) setError('다운로드 로그를 읽지 못했습니다. 새로고침해 주세요.')
            } finally { if (!disposed) setLoading(false) }
        }
        void load()
        return () => { disposed = true; clearTimeout(timer) }
    }, [jobId, open, before, attempt, revision])
    useEffect(() => {
        if (follow && box.current) box.current.scrollTop = box.current.scrollHeight
    }, [page, follow])
    const button = 'rounded-lg border border-[var(--border-line)] px-3 py-2 text-xs disabled:opacity-40'
    const copy = async () => {
        if (!page) return
        try { await navigator.clipboard.writeText(page.entries.map(formatDownloadLog).join('\n')); setCopyNotice('표시된 로그를 복사했습니다.') }
        catch { setCopyNotice('복사하지 못했습니다. 로그를 선택해 직접 복사할 수 있습니다.') }
    }
    return <section aria-label="다운로드 실행 로그" className="space-y-3">
        <button type="button" className={button} aria-expanded={open} onClick={() => setOpen(!open)}>{open ? '로그 닫기' : '로그 상세보기'}</button>
        {open && <div className="space-y-3 rounded-xl border border-[var(--border-line)] p-3">
            <div className="flex flex-wrap items-center gap-2">
                <label className="text-xs">실행 시도 <select aria-label="로그 실행 시도" value={attempt}
                    className="rounded bg-[var(--bg-app)] p-1" onChange={event => { setAttempt(event.target.value); setBefore(0); setFollow(true) }}>
                    <option value="">모든 시도</option>
                    {page?.attempts.map(item => <option key={item.id} value={item.id}>시도 {item.number}{item.id === page.currentAttemptId ? ' · 최근' : ''}</option>)}
                </select></label>
                <button type="button" className={button} disabled={loading || !page?.hasOlder} onClick={() => { setBefore(page!.entries[0].sequence); setFollow(false) }}>이전 로그</button>
                <button type="button" className={button} disabled={loading} onClick={() => { setBefore(0); setFollow(true); setRevision(value => value + 1) }}>최신 로그</button>
                <button type="button" className={button} disabled={!page?.entries.length} onClick={() => void copy()}>표시된 로그 복사</button>
                <label className="flex items-center gap-1 text-xs"><input type="checkbox" checked={follow} onChange={event => setFollow(event.target.checked)} />자동 스크롤</label>
            </div>
            <p className="text-xs text-[var(--text-muted)]">엔진의 실제 출력·경고·오류입니다. system은 앱의 실행 기록입니다. 인증값·URL 쿼리와 추출용 JSON은 보관하지 않습니다.</p>
            {loading && <p role="status" className="text-xs">로그를 읽는 중…</p>}
            {error && <p role="alert" className="text-xs text-red-500">{error} <button type="button" className="underline" onClick={() => setRevision(value => value + 1)}>새로고침</button></p>}
            {page?.warning && <p role="alert" className="text-xs text-red-500">{page.warning}</p>}
            {page?.truncated && <p className="text-xs text-[var(--text-muted)]">보관 한도(최근 2,000줄·1MiB) 때문에 일부 오래된 로그는 생략됐습니다.</p>}
            <pre ref={box} aria-label="엔진 출력 로그" tabIndex={0}
                className="max-h-96 min-h-32 select-text overflow-auto whitespace-pre-wrap break-all rounded-lg bg-[var(--bg-app)] p-3 font-mono text-xs">
                {page?.entries.length ? page.entries.map(formatDownloadLog).join('\n') : '아직 기록된 엔진 출력이 없습니다.'}
            </pre>
            {copyNotice && <p role="status" className="text-xs">{copyNotice}</p>}
        </div>}
    </section>
}
