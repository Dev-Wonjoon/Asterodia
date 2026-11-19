import type { LocalImportJob } from '@contracts/local-import-api'

export const states: Record<string, string> = {
    scanning: '폴더 탐색 중', ready: '탐색 완료 · 등록 대기', running: '등록 중',
    completed: '처리 완료', completed_with_errors: '처리 완료 · 일부 실패', paused: '중단됨',
    failed: '오류로 중단됨', cancelled: '종료됨', interrupted: '앱 종료로 중단됨',
    pending: '대기', registered: '등록됨', unchanged: '기등록', excluded: '이번 작업에서 제외',
}
export function importProgress(job: LocalImportJob): number | undefined {
    if (!job.scanComplete) return undefined
    if (job.counts.discovered === 0) return 0
    return Math.max(0, Math.min(100, job.counts.processed / job.counts.discovered * 100))
}
export function canResume(job: LocalImportJob): boolean {
    return job.canResume && ['paused', 'interrupted', 'cancelled', 'failed'].includes(job.state)
}
