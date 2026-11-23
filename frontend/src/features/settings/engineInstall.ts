import { translate } from '@renderer/shared/i18n'
import type { EngineInstallProgress } from '@contracts/types'
import { formatBytes } from '@renderer/shared/utils/formatBytes'

const labels: Record<string, string> = {
    get preparing() { return translate("설치 준비 중") }, get downloading() { return translate("다운로드 중") }, get extracting() { return translate("압축 해제 중") },
    get verifying() { return translate("실행 파일 확인 중") }, get committing() { return translate("설치 파일 반영 중") }, get completed() { return translate("설치 완료") }, get failed() { return translate("설치 실패") },
}

export interface EngineInstallBatch {
    names: string[]
    processed: number
    succeeded: number
    failed: string[]
}

export function batchProgressView(batch: EngineInstallBatch) {
    return {
        percent: batch.names.length ? batch.processed / batch.names.length * 100 : 0,
        label: translate('처리 {0}/{1}개 · 완료 {2}개 · 실패 {3}개', {
            '0': batch.processed, '1': batch.names.length, '2': batch.succeeded, '3': batch.failed.length,
        }),
    }
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
        label: labels[progress.phase] ?? translate("설치 중"),
        percent: progress.phase === 'failed' && percent === undefined ? 0 : percent,
        detail: total === null ? translate("{0} 다운로드", { "0": formatBytes(downloaded) }) : `${formatBytes(downloaded)} / ${formatBytes(total)}`,
    }
}
