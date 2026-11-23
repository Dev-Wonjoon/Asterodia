import { translateError, translate, useLanguage } from '@renderer/shared/i18n'
import { DOWNLOAD_PAGE_SIZE } from '../downloadStore'
import { useDownloadViewModel } from '../useDownloadViewModel'
import { canDeleteDownload, downloadPlatform, downloadStatus } from '../downloadPresentation'
import { X } from 'lucide-react'
import { DownloadJobActions } from './DownloadJobActions'
import { DownloadThumbnail } from './DownloadThumbnail'
import { DownloadProgress } from './DownloadProgress'

export function DownloadQueue(): React.JSX.Element {
    useLanguage()
    const vm = useDownloadViewModel()
    const button =
        'rounded-lg border border-[var(--border-line)] px-3 py-2 text-sm disabled:opacity-40'
    return (
        <section aria-label={translate("다운로드 작업 목록")} className="space-y-4">
            {vm.error && <p role="alert">{translateError(vm.error)}</p>}
            {vm.halted && <p role="alert">{vm.halted}</p>}
            {!vm.ready && !vm.error && <p role="status">{translate("작업 목록을 불러오는 중...")}</p>}
            {vm.ready && vm.queue.length === 0 && (
                <p className="py-16 text-center text-[var(--text-muted)]">{translate("등록된 작업이 없습니다. 상단에 링크를 붙여넣어 다운로드하세요.")}</p>
            )}
            <ul className="space-y-3">
                {vm.queue.map((job) => {
                    const selected = vm.detail.jobId === job.id
                    return (
                        <li
                            key={job.id}
                            className={
                                'relative flex items-start gap-4 rounded-xl border bg-[var(--bg-sidebar)] p-4 ' +
                                (selected
                                    ? 'border-[var(--color-primary)]'
                                    : 'border-[var(--border-line)]')
                            }
                        >
                            <DownloadThumbnail
                                url={job.url}
                                localPath={job.thumbnailPath}
                                completed={job.state === 'completed'}
                                title={job.title || translate("다운로드 썸네일")}
                            />
                            <div className={'min-w-0 flex-1 space-y-2' + (canDeleteDownload(job) ? ' pr-7' : '')}>
                                <p className="truncate text-sm font-medium" title={job.url}>
                                    {job.url}
                                </p>
                                <p className="text-xs text-[var(--text-muted)]">
                                    {downloadPlatform(job.url)}
                                </p>
                                <p className="text-sm">
                                    {downloadStatus(job)}
                                    {job.cleanupRequired ? translate(" · 정리 필요") : ''}
                                    {job.options === null ? translate(" · 이전 옵션 확인 필요") : ''}
                                </p>
                                <DownloadProgress job={job} />
                                <div className="flex flex-wrap gap-2">
                                    <button
                                        type="button"
                                        className={button}
                                        data-detail-trigger
                                        aria-expanded={selected}
                                        onClick={() =>
                                            selected ? vm.closeDetail() : void vm.openDetail(job.id)
                                        }
                                    >
                                        {selected ? translate("상세 닫기") : translate("상세보기")}
                                    </button>
                                    <DownloadJobActions job={job} />
                                </div>
                                {job.state === 'running' && job.phase === 'committing' && (
                                    <p className="text-xs text-[var(--text-muted)]">{translate("저장 중에는 취소할 수 없습니다.")}</p>
                                )}
                            </div>
                            {canDeleteDownload(job) && <button type="button" title={translate("요청 기록 삭제")} aria-label={translate("요청 기록 삭제")}
                                disabled={Boolean(vm.deletingIds?.includes(job.id)) || vm.requestedIds.includes(job.id) || vm.cancellingIds.includes(job.id) || vm.batchRetry?.running}
                                onClick={() => void vm.deleteJob(job.id)}
                                className="absolute right-2 top-2 rounded-md p-2 text-[var(--text-muted)] hover:bg-[var(--bg-hover)] disabled:opacity-40">
                                <X className="size-4" aria-hidden="true" />
                            </button>}
                        </li>
                    )
                })}
            </ul>
            {(vm.queue.length > 0 || vm.offset > 0) && (
                <nav aria-label={translate("다운로드 작업 페이지")} className="flex items-center gap-3">
                    <button
                        type="button"
                        className={button}
                        disabled={vm.loading || vm.offset === 0}
                        onClick={() => void vm.refresh(Math.max(0, vm.offset - DOWNLOAD_PAGE_SIZE))}
                    >{translate("이전")}</button>
                    <span className="text-sm">{translate("{0} 페이지", { "0": Math.floor(vm.offset / DOWNLOAD_PAGE_SIZE) + 1 })}</span>
                    <button
                        type="button"
                        className={button}
                        disabled={vm.loading || !vm.hasNextPage}
                        onClick={() => void vm.refresh(vm.offset + DOWNLOAD_PAGE_SIZE)}
                    >{translate("다음")}</button>
                </nav>
            )}
        </section>
    )
}
