import type { DownloadJobDto } from '@contracts/download-api'
import { ProgressBar } from '@renderer/shared/components/ui/ProgressBar'
import { downloadProgressView } from '../downloadPresentation'

export function DownloadProgress({ job }: { job: DownloadJobDto }) {
    if (job.state !== 'running' || job.phase === 'finished') return null
    const view = downloadProgressView(job)
    return <div className="space-y-1" aria-label="미디어 다운로드 진행 상태">
        <ProgressBar label={job.transfer && job.transfer.fileCount > 1 ? '현재 파일 다운로드 진행률' : '다운로드 진행률'} value={view.percent} />
        {(view.percentLabel || view.detail) && <div role="status" className="flex flex-wrap justify-between gap-x-3 gap-y-1 text-xs text-[var(--text-muted)]">
            {view.percentLabel && <span>{view.percentLabel}</span>}
            {view.detail && <span className="min-w-0 break-words">{view.detail}</span>}
        </div>}
    </div>
}
