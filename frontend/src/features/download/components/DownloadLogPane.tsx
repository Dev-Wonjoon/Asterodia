import { translateError, translateMessage, translate, useLanguage } from '@renderer/shared/i18n'
import { useEffect, useRef, useState } from 'react'
import type { DownloadLogPage } from '@contracts/download-api'
import { getApi } from '@renderer/shared/api'
import { formatDownloadLog, pollDownloadLogs } from '../downloadLogs'

export function DownloadLogPane({ jobId }: { jobId: string }) {
    useLanguage()
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
                if (!result.ok) { setError(translateError(result.error)); return }
                setPage(result.data); setError(null)
                if (pollDownloadLogs(result.data, before, attempt)) timer = setTimeout(() => void load(), 1000)
            } catch {
                if (!disposed) setError(translate("download.couldNotReadDownloadLogsPleaseRefresh"))
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
        try { await navigator.clipboard.writeText(page.entries.map(formatDownloadLog).join('\n')); setCopyNotice(translate("download.displayedLogsCopied")) }
        catch { setCopyNotice(translate("download.couldNotCopySelectTheLogsAndCopyThem")) }
    }
    return <section aria-label={translate("download.downloadExecutionLog")} className="space-y-3">
        <button type="button" className={button} aria-expanded={open} onClick={() => setOpen(!open)}>{open ? translate("download.closeLog") : translate("download.viewLogDetails")}</button>
        {open && <div className="space-y-3 rounded-xl border border-[var(--border-line)] p-3">
            <div className="flex flex-wrap items-center gap-2">
                <label className="text-xs">{translate("download.attempt")}<select aria-label={translate("download.logAttempt")} value={attempt}
                    className="rounded bg-[var(--bg-app)] p-1" onChange={event => { setAttempt(event.target.value); setBefore(0); setFollow(true) }}>
                    <option value="">{translate("download.allAttempts")}</option>
                    {page?.attempts.map(item => <option key={item.id} value={item.id}>{translate("download.log.attemptOption", { attempt: item.number, latestLabel: item.id === page.currentAttemptId ? translate("download.latest") : '' })}</option>)}
                </select></label>
                <button type="button" className={button} disabled={loading || !page?.hasOlder} onClick={() => { setBefore(page!.entries[0].sequence); setFollow(false) }}>{translate("download.olderLogs")}</button>
                <button type="button" className={button} disabled={loading} onClick={() => { setBefore(0); setFollow(true); setRevision(value => value + 1) }}>{translate("download.latestLogs")}</button>
                <button type="button" className={button} disabled={!page?.entries.length} onClick={() => void copy()}>{translate("download.copyDisplayedLogs")}</button>
                <label className="flex items-center gap-1 text-xs"><input type="checkbox" checked={follow} onChange={event => setFollow(event.target.checked)} />{translate("download.autoScroll")}</label>
            </div>
            <p className="text-xs text-[var(--text-muted)]">{translate("download.actualEngineOutputWarningsAndErrorsTheSystemStream")}</p>
            {loading && <p role="status" className="text-xs">{translate("download.readingLogs")}</p>}
            {error && <p role="alert" className="text-xs text-red-500">{translateMessage(error)} <button type="button" className="underline" onClick={() => setRevision(value => value + 1)}>{translate("common.refresh")}</button></p>}
            {page?.warning && <p role="alert" className="text-xs text-red-500">{page.warning}</p>}
            {page?.truncated && <p className="text-xs text-[var(--text-muted)]">{translate("download.someOlderLogsWereOmittedDueToRetentionLimits")}</p>}
            <pre ref={box} aria-label={translate("download.engineOutputLog")} tabIndex={0}
                className="max-h-96 min-h-32 select-text overflow-auto whitespace-pre-wrap break-all rounded-lg bg-[var(--bg-app)] p-3 font-mono text-xs">
                {page?.entries.length ? page.entries.map(formatDownloadLog).join('\n') : translate("download.noEngineOutputRecordedYet")}
            </pre>
            {copyNotice && <p role="status" className="text-xs">{copyNotice}</p>}
        </div>}
    </section>
}
