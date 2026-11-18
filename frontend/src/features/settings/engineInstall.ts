import type { EngineInstallProgress } from '@contracts/types'
import { formatBytes } from '@renderer/shared/utils/formatBytes'

const labels: Record<string, string> = {
    preparing: '설치 준비 중', downloading: '다운로드 중', extracting: '압축 해제 중',
    verifying: '실행 파일 확인 중', committing: '설치 파일 반영 중', completed: '설치 완료', failed: '설치 실패',
}
export function mergeInstallProgress(previous: EngineInstallProgress | undefined, next: EngineInstallProgress): EngineInstallProgress {
    if (!previous || previous.installId !== next.installId) return next
    if (previous.phase === 'completed' || previous.phase === 'failed') return previous
    return {
        ...next,
        downloaded: Math.max(previous.downloaded, next.downloaded),
        total: next.total ?? previous.total,
    }
}
export function installProgressView(progress: EngineInstallProgress) {
    const downloaded = Number.isFinite(progress.downloaded) ? Math.max(0, progress.downloaded) : 0
    const total = progress.total !== null && Number.isFinite(progress.total) && progress.total > 0 ? progress.total : null
    const percent = progress.phase === 'completed' ? 100 : total === null ? undefined : Math.min(100, downloaded / total * 100)
    return {
        label: labels[progress.phase] ?? '설치 중',
        percent: progress.phase === 'failed' && percent === undefined ? 0 : percent,
        detail: total === null ? `${formatBytes(downloaded)} 다운로드` : `${formatBytes(downloaded)} / ${formatBytes(total)}`,
    }
}
