import { SettingSection } from '@renderer/shared/components/ui/SettingSection'
import { useEngineSettingsViewModel } from '@renderer/features/settings/useEngineSettingsViewModel'
import { SettingRow } from '@renderer/shared/components/ui/SettingRow'
import { Button } from '@renderer/shared/components/ui/Button'
import { EngineInstallProgress } from './components/EngineInstallProgress'

const ENGINE_LABELS: Record<string, string> = {
    'yt-dlp': 'yt-dlp',
    'gallery-dl': 'gallery-dl',
    ffmpeg: 'FFmpeg',
    ffprobe: 'FFprobe · 미디어 정보 검사',
    imagemagick: 'ImageMagick · 이미지 검사 및 미리보기'
}

export const EngineSettings = (): React.JSX.Element => {
    const { engines, progress, installing, busy, loading, statusError, refreshStatus, install } = useEngineSettingsViewModel()

    if (loading) {
        return (
            <div className="flex items-center justify-center h-32 text-[var(--text-muted)] text-sm">
                엔진 상태 확인 중...
            </div>
        )
    }

    return (
        <SettingSection title="엔진 관리" description="다운로드에 사용되는 외부 도구를 관리합니다.">
            {statusError && <div role="alert" className="px-4 py-3 text-sm text-red-500">{statusError} <button type="button" onClick={() => void refreshStatus()}>다시 확인</button></div>}
            {Object.entries(engines).map(([name, status]) => (
                <div key={name}>
                    <SettingRow
                        label={ENGINE_LABELS[name] || name}
                        value={status.installed ? status.version || '버전 확인 불가' : '미설치'}
                        status={status.installed ? 'success' : 'error'}
                        action={
                            <Button
                                onClick={() => install(name)}
                                disabled={busy}
                                className={installing === name ? 'cursor-wait' : ''}
                            >
                                {installing === name
                                    ? '설치 중...'
                                    : status.installed
                                      ? '업데이트'
                                      : '설치'}
                            </Button>
                        }
                    />
                    <EngineInstallProgress name={name} progress={progress[name]} />
                </div>
            ))}
        </SettingSection>
    )
}
