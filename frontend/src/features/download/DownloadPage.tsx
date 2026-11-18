import { useEffect } from 'react'
import { ListDetailLayout } from '@renderer/shared/components/layout/ListDetailLayout'
import { useEngineSettingsViewModel } from '@renderer/features/settings/useEngineSettingsViewModel'
import { EngineInstallProgress } from '@renderer/features/settings/components/EngineInstallProgress'
import { useDownloadViewModel } from './useDownloadViewModel'
import { DownloadQueue } from './components/DownloadQueue'
import { DownloadDetailPane } from './components/DownloadDetailPane'

export function DownloadPage(): React.JSX.Element {
    const vm = useDownloadViewModel()
    const { closeDetail } = vm
    useEffect(() => () => closeDetail(), [closeDetail])
    const engine = useEngineSettingsViewModel()
    const missing = Object.entries(engine.engines).filter(([, status]) => !status.installed)
    return <ListDetailLayout detailLabel="다운로드 상세" detailKey={vm.detail.jobId} onClose={vm.closeDetail}
        detail={vm.detail.jobId !== null ? <DownloadDetailPane /> : undefined}>
        <div className="space-y-5">
            <header className="flex flex-wrap items-center justify-between gap-3">
                <div><h1 className="text-lg font-semibold">다운로드</h1>
                    <p className="mt-1 text-sm text-[var(--text-muted)]">작업 상태를 확인하고 취소하거나 다시 실행하세요.</p></div>
                <div className="flex flex-wrap gap-2">
                <button type="button" title="모든 페이지의 실패·취소 요청을 기존 옵션으로 한 번씩 재시도합니다." disabled={!vm.ready || Boolean(vm.halted) || vm.loading || vm.batchRetry?.running} onClick={() => void vm.retryAll()}
                    className="rounded-lg border border-[var(--border-line)] px-3 py-2 text-sm disabled:opacity-40">{vm.batchRetry?.running ? '일괄 재시도 중…' : '실패·취소 일괄 재시도'}</button>
                <button type="button" disabled={vm.loading} onClick={() => void vm.refresh()}
                    className="rounded-lg border border-[var(--border-line)] px-3 py-2 text-sm disabled:opacity-40">{vm.loading ? '조회 중' : '새로고침'}</button>
                </div>
            </header>
            {vm.batchRetry && <p role="status" className="text-xs text-[var(--text-muted)]">
                {vm.batchRetry.running && vm.batchRetry.total === 0 ? '전체 페이지의 재시도 대상을 확인하고 있습니다.' : `일괄 재시도 ${vm.batchRetry.running ? '진행 중' : '종료'} · 처리 ${vm.batchRetry.processed}/${vm.batchRetry.total}건 · 실패 ${vm.batchRetry.failed}건 · 제외 ${vm.batchRetry.skipped}건`}
            </p>}
            {(missing.length > 0 || engine.statusError) && <section aria-label="필요한 엔진 설치" aria-busy={engine.busy}
                className="space-y-3 rounded-xl border border-[var(--border-line)] p-4 text-sm">
                <h2 className="font-medium">필요한 엔진 설치</h2>
                {engine.statusError && <p role="alert" className="text-red-500">{engine.statusError} <button type="button" className="underline" onClick={() => void engine.refreshStatus()}>다시 확인</button></p>}
                {missing.map(([name, status]) => <div key={name}>
                    <div className="flex items-center justify-between gap-4 px-4 py-2">
                        <div className="min-w-0"><p className="font-medium">{name}</p><p className="mt-1 break-words text-xs text-[var(--text-muted)]">{status.error || '설치가 필요합니다.'}</p></div>
                        <button type="button" disabled={engine.busy} onClick={() => void engine.install(name)}
                            className="shrink-0 rounded-lg bg-[var(--color-primary)] px-3 py-2 text-xs text-white disabled:opacity-40">{engine.installing === name ? '설치 중…' : '설치'}</button>
                    </div>
                    <EngineInstallProgress name={name} progress={engine.progress[name]} />
                </div>)}
            </section>}
            <DownloadQueue />
        </div>
    </ListDetailLayout>
}
