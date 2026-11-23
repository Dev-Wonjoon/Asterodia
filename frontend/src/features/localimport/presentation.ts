import { translate } from '@renderer/shared/i18n'
import type { LocalImportJob } from '@contracts/local-import-api'

export const states: Record<string, string> = {
    get scanning() { return translate("폴더 탐색 중") }, get ready() { return translate("탐색 완료 · 등록 대기") }, get running() { return translate("등록 중") },
    get completed() { return translate("처리 완료") }, get completed_with_errors() { return translate("처리 완료 · 일부 실패") }, get paused() { return translate("중단됨") },
    get failed() { return translate("오류로 중단됨") }, get cancelled() { return translate("종료됨") }, get interrupted() { return translate("앱 종료로 중단됨") },
    get pending() { return translate("대기") }, get registered() { return translate("등록됨") }, get unchanged() { return translate("기등록") }, get excluded() { return translate("이번 작업에서 제외") },
}
export function importProgress(job: LocalImportJob): number | undefined {
    if (!job.scanComplete) return undefined
    if (job.counts.discovered === 0) return 0
    return Math.max(0, Math.min(100, job.counts.processed / job.counts.discovered * 100))
}
export function canResume(job: LocalImportJob): boolean {
    return job.canResume && ['paused', 'interrupted', 'cancelled', 'failed'].includes(job.state)
}
