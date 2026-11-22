import { useContext } from 'react'
import { SettingSection } from '@renderer/shared/components/ui/SettingSection'
import { Button } from '@renderer/shared/components/ui/Button'
import { ProgressBar } from '@renderer/shared/components/ui/ProgressBar'
import { formatBytes } from '@renderer/shared/utils/formatBytes'
import { LeaveGuardContext } from '@renderer/shared/hooks/leaveGuardContext'
import { useConfirm } from '@renderer/shared/hooks/useConfirm'
import { useAppUpdate } from './useAppUpdate'

const labels = {
    idle: '새 버전을 자동으로 확인합니다.', unconfigured: '업데이트 준비 중…', checking: '새 버전 확인 중…',
    'up-to-date': '최신 버전입니다.', available: '새 버전을 다운로드할 수 있습니다.',
    downloading: '업데이트 다운로드 중…', verifying: '업데이트 파일 검증 중…', installing: '업데이트 적용 준비 중…',
    ready: '업데이트 준비가 끝났습니다. 재시작하면 적용됩니다.', error: '업데이트를 완료하지 못했습니다.',
    restarting: '업데이트를 적용하기 위해 앱을 다시 시작합니다…', unsupported: '현재 실행 환경에서는 자동 업데이트를 지원하지 않습니다.',
}

export function UpdateSettings() {
    const vm = useAppUpdate()
    const guard = useContext(LeaveGuardContext)
    const { confirm } = useConfirm()
    if (!vm) return null
    const { status, pending, error, act } = vm
    const working = Boolean(pending || (status && ['checking', 'downloading', 'verifying', 'installing', 'restarting'].includes(status.state)))
    async function restart() {
        if (guard?.current && !await guard.current()) return
        if (!await confirm({ title: '업데이트 적용', message: `StorageBucket ${status?.version}을 적용하기 위해 앱을 다시 시작할까요? 진행 중인 작업이 있으면 적용을 미룹니다.`, confirmLabel: '재시작하여 적용' })) return
        await act('restart')
    }
    return <SettingSection title="앱 업데이트" description="정식 버전을 자동으로 확인합니다. 다운로드와 재시작은 직접 선택할 수 있습니다.">
        <div className="space-y-3 px-4 py-3 text-sm">
            <p>현재 버전 {status?.currentVersion ?? '확인 중…'}</p>
            <p role="status">{status ? labels[status.state] : '업데이트 상태 확인 중…'}</p>
            {status?.version && <p>새 버전 {status.version} · {formatBytes(status.size)}</p>}
            {(error || status?.error) && <p role="alert" className="break-words text-red-500">{error || status?.error}</p>}
            {status?.state === 'downloading' && <>
                <ProgressBar label="업데이트 다운로드 진행률" value={status.size > 0 ? Math.min(100, status.written / status.size * 100) : undefined} />
                <p className="text-xs text-[var(--text-muted)]">{formatBytes(status.written)} / {formatBytes(status.size)}</p>
            </>}
            <div className="flex flex-wrap gap-2">
                <Button disabled={!status?.supported || working || status.state === 'ready'} onClick={() => void act('check')}>업데이트 확인</Button>
                {status?.version && status.state !== 'ready' && <Button disabled={working} onClick={() => void act('download')}>업데이트 다운로드</Button>}
                {status?.state === 'ready' && <Button disabled={working} onClick={() => void restart()}>재시작하여 적용</Button>}
            </div>
            {status?.lastChecked ? <p className="text-xs text-[var(--text-muted)]">마지막 확인: {new Date(status.lastChecked).toLocaleString()}</p> : null}
            {status?.notes && <details><summary className="cursor-pointer">변경 사항</summary><pre className="mt-2 max-h-64 overflow-auto whitespace-pre-wrap break-words font-sans text-[var(--text-muted)]">{status.notes}</pre></details>}
            <p className="text-xs text-[var(--text-muted)]">기존 라이브러리와 설정은 유지됩니다. 다운로드·가져오기·파일 이동·편집·엔진 설치를 마친 뒤 적용하세요.</p>
        </div>
    </SettingSection>
}

export function UpdateNotice({ onOpen }: { onOpen(): void }) {
    const vm = useAppUpdate()
    const status = vm?.status
    if (!status || (status.state !== 'available' && status.state !== 'ready')) return null
    return <div role="status" className="flex flex-none items-center justify-between gap-3 border-b border-[var(--border-line)] bg-[var(--bg-sidebar)] px-4 py-2 text-sm">
        <span>StorageBucket {status.version} {status.state === 'ready' ? '업데이트 적용 준비 완료' : '새 버전이 있습니다.'}</span>
        <button type="button" className="shrink-0 underline" onClick={onOpen}>업데이트 보기</button>
    </div>
}
