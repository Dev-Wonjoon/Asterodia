import { useEffect, useId, useRef, useState } from 'react'
import type { MoveSelection } from '@contracts/file-move-api'
import type { BatchRequest, BatchResult, ConversionFormat } from '@contracts/conversion'
import { activeConversionStates, latestBatchJobs, retryableConversionStates } from '@contracts/conversion'
import type { EditJob, EditQuality } from '@contracts/media-edit-api'
import { getApi } from '@renderer/shared/api'
import { translate, translateError, translateMessage, useLanguage } from '@renderer/shared/i18n'
import { BaseModal } from '@renderer/shared/components/ui/BaseModal'
import { MediaFormatFields, formatFieldClass, useOutputFormats } from './MediaFormatFields'

const button = 'rounded-lg border border-[var(--border-line)] px-3 py-2 text-sm disabled:opacity-40'
const defaultQuality: EditQuality = { preset: 'balanced', quality: 80, effort: 'normal', lossless: false }

export function BatchConvertPanel({ selection, initialMode = 'convert', onClose, onChanged, allowReveal = true }: {
    selection: MoveSelection[]; initialMode?: 'convert' | 'webp'; onClose(): void; onChanged?(): void; allowReveal?: boolean
}) {
    useLanguage()
    const title = useId(), panel = useRef<HTMLElement>(null)
    const { formats, error: formatError } = useOutputFormats()
    const [mode, setMode] = useState(initialMode)
    const [format, setFormat] = useState<ConversionFormat>({ type: 'video', extension: 'mp4', videoCodec: 'h264', audioCodec: 'aac' })
    const [start, setStart] = useState(0), [duration, setDuration] = useState(30)
    const [width, setWidth] = useState(640), [fps, setFPS] = useState(15), [loop, setLoop] = useState(0)
    const [quality, setQuality] = useState<EditQuality>(defaultQuality)
    const [directory, setDirectory] = useState(''), [jobs, setJobs] = useState<EditJob[]>([])
    const [failures, setFailures] = useState<BatchResult['failures']>([]), [error, setError] = useState('')
    const [statusError, setStatusError] = useState('')
    const [busy, setBusy] = useState(false)
    const request = useRef<{ key: string; value: BatchRequest } | null>(null)
    const lock = useRef(false), alive = useRef(true), changed = useRef(onChanged)
    const notified = useRef(new Set<string>())
    changed.current = onChanged

    useEffect(() => {
        alive.current = true
        const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null
        const background = document.getElementById('root'), wasInert = background?.inert ?? false
        if (background) background.inert = true
        panel.current?.focus()
        const trap = (event: KeyboardEvent) => {
            if (event.key !== 'Tab') return
            const elements = [...panel.current?.querySelectorAll<HTMLElement>('button:not(:disabled),input:not(:disabled),select:not(:disabled),a[href]') ?? []].filter(item => !item.closest('[hidden]'))
            const first = elements[0], last = elements[elements.length - 1]
            if (event.shiftKey && (document.activeElement === first || document.activeElement === panel.current)) { event.preventDefault(); last?.focus() }
            else if (!event.shiftKey && (document.activeElement === last || document.activeElement === panel.current)) { event.preventDefault(); first?.focus() }
        }
        document.addEventListener('keydown', trap, true)
        return () => {
            alive.current = false
            if (background) background.inert = wasInert
            document.removeEventListener('keydown', trap, true)
            if (previous?.isConnected) previous.focus()
        }
    }, [])

    async function refresh() {
        const result = await getApi().mediaEdit.list()
        if (!alive.current) return
        if (!result.ok) throw new Error(translateError(result.error))
        setStatusError('')
        const batchJobs = latestBatchJobs(result.data.jobs)
        setJobs(batchJobs)
        let completed = false
        for (const job of batchJobs) {
            if (job.state === 'completed' && !notified.current.has(job.id)) { notified.current.add(job.id); completed = true }
        }
        if (completed) changed.current?.()
    }
    useEffect(() => {
        let disposed = false, timer: ReturnType<typeof setTimeout>
        const poll = async () => {
            try { await refresh() } catch { if (alive.current) setStatusError(translate('conversion.statusUnavailable')) }
            if (!disposed) timer = setTimeout(poll, 1500)
        }
        void poll()
        return () => { disposed = true; clearTimeout(timer) }
    }, [])

    const available = formats.some(item => item.type === (mode === 'webp' ? 'image' : format.type) && item.extension === (mode === 'webp' ? 'webp' : format.extension))
    const validWebP = Number.isFinite(start) && start >= 0 && Number.isFinite(duration) && duration > 0 && duration <= 30 && Number.isInteger(width) && width >= 64 && width <= 1920 && Number.isInteger(fps) && fps >= 1 && fps <= 30 && Number.isInteger(quality.quality) && quality.quality >= 1 && quality.quality <= 100
    async function action(work: () => Promise<void>) {
        if (lock.current) return
        lock.current = true; setBusy(true); setError('')
        try { await work() }
        catch (error) { if (alive.current) setError(error instanceof Error ? error.message : translate('conversion.requestFailed')) }
        finally { lock.current = false; if (alive.current) setBusy(false) }
    }
    function submit() {
        if (!selection.length || !available || mode === 'webp' && !validWebP) return
        void action(async () => {
            const options = { selection, mode, format, start, duration, width, fps, loop, quality, ...(directory ? { outputDirectory: directory } : {}) }
            const key = JSON.stringify(options)
            if (request.current?.key !== key) request.current = { key, value: { ...options, requestId: crypto.randomUUID() } }
            const result = await getApi().mediaEdit.batch(request.current.value)
            if (!result.ok) { request.current = null; throw new Error(translateError(result.error)) }
            if (!alive.current) return
            setFailures(result.data.failures)
            await refresh()
            // Keep the idempotency key until settings or selection change.
        })
    }
    async function reveal(job: EditJob) {
        const result = await getApi().mediaEdit.resultPath({ id: job.id })
        if (!result.ok) throw new Error(translateError(result.error))
        await getApi().showItemInFolder(result.data)
    }
    async function retry(values: EditJob[]) {
        for (const job of values) {
            const result = await getApi().mediaEdit.retryBatch({ id: job.id })
            if (!result.ok) throw new Error(translateError(result.error))
        }
        await refresh()
    }
    const batchIDs = [...new Set(jobs.map(job => job.options.batch!.id))]
    return <BaseModal onClose={onClose}>
        <section ref={panel} tabIndex={-1} role="dialog" aria-modal="true" aria-labelledby={title} className="max-h-[90dvh] w-[min(56rem,calc(100vw-2rem))] space-y-4 overflow-y-auto p-5 text-[var(--text-main)]">
            <div className="flex items-center justify-between gap-3"><h2 id={title} className="text-lg font-semibold">{translate('conversion.title')}</h2><button className={button} onClick={onClose}>{translate('common.close')}</button></div>
            <p className="text-sm text-[var(--text-muted)]">{translate('conversion.description')}</p>
            <fieldset disabled={busy} className="grid gap-4 sm:grid-cols-2">
                <div className="space-y-3">
                    <p className="text-sm">{translate('conversion.selected', { count: selection.length })}</p>
                    {!selection.length && <p className="text-sm text-[var(--text-muted)]">{translate('conversion.selectFirst')}</p>}
                    <label className="block text-sm">{translate('conversion.mode')}<select className={formatFieldClass} value={mode} onChange={event => setMode(event.target.value as typeof mode)}>
                        <option value="convert">{translate('conversion.convert')}</option><option value="webp">{translate('conversion.webp')}</option>
                    </select></label>
                    {mode === 'convert' ? <>
                        <label className="block text-sm">{translate('download.type')}<select className={formatFieldClass} value={format.type} onChange={event => setFormat(event.target.value === 'audio' ? { type: 'audio', extension: 'mp3', audioCodec: 'mp3' } : { type: 'video', extension: 'mp4', videoCodec: 'h264', audioCodec: 'aac' })}><option value="video">{translate('media.video')}</option><option value="audio">{translate('conversion.audio')}</option></select></label>
                        <MediaFormatFields value={format} onChange={setFormat} formats={formats} />
                    </> : <>
                        <p className="text-xs text-[var(--text-muted)]">{translate('conversion.webpBounds')}</p>
                        <label className="block text-sm">{translate('conversion.start')}<input className={formatFieldClass} type="number" min={0} step="0.1" value={start} onChange={event => setStart(event.target.valueAsNumber)} /></label>
                        <label className="block text-sm">{translate('conversion.duration')}<input className={formatFieldClass} type="number" min="0.1" max={30} step="0.1" value={duration} onChange={event => setDuration(event.target.valueAsNumber)} /></label>
                        <label className="block text-sm">{translate('conversion.width')}<input className={formatFieldClass} type="number" min={64} max={1920} value={width} onChange={event => setWidth(event.target.valueAsNumber)} /></label>
                        <label className="block text-sm">FPS<input className={formatFieldClass} type="number" min={1} max={30} value={fps} onChange={event => setFPS(event.target.valueAsNumber)} /></label>
                    </>}
                </div>
                <div className="space-y-3">
                    {mode === 'webp' && <>
                        <label className="block text-sm">{translate('conversion.quality')}<input className={formatFieldClass} type="number" min={1} max={100} value={quality.quality} onChange={event => setQuality({ ...quality, preset: 'custom', quality: event.target.valueAsNumber })} /></label>
                        <label className="flex gap-2 text-sm"><input type="checkbox" checked={quality.lossless} onChange={event => setQuality({ ...quality, preset: 'custom', lossless: event.target.checked })} />{translate('conversion.lossless')}</label>
                        <label className="flex gap-2 text-sm"><input type="checkbox" checked={loop === 0} onChange={event => setLoop(event.target.checked ? 0 : 1)} />{translate('conversion.loop')}</label>
                    </>}
                    <p className="text-sm">{translate('conversion.output')}</p><p className="break-all text-xs text-[var(--text-muted)]">{directory || translate('conversion.defaultDirectory')}</p>
                    {allowReveal && <div className="flex gap-2"><button className={button} onClick={() => void action(async () => { const path = await getApi().chooseEditOutputDirectory(); if (path && alive.current) setDirectory(path) })}>{translate('conversion.chooseDirectory')}</button>{directory && <button className={button} onClick={() => setDirectory('')}>{translate('conversion.resetDirectory')}</button>}</div>}
                    {formatError && <p role="alert" className="text-sm text-red-500">{translateMessage(formatError)}</p>}
                    <button disabled={busy || !selection.length || !available || mode === 'webp' && !validWebP} className={button + ' bg-[var(--color-primary)] text-white'} onClick={submit}>{translate('conversion.enqueue')}</button>
                </div>
            </fieldset>
            {error && <p role="alert" className="text-sm text-red-500">{translateMessage(error)}</p>}
            {statusError && <p role="alert" className="text-sm text-red-500">{translateMessage(statusError)}</p>}
            {failures.length > 0 && <ul className="max-h-32 overflow-y-auto text-sm text-red-500">{failures.map((item, i) => <li key={i}>{item.fileId || item.mediaId}: {translateMessage(item.message)}</li>)}</ul>}
            <h3 className="font-semibold">{translate('conversion.history')}</h3>
            {!jobs.length && <p className="text-sm text-[var(--text-muted)]">{translate('conversion.noJobs')}</p>}
            {batchIDs.map(id => {
                const values = jobs.filter(job => job.options.batch!.id === id)
                const running = values.filter(job => activeConversionStates.has(job.state))
                const failed = values.filter(job => retryableConversionStates.has(job.state))
                const completed = values.filter(job => job.state === 'completed').length
                return <section key={id} className="space-y-2 rounded-lg border border-[var(--border-line)] p-3">
                    <div className="flex flex-wrap items-center justify-between gap-2"><p role="status" className="text-sm">{translate('conversion.summary', { total: values.length, completed, failed: failed.length, active: running.length })}</p><div className="flex gap-2">
                        {failed.length > 0 && <button disabled={busy} className={button} onClick={() => void action(() => retry(failed))}>{translate('conversion.retryFailed')}</button>}
                        {running.length > 0 && <button disabled={busy} className={button} onClick={() => void action(async () => { const result = await getApi().mediaEdit.cancelBatch({ id }); if (!result.ok) throw new Error(translateError(result.error)); await refresh() })}>{translate('conversion.cancelBatch')}</button>}
                    </div></div>
                    <ul className="max-h-64 space-y-2 overflow-y-auto">{values.map(job => <li key={job.id} className="rounded-lg bg-[var(--bg-app)] p-2 text-sm">
                        <div className="flex flex-wrap items-center justify-between gap-2"><span className="min-w-0 break-all">{job.options.batch!.title} · {job.options.batch!.mode === 'webp' ? 'WebP' : job.options.batch!.format.extension?.toUpperCase()}</span><span>{translate(`conversion.state.${job.state === 'stop_unconfirmed' ? 'stopUnconfirmed' : job.state}`)} {activeConversionStates.has(job.state) ? `${job.progress}%` : ''}</span></div>
                        {activeConversionStates.has(job.state) && <progress aria-label={job.options.batch!.title} className="mt-2 w-full" value={job.progress} max={100} />}
                        {job.error && <p className="mt-1 text-red-500">{translateMessage(job.error)}</p>}
                        {job.outputNotice && <p>{translateMessage(job.outputNotice)}</p>}
                        <div className="mt-1 flex gap-2">
                            {retryableConversionStates.has(job.state) && <button disabled={busy} className={button} onClick={() => void action(() => retry([job]))}>{translate('conversion.retry')}</button>}
                            {['pending', 'running', 'verifying'].includes(job.state) && <button disabled={busy || job.cancelling} className={button} onClick={() => void action(async () => { const result = await getApi().mediaEdit.cancel({ id: job.id }); if (!result.ok) throw new Error(translateError(result.error)); await refresh() })}>{translate('common.cancel')}</button>}
                            {job.state === 'completed' && job.resultFileId && (allowReveal ? <button disabled={busy} className={button} onClick={() => void action(() => reveal(job))}>{translate('conversion.reveal')}</button> : <a className={button} href={`/edit-assets/jobs/${job.id}?download=1`} download>{translate('conversion.downloadResult')}</a>)}
                        </div>
                    </li>)}</ul>
                </section>
            })}
        </section>
    </BaseModal>
}
