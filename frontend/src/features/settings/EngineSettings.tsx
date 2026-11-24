import { translateMessage, translate, useLanguage } from '@renderer/shared/i18n'
import { SettingSection } from '@renderer/shared/components/ui/SettingSection'
import { useEngineSettingsViewModel } from '@renderer/features/settings/useEngineSettingsViewModel'
import { SettingRow } from '@renderer/shared/components/ui/SettingRow'
import { EngineInstallActions, EngineInstallProgress } from './components/EngineInstallProgress'
import { useLeaveGuard } from '@renderer/shared/hooks/useLeaveGuard'

const ENGINE_LABELS: Record<string, string> = {
    'yt-dlp': 'yt-dlp',
    'gallery-dl': 'gallery-dl',
    ffmpeg: 'FFmpeg',
    get ffprobe() { return translate("settings.ffprobeMediaInformationInspection") },
    get imagemagick() { return translate("settings.imagemagickImageInspectionAndPreviews") }
}

export const EngineSettings = (): React.JSX.Element => {
    useLanguage()
    const installer = useEngineSettingsViewModel()
    const { engines, progress, busy, loading, statusError, refreshStatus } = installer
    useLeaveGuard(false, busy)

    if (loading) {
        return (
            <div className="flex items-center justify-center h-32 text-[var(--text-muted)] text-sm">{translate("settings.checkingEngineStatus")}</div>
        )
    }

    return (
        <SettingSection title={translate("settings.engineManagement")} description={translate("settings.manageExternalToolsUsedForDownloads")}>
            {statusError && <div role="alert" className="px-4 py-3 text-sm text-red-500">{translateMessage(statusError)} <button type="button" onClick={() => void refreshStatus()}>{translate("download.checkAgain")}</button></div>}
            <EngineInstallActions installer={installer} />
            {Object.entries(engines).map(([name, status]) => (
                <div key={name}>
                    <SettingRow
                        label={ENGINE_LABELS[name] || name}
                        value={status.installed ? status.version || translate("settings.versionUnavailable") : translate("settings.notInstalled")}
                        status={status.installed ? 'success' : 'error'}
                    />
                    <EngineInstallProgress name={name} progress={progress[name]} />
                </div>
            ))}
        </SettingSection>
    )
}
