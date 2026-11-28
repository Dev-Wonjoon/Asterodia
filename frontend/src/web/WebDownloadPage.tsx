import { MediaFormatFields, useOutputFormats } from '../features/mediaedit/MediaFormatFields'
import { useEffect, useRef, useState } from 'react'
import type { DownloadJobDto, DownloadJobOptions, DownloadPageDto, DownloadResponse } from '@contracts/download-api'
import { translate, translateError, translateMessage, useLanguage } from '@renderer/shared/i18n'
import { DownloadProgress } from '../features/download/components/DownloadProgress'
import { canCancelDownload, downloadStatus, downloadTrigger } from '../features/download/downloadPresentation'
import { isHttpUrl } from '../shared/utils/url'
import { webDownloads } from './download-api'

const control = 'rounded-lg border border-[var(--border-line)] bg-[var(--bg-app)] px-3 py-2 text-sm disabled:opacity-40'
const empty: DownloadPageDto = { data: [], activeJobs: [], limit: 50, offset: 0, hasNextPage: false }
function unwrap<T>(result: DownloadResponse<T>): T { if (!result.ok) throw new Error(translateError(result.error)); return result.data }

export function WebDownloadPage({ onGallery }: { onGallery(): void }) {
    useLanguage()
    const [url, setURL] = useState(''), [options, setOptions] = useState<DownloadJobOptions>({ type: 'video', quality: 'best', playlist: false })
    const [enabled, setEnabled] = useState(false), [checked, setChecked] = useState(false), [page, setPage] = useState(empty)
    const [offset, setOffset] = useState(0), [refresh, setRefresh] = useState(0), [error, setError] = useState(''), [notice, setNotice] = useState(''), [busy, setBusy] = useState('')
    const { formats, error: formatError } = useOutputFormats(enabled, webDownloads.formats)
    const [readError, setReadError] = useState('')
    const mounted = useRef(false), locked = useRef(false), readRevision = useRef(0)
    useEffect(() => { mounted.current = true; return () => { mounted.current = false } }, [])
    useEffect(() => {
        let disposed = false, timer: ReturnType<typeof setTimeout>
        const own = ++readRevision.current
        async function poll() {
            try {
                const access = await webDownloads.status()
                if (disposed || own !== readRevision.current) return
                setEnabled(access.enabled); setChecked(true)
                if (access.enabled) {
                    const value = unwrap(await webDownloads.list(offset))
                    if (!disposed && own === readRevision.current) { setPage(value); setReadError('') }
                } else setPage(empty)
            } catch (failure) { if (!disposed && own === readRevision.current) { setChecked(true); setReadError(failure instanceof Error ? failure.message : translate('common.couldNotConnectToTheServerTryAgainShortly')) } }
            finally { if (!disposed) timer = setTimeout(() => void poll(), 2000) }
        }
        void poll()
        return () => { disposed = true; clearTimeout(timer) }
    }, [offset, refresh])

    async function perform(id: string, action: () => Promise<DownloadResponse<unknown>>) {
        if (locked.current || !enabled) return false
        locked.current = true; setBusy(id); setError(''); setNotice('')
        // Invalidate a poll started before this mutation, without cancelling the
        // accepted PC job if this view is closed.
        ++readRevision.current
        try {
            unwrap(await action())
            if (!mounted.current) return true
            setOffset(0); setRefresh(value => value + 1)
            return true
        } catch (failure) {
            if (mounted.current) setError(failure instanceof Error ? failure.message : translate('common.couldNotProcessTheDownloadRequest'))
            return false
        } finally {
            locked.current = false
            if (mounted.current) { setBusy(''); setRefresh(value => value + 1) }
        }
    }
    const jobs = [...new Map([...(page.activeJobs ?? []), ...page.data].map(job => [job.id, job])).values()]
    return <section className="h-full overflow-auto p-4 sm:p-6" aria-label={translate('navigation.downloads')}>
        <div className="mx-auto max-w-4xl space-y-5">
            <header className="flex flex-wrap items-center justify-between gap-3"><h1 className="text-lg font-semibold">{translate('navigation.downloads')}</h1><button type="button" className={control} disabled={!!busy} onClick={() => setRefresh(value => value + 1)}>{translate('common.refresh')}</button></header>
            <p className="text-sm text-[var(--text-muted)]">{translate('web.download.help')}</p>
            {!checked && <p role="status">{translate('settings.checkingStatus')}</p>}
            {checked && !enabled && <p role="status" className="rounded-xl border border-[var(--border-line)] p-4">{translate('web.download.permission')}</p>}
            <form className="space-y-3 rounded-xl border border-[var(--border-line)] p-4" onSubmit={event => {
                event.preventDefault(); const value = url.trim()
                if (!isHttpUrl(value)) { setError(translate('common.checkTheDownloadUrlAndOptions')); return }
                void perform('new', () => webDownloads.start(value, options)).then(success => { if (success && mounted.current) { setURL(''); setNotice(translate('web.download.accepted')) } })
            }}>
                <label className="block space-y-2"><span>{translate('download.downloadUrl')}</span><input type="url" required value={url} disabled={!enabled || !!busy} onChange={event => setURL(event.target.value)} placeholder={translate('download.pasteADownloadLink')} className={control + ' w-full'} /></label>
                <div className="flex flex-wrap items-end gap-3">
                    <label className="grid gap-1 text-sm">{translate('download.type')}<select aria-label={translate('download.downloadType')} className={control} disabled={!enabled || !!busy} value={options.type} onChange={event => setOptions({ type: event.target.value === 'audio' ? 'audio' : 'video', quality: event.target.value === 'audio' ? 'audio' : 'best', playlist: options.playlist, extension: event.target.value === 'audio' ? 'mp3' : undefined })}><option value="video">{translate('media.video')}</option><option value="audio">{translate('conversion.audio')}</option></select></label>
                    {options.type !== 'audio' && <>
                        <label className="grid gap-1 text-sm">{translate('download.quality')}<select aria-label={translate('download.downloadQuality')} className={control} disabled={!enabled || !!busy} value={options.quality} onChange={event => setOptions({ ...options, quality: event.target.value as DownloadJobOptions['quality'] })}>{['best', '8k', '4k', '2k', '1080', '720', '480'].map(value => <option key={value} value={value}>{value === 'best' ? translate('download.bestQuality') : value}</option>)}</select></label>
                    </>}
                    <fieldset disabled={!enabled || !!busy} className="grid grid-cols-2 items-end gap-3 sm:grid-cols-3">
                        <MediaFormatFields value={{ type: options.type === 'audio' ? 'audio' : 'video', extension: options.extension, videoCodec: options.videoCodec, audioCodec: options.audioCodec }}
                            formats={formats.length ? formats : [
                                { type: 'video', extension: 'mp4', videoCodecs: [], audioCodecs: [] },
                                { type: 'video', extension: 'mkv', videoCodecs: [], audioCodecs: [] },
                                { type: 'audio', extension: 'mp3', videoCodecs: [], audioCodecs: [] },
                            ]} auto containerLabel={translate('download.downloadFormat')}
                            onChange={format => setOptions({ ...options, extension: format.extension, videoCodec: format.videoCodec, audioCodec: format.audioCodec })} />
                    </fieldset>
                    <label className="flex min-h-10 items-center gap-2 text-sm"><input type="checkbox" disabled={!enabled || !!busy} checked={!!options.playlist} onChange={event => setOptions({ ...options, playlist: event.target.checked })} />{translate('download.includePlaylist')}</label>
                    <button type="submit" className={control + ' bg-[var(--color-primary)] font-semibold'} disabled={!enabled || !!busy || !url.trim()}>{busy === 'new' ? translate('download.processing') : translate('web.download.start')}</button>
                </div>
                {formatError && <p className="text-xs text-[var(--text-muted)]">{translate('conversion.additionalFormatsNeedFFmpeg')}</p>}
                <p className="text-xs text-[var(--text-muted)]">{translate('download.instagramSavesPhotosAndVideosTogetherInTheirOriginal')}</p>
            </form>
            {(error || readError) && <p role="alert" className="break-words text-sm text-red-500">{translateMessage(error || readError)}</p>}
            {notice && <p role="status" className="text-sm">{translateMessage(notice)}</p>}
            <div className="flex flex-wrap items-center justify-between gap-3"><h2 className="font-medium">{translate('web.download.jobs')}</h2><button type="button" className={control} onClick={onGallery}>{translate('web.download.gallery')}</button></div>
            {enabled && !jobs.length && <p className="text-sm text-[var(--text-muted)]">{translate('web.download.empty')}</p>}
            <div className="space-y-3">{jobs.map((job: DownloadJobDto) => <article key={job.id} className="space-y-3 rounded-xl border border-[var(--border-line)] p-4">
                <div className="flex flex-wrap justify-between gap-2"><h3 className="min-w-0 break-words font-medium">{job.title || job.url}</h3><span className="text-sm" role="status">{downloadStatus(job)}</span></div>
                {job.title && <p className="break-all text-xs text-[var(--text-muted)]">{job.url}</p>}
                <DownloadProgress job={job} />
                {(job.errorCode || job.errorMessage) && <p className="break-words text-sm text-red-500">{translateError({ code: job.errorCode || 'DOWNLOAD_ENGINE_FAILED', message: job.errorMessage || '', context: {} })}</p>}
                <div className="flex flex-wrap gap-2">
                    {canCancelDownload(job) && <button type="button" className={control} disabled={!!busy || !enabled} onClick={() => void perform(job.id, () => webDownloads.cancel(job.id))}>{translate('common.cancel')}</button>}
                    {!job.historyOnly && job.options && !job.cleanupRequired && downloadTrigger(job) && job.state !== 'pending' && <button type="button" className={control} disabled={!!busy || !enabled} onClick={() => void perform(job.id, () => webDownloads.retry(job.id))}>{translate('common.retry')}</button>}
                    {job.cleanupRequired && <button type="button" className={control} disabled={!!busy || !enabled} onClick={() => void perform(job.id, () => webDownloads.cleanup(job.id))}>{translate('download.cleanUpTemporaryFiles')}</button>}
                </div>
            </article>)}</div>
            {enabled && <nav className="flex justify-between gap-3" aria-label={translate('navigation.downloads')}><button type="button" className={control} disabled={!!busy || offset === 0} onClick={() => setOffset(Math.max(0, offset - 50))}>{translate('gallery.previousFiles')}</button><button type="button" className={control} disabled={!!busy || !page.hasNextPage} onClick={() => setOffset(offset + 50)}>{translate('gallery.nextFiles')}</button></nav>}
        </div>
    </section>
}
