import { translateMessage, getLocale, translate, useLanguage } from '@renderer/shared/i18n'
import type { LocalImportFileFilter } from '@contracts/local-import-api'
import { canResume, importProgress, states } from './presentation'
import { useLocalImportViewModel } from './useLocalImportViewModel'
import { X } from 'lucide-react'
import { formatBytes } from '@renderer/shared/utils/formatBytes'
import { ProgressBar } from '@renderer/shared/components/ui/ProgressBar'
import { useEffect, useImperativeHandle, type Ref } from 'react'

export interface LocalImportControls { choose: () => void; reopen: () => void }

export function LocalImportPanel({ disabled, onComplete, compact = false, controlRef, onBusyChange, onHasResultChange }: { disabled: boolean; onComplete: () => void; compact?: boolean; controlRef?: Ref<LocalImportControls>; onBusyChange?: (busy: boolean) => void; onHasResultChange?: (available: boolean) => void }) {
    useLanguage()
    const vm = useLocalImportViewModel(onComplete)
    const job = vm.page?.job
    const progress = job ? importProgress(job) : undefined
    const running = job?.state === 'running' || job?.state === 'scanning'
    const stopping = job?.pauseRequested || job?.cancelRequested
    const button = 'rounded-lg border border-[var(--border-line)] px-3 py-2 text-sm disabled:opacity-40'
    const number = (n: number) => n.toLocaleString(getLocale())
    useImperativeHandle(controlRef, () => ({ choose: () => { if (!disabled && !vm.busy) void vm.choose() }, reopen: vm.reopen }))
    useEffect(() => { onBusyChange?.(vm.busy) }, [vm.busy, onBusyChange])
    useEffect(() => { onHasResultChange?.(!!job) }, [!!job, onHasResultChange])
    if (compact && !vm.error && (vm.dismissed || (!job && !vm.busy))) return null
    if (vm.dismissed) return <section aria-label={translate("localImport.importLocalFolder")} className="flex flex-wrap items-center gap-2">
        <button type="button" className={button} disabled={disabled || vm.busy} onClick={() => void vm.choose()}>{translate("gallery.importFolder")}</button>
        <button type="button" className={button} onClick={vm.reopen}>{translate("localImport.recentResults")}</button>
        {vm.error && <p role="alert" className="text-sm text-red-500">{translateMessage(vm.error)}</p>}
    </section>
    return <section aria-label={translate("localImport.importLocalFolder")} className="space-y-3 rounded-xl border border-[var(--border-line)] bg-[var(--bg-sidebar)] p-4">
        <div className="flex flex-wrap items-center justify-between gap-3">
            <div><h2 className="font-medium">{translate("localImport.importLocalFolder")}</h2><p className="mt-1 text-xs text-[var(--text-muted)]">{translate("localImport.registerImagesVideosAndAudioInSubfoldersOriginalsAre")}</p></div>
            <div className="flex items-center gap-2">
                <button type="button" className={button} disabled={disabled || vm.busy} onClick={() => void vm.choose()}>{translate("gallery.selectFolder")}</button>
                {vm.canDismiss && <button type="button" aria-label={translate("localImport.closeImportResults")} title={translate("localImport.closeImportResults")} onClick={vm.dismiss}
                    className="rounded-lg p-2 text-[var(--text-muted)] hover:bg-[var(--bg-hover)]"><X size={18} aria-hidden="true" /></button>}
            </div>
        </div>
        {vm.error && <p role="alert" className="text-sm text-red-500">{translateMessage(vm.error)}</p>}
        {job && <>
            <p className="break-all text-sm">{job.root}</p>
            <div role="status" aria-live="polite" className="space-y-1 text-sm text-[var(--text-muted)]">
                <p>{stopping && running ? translate("localImport.stopping") : states[job.state] ?? job.state}</p>
                <p>{job.scanComplete ? translate("localImport.totalProcessed", { total: number(job.counts.discovered), processed: number(job.counts.processed), percent: (progress ?? 0).toFixed(1) }) : translate("localImport.foundCountingTotal", { count: number(job.counts.discovered) })}</p>
            </div>
            <ProgressBar label={translate("localImport.folderImportProgress")} value={progress} />
            <div className="space-y-1 text-xs text-[var(--text-muted)]">
                <p>{translate("localImport.newUpdatedExistingFailedExcluded", { added: number(job.counts.added), updated: number(job.counts.updated), existing: number(job.counts.unchanged), failed: number(job.counts.failed), excluded: number(job.counts.userExcluded) })}</p>
                <p>{translate("localImport.processedSFilesSAverage", { processedSize: formatBytes(job.counts.processedBytes), totalSize: formatBytes(job.counts.totalBytes), bytesPerSecond: formatBytes(job.bytesPerSecond), filesPerSecond: job.filesPerSecond.toFixed(1) })}</p>
                <p>{translate("localImport.skippedScanErrorsCompletedFoldersPartiallyFailedFolders", { skipped: number(job.counts.scanExcluded), scanErrors: number(job.counts.scanErrors), completed: number(job.counts.completedFolders), failed: number(job.counts.failedFolders) })}</p>
                {job.currentPath && <p className="break-all">{translate("localImport.current", { path: job.currentPath })}</p>}
                <p>{translate("localImport.workContinuesWhenYouSwitchViewsCompletedFilesAre")}</p>
            </div>
            {job.error && <p role="alert" className="break-words text-sm text-red-500">{translateMessage(job.error)}</p>}
            {job.largeFolders > 0 && <p role="alert" className="text-sm text-amber-600">{translate("localImport.foldersContainOver50000FilesProcessingMayTake", { count: number(job.largeFolders) })}</p>}
            {!job.canResume && <p className="text-sm text-[var(--text-muted)]">{translate("localImport.thisJobUsesAnOlderFormatScanTheSame")}</p>}
            {job.state === 'ready' && <p className="text-xs text-[var(--text-muted)]">{translate("localImport.reviewTheListBeforeRegisteringImageChecksRequireImagemagick")}</p>}
            <div className="flex flex-wrap gap-2">
                {job.state === 'ready' && job.canResume && <button type="button" className={button} disabled={disabled || vm.requesting} onClick={() => void vm.run()}>{translate("localImport.startRegistration")}</button>}
                {['scanning', 'ready', 'running'].includes(job.state) && <button type="button" className={button} disabled={vm.requesting || stopping} onClick={() => void vm.pause()}>{translate("gallery.stop")}</button>}
                {canResume(job) && <button type="button" className={button} disabled={disabled || vm.requesting} onClick={() => void vm.resume()}>{job.scanComplete ? translate("gallery.resume") : translate("localImport.resumeScanning")}</button>}
                {!vm.busy && job.canResume && job.counts.failed > 0 && <>
                    <button type="button" className={button} disabled={disabled} onClick={() => void vm.retry()}>{translate("localImport.retryFailedFiles")}</button>
                    <button type="button" className={button} disabled={disabled} onClick={() => void vm.exclude()}>{translate("localImport.excludeAllFailedFiles")}</button>
                </>}
                {!vm.busy && <button type="button" className={button} disabled={disabled} onClick={() => void vm.rescan()}>{translate("localImport.scanTheSameFolderAgain")}</button>}
                <button type="button" className={button} disabled={vm.requesting} onClick={() => void vm.refresh()}>{translate("gallery.refreshStatus")}</button>
                <button type="button" className={button} aria-expanded={vm.showFiles} onClick={() => vm.setShowFiles(!vm.showFiles)}>{translate("localImport.perFileResults", { action: vm.showFiles ? translate("gallery.collapse") : translate("localImport.view") })}</button>
            </div>
            {vm.showFiles && <div className="space-y-2 rounded border border-[var(--border-line)] p-3">
                <label className="text-sm">{translate("localImport.fileStatus")}<select aria-label={translate("localImport.filter.fileStatus")} className="rounded bg-[var(--bg-sidebar)] p-1" value={vm.fileState} onChange={e => vm.setFileState(e.target.value as LocalImportFileFilter)}>
                    <option value="failed">{translate("download.failed")}</option><option value="pending">{translate("localImport.awaitingRegistration")}</option><option value="excluded">{translate("localImport.excludedByUser")}</option><option value="">{translate("gallery.all")}</option>
                </select></label>
                <p className="text-xs text-[var(--text-muted)]">{translate("localImport.stopTheRunningJobToExcludeOrRetryFiles")}</p>
                {vm.fileError && <p role="alert" className="text-sm text-red-500">{translateMessage(vm.fileError)}</p>}
                {!vm.filePage && !vm.fileError && <p className="text-sm">{translate("localImport.loadingFiles")}</p>}
                {vm.filePage?.total === 0 && <p className="text-sm">{translate("localImport.noMatchingFiles")}</p>}
                <ul aria-label={translate("localImport.perFileImportResults")} className="max-h-72 space-y-2 overflow-y-auto text-sm">
                    {vm.filePage?.files.map(file => <li key={file.id} className="space-y-1 border-b border-[var(--border-line)] pb-2">
                        <p className="break-all">{file.path} · {states[file.state] ?? file.state}</p>
                        {file.error && <p className="break-words text-red-500">{translateMessage(file.error)}</p>}
                        <div className="flex gap-2">
                            {['failed', 'excluded'].includes(file.state) && <button type="button" className={button} disabled={disabled || vm.busy} onClick={() => void vm.retry(file.id)}>{translate("download.retry")}</button>}
                            {['pending', 'failed'].includes(file.state) && <button type="button" className={button} disabled={disabled || vm.busy} onClick={() => void vm.exclude(file.id)}>{translate("localImport.excludeFromThisJob")}</button>}
                        </div>
                    </li>)}
                </ul>
                {(vm.filePage?.total ?? 0) > 25 && <div className="flex items-center gap-3 text-sm">
                    <button type="button" className={button} disabled={vm.fileOffset === 0 || vm.requesting} onClick={() => vm.setFileOffset(Math.max(0, vm.fileOffset - 25))}>{translate("localImport.previousFiles")}</button>
                    <span>{vm.fileOffset + 1}–{vm.fileOffset + (vm.filePage?.files.length ?? 0)} / {vm.filePage?.total}</span>
                    <button type="button" className={button} disabled={!vm.filePage?.hasNextPage || vm.requesting} onClick={() => vm.setFileOffset(vm.fileOffset + 25)}>{translate("localImport.nextFiles")}</button>
                </div>}
            </div>}
            <ul className="max-h-64 space-y-2 overflow-y-auto text-sm" aria-label={translate("localImport.perFolderImportResults")}>
                {vm.page?.folders.map(folder => <li key={folder.id} className="rounded border border-[var(--border-line)] px-3 py-2">
                    <p className="break-all">{translate("localImport.files", { path: folder.path === '.' ? translate("localImport.selectedFolder") : folder.path, files: number(folder.fileCount), state: states[folder.state] ?? folder.state })}</p>
                    {folder.error && <p className="mt-1 break-words text-red-500">{translateMessage(folder.error)}</p>}
                </li>)}
            </ul>
            {(vm.page?.total ?? 0) > 25 && <div className="flex items-center gap-3 text-sm">
                <button type="button" className={button} disabled={vm.offset === 0 || vm.requesting} onClick={() => vm.goTo(Math.max(0, vm.offset - 25))}>{translate("download.previous")}</button>
                <span>{vm.offset + 1}–{vm.offset + (vm.page?.folders.length ?? 0)} / {vm.page?.total}</span>
                <button type="button" className={button} disabled={!vm.page?.hasNextPage || vm.requesting} onClick={() => vm.goTo(vm.offset + 25)}>{translate("download.next")}</button>
            </div>}
        </>}
    </section>
}
