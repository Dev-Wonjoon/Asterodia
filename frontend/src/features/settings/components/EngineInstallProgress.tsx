import { translateMessage, translate, useLanguage } from '@renderer/shared/i18n'
import type { EngineInstallProgress as Progress } from '@contracts/types'
import { ProgressBar } from '@renderer/shared/components/ui/ProgressBar'
import { installProgressView, batchProgressView } from '../engineInstall'
import type { useEngineSettingsViewModel } from '../useEngineSettingsViewModel'
import { Button } from '@renderer/shared/components/ui/Button'

export function EngineInstallActions({ installer }: { installer: ReturnType<typeof useEngineSettingsViewModel> }) {
    useLanguage()
    const { batch, busy, installing, allInstalled, loading, statusError, installAll, retryFailed } = installer
    const failed = !!batch?.failed.length
    const view = batch ? batchProgressView(batch) : null
    return <div className="space-y-3 px-4 py-3">
        <div className="flex flex-wrap items-center justify-between gap-3">
            <p className="text-sm text-[var(--text-muted)]">{translate("settings.downloadAndInstallAllRequiredExternalToolsWithOne")}</p>
            <Button size="md" onClick={() => void (failed ? retryFailed() : installAll())}
                disabled={busy || loading || statusError !== null || allInstalled && !failed}>
                {busy ? translate("settings.installing") : failed ? translate("settings.retryFailedTools") : allInstalled ? translate("settings.installationCompleted") : translate("settings.install")}
            </Button>
        </div>
        {view && <div className="space-y-2" aria-label={translate("settings.externalToolInstallation")} aria-busy={busy}>
            <ProgressBar label={translate("settings.overallInstallationProgress")} value={view.percent} failed={!busy && failed} />
            <p role="status" className="text-xs text-[var(--text-muted)]">{view.label}</p>
        </div>}
        {busy && <p role="status" className="text-xs text-[var(--text-muted)]">{translate("settings.engine.installingNamed", { engine: installing ?? '' })} {translate("settings.keepThisScreenOpenUntilInstallationFinishes")}</p>}
    </div>
}

export function EngineInstallProgress({ name, progress }: { name: string; progress?: Progress }) {
    useLanguage()
    if (!progress) return null
    const view = installProgressView(progress)
    return <div className="space-y-1 px-4 pb-3" aria-label={translate("settings.installationStatus", { engine: name })}>
        <ProgressBar label={translate("settings.downloadProgress", { engine: name })} value={view.percent} failed={progress.phase === 'failed'} />
        <div className="flex flex-wrap justify-between gap-x-3 text-xs text-[var(--text-muted)]">
            <span role="status">{view.label}{view.percent === undefined ? '' : ` · ${view.percent.toFixed(1)}%`}</span>
            <span>{view.detail}</span>
        </div>
        {progress.error && <p role="alert" className="break-words text-xs text-red-500">{translateMessage(progress.error)}</p>}
    </div>
}
