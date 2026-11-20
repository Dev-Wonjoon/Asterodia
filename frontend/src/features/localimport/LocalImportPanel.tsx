import type { LocalImportFileFilter } from '@contracts/local-import-api'
import { canResume, importProgress, states } from './presentation'
import { useLocalImportViewModel } from './useLocalImportViewModel'
import { X } from 'lucide-react'
import { formatBytes } from '@renderer/shared/utils/formatBytes'
import { ProgressBar } from '@renderer/shared/components/ui/ProgressBar'
import { useEffect, useImperativeHandle, type Ref } from 'react'

export interface LocalImportControls { choose: () => void; reopen: () => void }

export function LocalImportPanel({ disabled, onComplete, compact = false, controlRef, onBusyChange, onHasResultChange }: { disabled: boolean; onComplete: () => void; compact?: boolean; controlRef?: Ref<LocalImportControls>; onBusyChange?: (busy: boolean) => void; onHasResultChange?: (available: boolean) => void }) {
    const vm = useLocalImportViewModel(onComplete)
    const job = vm.page?.job
    const progress = job ? importProgress(job) : undefined
    const running = job?.state === 'running' || job?.state === 'scanning'
    const stopping = job?.pauseRequested || job?.cancelRequested
    const button = 'rounded-lg border border-[var(--border-line)] px-3 py-2 text-sm disabled:opacity-40'
    const number = (n: number) => n.toLocaleString()
    useImperativeHandle(controlRef, () => ({ choose: () => { if (!disabled && !vm.busy) void vm.choose() }, reopen: vm.reopen }))
    useEffect(() => { onBusyChange?.(vm.busy) }, [vm.busy, onBusyChange])
    useEffect(() => { onHasResultChange?.(!!job) }, [!!job, onHasResultChange])
    if (compact && !vm.error && (vm.dismissed || (!job && !vm.busy))) return null
    if (vm.dismissed) return <section aria-label="로컬 폴더 가져오기" className="flex flex-wrap items-center gap-2">
        <button type="button" className={button} disabled={disabled || vm.busy} onClick={() => void vm.choose()}>폴더 가져오기</button>
        <button type="button" className={button} onClick={vm.reopen}>최근 결과</button>
        {vm.error && <p role="alert" className="text-sm text-red-500">{vm.error}</p>}
    </section>
    return <section aria-label="로컬 폴더 가져오기" className="space-y-3 rounded-xl border border-[var(--border-line)] bg-[var(--bg-sidebar)] p-4">
        <div className="flex flex-wrap items-center justify-between gap-3">
            <div><h2 className="font-medium">로컬 폴더 가져오기</h2><p className="mt-1 text-xs text-[var(--text-muted)]">하위 폴더의 이미지·영상·오디오를 등록합니다. 원본은 이동하거나 복사하지 않습니다.</p></div>
            <div className="flex items-center gap-2">
                <button type="button" className={button} disabled={disabled || vm.busy} onClick={() => void vm.choose()}>폴더 선택</button>
                {vm.canDismiss && <button type="button" aria-label="가져오기 결과 닫기" title="가져오기 결과 닫기" onClick={vm.dismiss}
                    className="rounded-lg p-2 text-[var(--text-muted)] hover:bg-[var(--bg-hover)]"><X size={18} aria-hidden="true" /></button>}
            </div>
        </div>
        {vm.error && <p role="alert" className="text-sm text-red-500">{vm.error}</p>}
        {job && <>
            <p className="break-all text-sm">{job.root}</p>
            <div role="status" aria-live="polite" className="space-y-1 text-sm text-[var(--text-muted)]">
                <p>{stopping && running ? '중단 처리 중…' : states[job.state] ?? job.state}</p>
                <p>{job.scanComplete ? `전체 ${number(job.counts.discovered)}개 · 처리 ${number(job.counts.processed)}개 (${(progress ?? 0).toFixed(1)}%)` : `발견 ${number(job.counts.discovered)}개 · 전체 수 확인 중`}</p>
            </div>
            <ProgressBar label="폴더 가져오기 진행률" value={progress} />
            <div className="space-y-1 text-xs text-[var(--text-muted)]">
                <p>신규 {number(job.counts.added)}개 · 갱신 {number(job.counts.updated)}개 · 기등록 {number(job.counts.unchanged)}개 · 실패 {number(job.counts.failed)}개 · 사용자 제외 {number(job.counts.userExcluded)}개</p>
                <p>처리 용량 {formatBytes(job.counts.processedBytes)} / {formatBytes(job.counts.totalBytes)} · {formatBytes(job.bytesPerSecond)}/초 · {job.filesPerSecond.toFixed(1)}개/초 (평균)</p>
                <p>탐색 제외 {number(job.counts.scanExcluded)}개 · 탐색 오류 {number(job.counts.scanErrors)}건 · 완료 {number(job.counts.completedFolders)}개 폴더 · 일부 실패 {number(job.counts.failedFolders)}개 폴더</p>
                {job.currentPath && <p className="break-all">현재: {job.currentPath}</p>}
                <p>다른 화면으로 이동해도 작업은 계속됩니다. 중단하거나 앱을 종료해도 완료한 파일은 유지됩니다.</p>
            </div>
            {job.error && <p role="alert" className="break-words text-sm text-red-500">{job.error}</p>}
            {job.largeFolders > 0 && <p role="alert" className="text-sm text-amber-600">파일이 5만 개를 넘는 폴더가 {number(job.largeFolders)}개 있습니다. 처리 시간이 길어질 수 있으며 계속 등록할 수 있습니다.</p>}
            {!job.canResume && <p className="text-sm text-[var(--text-muted)]">이전 방식으로 만든 작업입니다. 같은 폴더를 다시 탐색해 주세요.</p>}
            {job.state === 'ready' && <p className="text-xs text-[var(--text-muted)]">목록을 확인한 뒤 등록하세요. 이미지 검사는 ImageMagick, 영상·오디오 검사는 FFmpeg와 FFprobe가 필요합니다.</p>}
            <div className="flex flex-wrap gap-2">
                {job.state === 'ready' && job.canResume && <button type="button" className={button} disabled={disabled || vm.requesting} onClick={() => void vm.run()}>등록 시작</button>}
                {['scanning', 'ready', 'running'].includes(job.state) && <button type="button" className={button} disabled={vm.requesting || stopping} onClick={() => void vm.pause()}>중단</button>}
                {canResume(job) && <button type="button" className={button} disabled={disabled || vm.requesting} onClick={() => void vm.resume()}>{job.scanComplete ? '이어하기' : '탐색 이어하기'}</button>}
                {!vm.busy && job.canResume && job.counts.failed > 0 && <>
                    <button type="button" className={button} disabled={disabled} onClick={() => void vm.retry()}>실패 파일 재시도</button>
                    <button type="button" className={button} disabled={disabled} onClick={() => void vm.exclude()}>실패 파일 모두 제외</button>
                </>}
                {!vm.busy && <button type="button" className={button} disabled={disabled} onClick={() => void vm.rescan()}>같은 폴더 다시 탐색</button>}
                <button type="button" className={button} disabled={vm.requesting} onClick={() => void vm.refresh()}>상태 새로고침</button>
                <button type="button" className={button} aria-expanded={vm.showFiles} onClick={() => vm.setShowFiles(!vm.showFiles)}>파일별 결과 {vm.showFiles ? '접기' : '보기'}</button>
            </div>
            {vm.showFiles && <div className="space-y-2 rounded border border-[var(--border-line)] p-3">
                <label className="text-sm">파일 상태 <select aria-label="파일 상태" className="rounded bg-[var(--bg-sidebar)] p-1" value={vm.fileState} onChange={e => vm.setFileState(e.target.value as LocalImportFileFilter)}>
                    <option value="failed">실패</option><option value="pending">등록 대기</option><option value="excluded">사용자 제외</option><option value="">전체</option>
                </select></label>
                <p className="text-xs text-[var(--text-muted)]">진행 중에는 중단 후 파일을 제외하거나 재시도할 수 있습니다. 재시도는 현재 파일 상태를 다시 검사합니다.</p>
                {vm.fileError && <p role="alert" className="text-sm text-red-500">{vm.fileError}</p>}
                {!vm.filePage && !vm.fileError && <p className="text-sm">파일 목록을 불러오는 중…</p>}
                {vm.filePage?.total === 0 && <p className="text-sm">해당 파일이 없습니다.</p>}
                <ul aria-label="파일별 가져오기 결과" className="max-h-72 space-y-2 overflow-y-auto text-sm">
                    {vm.filePage?.files.map(file => <li key={file.id} className="space-y-1 border-b border-[var(--border-line)] pb-2">
                        <p className="break-all">{file.path} · {states[file.state] ?? file.state}</p>
                        {file.error && <p className="break-words text-red-500">{file.error}</p>}
                        <div className="flex gap-2">
                            {['failed', 'excluded'].includes(file.state) && <button type="button" className={button} disabled={disabled || vm.busy} onClick={() => void vm.retry(file.id)}>재시도</button>}
                            {['pending', 'failed'].includes(file.state) && <button type="button" className={button} disabled={disabled || vm.busy} onClick={() => void vm.exclude(file.id)}>이번 작업에서 제외</button>}
                        </div>
                    </li>)}
                </ul>
                {(vm.filePage?.total ?? 0) > 25 && <div className="flex items-center gap-3 text-sm">
                    <button type="button" className={button} disabled={vm.fileOffset === 0 || vm.requesting} onClick={() => vm.setFileOffset(Math.max(0, vm.fileOffset - 25))}>파일 이전</button>
                    <span>{vm.fileOffset + 1}–{vm.fileOffset + (vm.filePage?.files.length ?? 0)} / {vm.filePage?.total}</span>
                    <button type="button" className={button} disabled={!vm.filePage?.hasNextPage || vm.requesting} onClick={() => vm.setFileOffset(vm.fileOffset + 25)}>파일 다음</button>
                </div>}
            </div>}
            <ul className="max-h-64 space-y-2 overflow-y-auto text-sm" aria-label="폴더별 가져오기 결과">
                {vm.page?.folders.map(folder => <li key={folder.id} className="rounded border border-[var(--border-line)] px-3 py-2">
                    <p className="break-all">{folder.path === '.' ? '선택한 폴더' : folder.path} · 파일 {number(folder.fileCount)}개 · {states[folder.state] ?? folder.state}</p>
                    {folder.error && <p className="mt-1 break-words text-red-500">{folder.error}</p>}
                </li>)}
            </ul>
            {(vm.page?.total ?? 0) > 25 && <div className="flex items-center gap-3 text-sm">
                <button type="button" className={button} disabled={vm.offset === 0 || vm.requesting} onClick={() => vm.goTo(Math.max(0, vm.offset - 25))}>이전</button>
                <span>{vm.offset + 1}–{vm.offset + (vm.page?.folders.length ?? 0)} / {vm.page?.total}</span>
                <button type="button" className={button} disabled={!vm.page?.hasNextPage || vm.requesting} onClick={() => vm.goTo(vm.offset + 25)}>다음</button>
            </div>}
        </>}
    </section>
}
