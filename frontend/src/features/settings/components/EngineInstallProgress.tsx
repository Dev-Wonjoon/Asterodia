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
            <p className="text-sm text-[var(--text-muted)]">{translate('필요한 외부 도구를 한 번에 다운로드하고 설치합니다.')}</p>
            <Button size="md" onClick={() => void (failed ? retryFailed() : installAll())}
                disabled={busy || loading || statusError !== null || allInstalled && !failed}>
                {busy ? translate('설치 중…') : failed ? translate('실패한 도구 재시도') : allInstalled ? translate('설치 완료') : translate('설치')}
            </Button>
        </div>
        {view && <div className="space-y-2" aria-label={translate('외부 도구 설치')} aria-busy={busy}>
            <ProgressBar label={translate('전체 설치 진행률')} value={view.percent} failed={!busy && failed} />
            <p role="status" className="text-xs text-[var(--text-muted)]">{view.label}</p>
        </div>}
        {busy && <p role="status" className="text-xs text-[var(--text-muted)]">{translate('{0} 설치 중…', { '0': installing ?? '' })} {translate('설치가 끝날 때까지 이 화면을 유지해 주세요.')}</p>}
    </div>
}

export function EngineInstallProgress({ name, progress }: { name: string; progress?: Progress }) {
    useLanguage()
    if (!progress) return null
    const view = installProgressView(progress)
    return <div className="space-y-1 px-4 pb-3" aria-label={translate("{0} 설치 진행 상태", { "0": name })}>
        <ProgressBar label={translate("{0} 다운로드 진행률", { "0": name })} value={view.percent} failed={progress.phase === 'failed'} />
        <div className="flex flex-wrap justify-between gap-x-3 text-xs text-[var(--text-muted)]">
            <span role="status">{view.label}{view.percent === undefined ? '' : ` · ${view.percent.toFixed(1)}%`}</span>
            <span>{view.detail}</span>
        </div>
        {progress.error && <p role="alert" className="break-words text-xs text-red-500">{translateMessage(progress.error)}</p>}
    </div>
}
