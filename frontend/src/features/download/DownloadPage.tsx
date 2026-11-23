import { translateMessage, translate, useLanguage } from '@renderer/shared/i18n'
import { useEffect } from 'react'
import { ListDetailLayout } from '@renderer/shared/components/layout/ListDetailLayout'
import { useEngineSettingsViewModel } from '@renderer/features/settings/useEngineSettingsViewModel'
import { EngineInstallActions, EngineInstallProgress } from '@renderer/features/settings/components/EngineInstallProgress'
import { useLeaveGuard } from '@renderer/shared/hooks/useLeaveGuard'
import { useDownloadViewModel } from './useDownloadViewModel'
import { DownloadQueue } from './components/DownloadQueue'
import { DownloadDetailPane } from './components/DownloadDetailPane'

export function DownloadPage(): React.JSX.Element {
    useLanguage()
    const vm = useDownloadViewModel()
    const { closeDetail } = vm
    useEffect(() => () => closeDetail(), [closeDetail])
    const engine = useEngineSettingsViewModel()
    useLeaveGuard(false, engine.busy)
    const missing = Object.entries(engine.engines).filter(([, status]) => !status.installed)
    return <ListDetailLayout detailLabel={translate("다운로드 상세")} detailKey={vm.detail.jobId} onClose={vm.closeDetail}
        detail={vm.detail.jobId !== null ? <DownloadDetailPane /> : undefined}>
        <div className="space-y-5">
            <header className="flex flex-wrap items-center justify-between gap-3">
                <div><h1 className="text-lg font-semibold">{translate("다운로드")}</h1>
                    <p className="mt-1 text-sm text-[var(--text-muted)]">{translate("작업 상태를 확인하고 취소하거나 다시 실행하세요.")}</p></div>
                <div className="flex flex-wrap gap-2">
                <button type="button" title={translate("모든 페이지의 실패·취소 요청을 기존 옵션으로 한 번씩 재시도합니다.")} disabled={!vm.ready || Boolean(vm.halted) || vm.loading || vm.batchRetry?.running} onClick={() => void vm.retryAll()}
                    className="rounded-lg border border-[var(--border-line)] px-3 py-2 text-sm disabled:opacity-40">{vm.batchRetry?.running ? translate("일괄 재시도 중…") : translate("실패·취소 일괄 재시도")}</button>
                <button type="button" disabled={vm.loading} onClick={() => void vm.refresh()}
                    className="rounded-lg border border-[var(--border-line)] px-3 py-2 text-sm disabled:opacity-40">{vm.loading ? translate("조회 중") : translate("새로고침")}</button>
                </div>
            </header>
            {vm.batchRetry && <p role="status" className="text-xs text-[var(--text-muted)]">
                {vm.batchRetry.running && vm.batchRetry.total === 0 ? translate("전체 페이지의 재시도 대상을 확인하고 있습니다.") : translate("일괄 재시도 {0} · 처리 {1}/{2}건 · 실패 {3}건 · 제외 {4}건", { "0": vm.batchRetry.running ? translate("진행 중") : translate("종료"), "1": vm.batchRetry.processed, "2": vm.batchRetry.total, "3": vm.batchRetry.failed, "4": vm.batchRetry.skipped })}
            </p>}
            {(missing.length > 0 || engine.statusError || engine.batch) && <section aria-label={translate("필요한 엔진 설치")} aria-busy={engine.busy}
                className="space-y-3 rounded-xl border border-[var(--border-line)] p-4 text-sm">
                <h2 className="font-medium">{translate("필요한 엔진 설치")}</h2>
                {engine.statusError && <p role="alert" className="text-red-500">{translateMessage(engine.statusError)} <button type="button" className="underline" onClick={() => void engine.refreshStatus()}>{translate("다시 확인")}</button></p>}
                <EngineInstallActions installer={engine} />
                {Object.entries(engine.engines).filter(([name, status]) => !status.installed || engine.batch?.names.includes(name)).map(([name, status]) => <div key={name}>
                    <div className="flex items-center justify-between gap-4 px-4 py-2">
                        <div className="min-w-0"><p className="font-medium">{name}</p><p className="mt-1 break-words text-xs text-[var(--text-muted)]">{status.installed ? status.version || translate('설치됨') : status.error || translate("설치가 필요합니다.")}</p></div>
                    </div>
                    <EngineInstallProgress name={name} progress={engine.progress[name]} />
                </div>)}
            </section>}
            <DownloadQueue />
        </div>
    </ListDetailLayout>
}
