import type { EngineInstallProgress as Progress } from '@contracts/types'
import { ProgressBar } from '@renderer/shared/components/ui/ProgressBar'
import { installProgressView } from '../engineInstall'

export function EngineInstallProgress({ name, progress }: { name: string; progress?: Progress }) {
    if (!progress) return null
    const view = installProgressView(progress)
    return <div className="space-y-1 px-4 pb-3" aria-label={`${name} 설치 진행 상태`}>
        <ProgressBar label={`${name} 다운로드 진행률`} value={view.percent} failed={progress.phase === 'failed'} />
        <div className="flex flex-wrap justify-between gap-x-3 text-xs text-[var(--text-muted)]">
            <span role="status">{view.label}{view.percent === undefined ? '' : ` · ${view.percent.toFixed(1)}%`}</span>
            <span>{view.detail}</span>
        </div>
        {progress.error && <p role="alert" className="break-words text-xs text-red-500">{progress.error}</p>}
    </div>
}
