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
    get ffprobe() { return translate("FFprobe · 미디어 정보 검사") },
    get imagemagick() { return translate("ImageMagick · 이미지 검사 및 미리보기") }
}

export const EngineSettings = (): React.JSX.Element => {
    useLanguage()
    const installer = useEngineSettingsViewModel()
    const { engines, progress, busy, loading, statusError, refreshStatus } = installer
    useLeaveGuard(false, busy)

    if (loading) {
        return (
            <div className="flex items-center justify-center h-32 text-[var(--text-muted)] text-sm">{translate("엔진 상태 확인 중...")}</div>
        )
    }

    return (
        <SettingSection title={translate("엔진 관리")} description={translate("다운로드에 사용되는 외부 도구를 관리합니다.")}>
            {statusError && <div role="alert" className="px-4 py-3 text-sm text-red-500">{translateMessage(statusError)} <button type="button" onClick={() => void refreshStatus()}>{translate("다시 확인")}</button></div>}
            <EngineInstallActions installer={installer} />
            {Object.entries(engines).map(([name, status]) => (
                <div key={name}>
                    <SettingRow
                        label={ENGINE_LABELS[name] || name}
                        value={status.installed ? status.version || translate("버전 확인 불가") : translate("미설치")}
                        status={status.installed ? 'success' : 'error'}
                    />
                    <EngineInstallProgress name={name} progress={progress[name]} />
                </div>
            ))}
        </SettingSection>
    )
}
