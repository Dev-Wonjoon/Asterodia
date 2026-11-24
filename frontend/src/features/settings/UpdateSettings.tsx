import { getLocale, translate, translateMessage, useLanguage } from '@renderer/shared/i18n'
import { SettingSection } from '@renderer/shared/components/ui/SettingSection'
import { Button } from '@renderer/shared/components/ui/Button'
import { ProgressBar } from '@renderer/shared/components/ui/ProgressBar'
import { formatBytes } from '@renderer/shared/utils/formatBytes'
import { useAppUpdate } from './useAppUpdate'

const labels = {
    get idle() { return translate("settings.automaticallyChecksForNewVersions") }, get unconfigured() { return translate("settings.preparingUpdate") }, get checking() { return translate("settings.checkingForUpdates") },
    get 'up-to-date'() { return translate("settings.youAreUpToDate") }, get available() { return translate("settings.aNewVersionIsAvailableToDownload") },
    get downloading() { return translate("settings.downloadingUpdate") }, get verifying() { return translate("settings.verifyingUpdateFiles") }, get installing() { return translate("settings.preparingToApplyUpdate") },
    get ready() { return translate("settings.theUpdateIsReadyRestartToApplyIt") }, get error() { return translate("settings.couldNotCompleteTheUpdate") },
    get restarting() { return translate("settings.restartingTheAppToApplyTheUpdate") }, get unsupported() { return translate("settings.automaticUpdatesAreNotSupportedInThisEnvironment") },
}

export function UpdateSettings() {
    useLanguage()
    const vm = useAppUpdate()
    if (!vm) return null
    const { status, pending, error, act } = vm
    const working = Boolean(pending || (status && ['checking', 'downloading', 'verifying', 'installing', 'restarting'].includes(status.state)))
    return <SettingSection title={translate("settings.appUpdates")} description={translate("settings.automaticallyChecksForStableReleasesChooseWhenToDownload")}>
        <div className="space-y-3 px-4 py-3 text-sm">
            <p>{translate("settings.currentVersion", { version: status?.currentVersion ?? translate("settings.checking") })}</p>
            <p role="status">{status ? labels[status.state] : translate("settings.checkingUpdateStatus")}</p>
            {status?.version && <p>{translate("settings.newVersion", { version: status.version, size: formatBytes(status.size) })}</p>}
            {(error || status?.error) && <p role="alert" className="break-words text-red-500">{translateMessage(error || status?.error)}</p>}
            {status?.state === 'downloading' && <>
                <ProgressBar label={translate("settings.updateDownloadProgress")} value={status.size > 0 ? Math.min(100, status.written / status.size * 100) : undefined} />
                <p className="text-xs text-[var(--text-muted)]">{formatBytes(status.written)} / {formatBytes(status.size)}</p>
            </>}
            <div className="flex flex-wrap gap-2">
                <Button disabled={!status?.supported || working || status.state === 'ready'} onClick={() => void act('check')}>{translate("settings.checkForUpdates")}</Button>
                {status?.version && status.state !== 'ready' && <Button disabled={working} onClick={() => void act('download')}>{translate("settings.downloadUpdate")}</Button>}
                {status?.state === 'ready' && <Button disabled={working} onClick={() => void act('restart')}>{translate("settings.restartToApply")}</Button>}
            </div>
            {status?.lastChecked ? <p className="text-xs text-[var(--text-muted)]">{translate("settings.lastChecked", { time: new Date(status.lastChecked).toLocaleString(getLocale()) })}</p> : null}
            {status?.notes && <details><summary className="cursor-pointer">{translate("settings.changes")}</summary><pre className="mt-2 max-h-64 overflow-auto whitespace-pre-wrap break-words font-sans text-[var(--text-muted)]">{status.notes}</pre></details>}
            <p className="text-xs text-[var(--text-muted)]">{translate("settings.yourLibraryAndSettingsArePreservedFinishDownloadsImports")}</p>
        </div>
    </SettingSection>
}

export function UpdateNotice({ onOpen }: { onOpen(): void }) {
    useLanguage()
    const vm = useAppUpdate()
    const status = vm?.status
    if (!status || (status.state !== 'available' && status.state !== 'ready')) return null
    return <div role="status" className="flex flex-none items-center justify-between gap-3 border-b border-[var(--border-line)] bg-[var(--bg-sidebar)] px-4 py-2 text-sm">
        <span>StorageBucket {status.version} {status.state === 'ready' ? translate("settings.readyToApplyUpdate") : translate("settings.aNewVersionIsAvailable")}</span>
        <button type="button" className="shrink-0 underline" onClick={onOpen}>{translate("settings.viewUpdate")}</button>
    </div>
}
