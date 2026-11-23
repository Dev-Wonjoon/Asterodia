import { getLocale, translate, translateMessage, useLanguage } from '@renderer/shared/i18n'
import { SettingSection } from '@renderer/shared/components/ui/SettingSection'
import { Button } from '@renderer/shared/components/ui/Button'
import { ProgressBar } from '@renderer/shared/components/ui/ProgressBar'
import { formatBytes } from '@renderer/shared/utils/formatBytes'
import { useAppUpdate } from './useAppUpdate'

const labels = {
    get idle() { return translate("새 버전을 자동으로 확인합니다.") }, get unconfigured() { return translate("업데이트 준비 중…") }, get checking() { return translate("새 버전 확인 중…") },
    get 'up-to-date'() { return translate("최신 버전입니다.") }, get available() { return translate("새 버전을 다운로드할 수 있습니다.") },
    get downloading() { return translate("업데이트 다운로드 중…") }, get verifying() { return translate("업데이트 파일 검증 중…") }, get installing() { return translate("업데이트 적용 준비 중…") },
    get ready() { return translate("업데이트 준비가 끝났습니다. 재시작하면 적용됩니다.") }, get error() { return translate("업데이트를 완료하지 못했습니다.") },
    get restarting() { return translate("업데이트를 적용하기 위해 앱을 다시 시작합니다…") }, get unsupported() { return translate("현재 실행 환경에서는 자동 업데이트를 지원하지 않습니다.") },
}

export function UpdateSettings() {
    useLanguage()
    const vm = useAppUpdate()
    if (!vm) return null
    const { status, pending, error, act } = vm
    const working = Boolean(pending || (status && ['checking', 'downloading', 'verifying', 'installing', 'restarting'].includes(status.state)))
    return <SettingSection title={translate("앱 업데이트")} description={translate("정식 버전을 자동으로 확인합니다. 다운로드와 재시작은 직접 선택할 수 있습니다.")}>
        <div className="space-y-3 px-4 py-3 text-sm">
            <p>{translate("현재 버전 {0}", { "0": status?.currentVersion ?? translate("확인 중…") })}</p>
            <p role="status">{status ? labels[status.state] : translate("업데이트 상태 확인 중…")}</p>
            {status?.version && <p>{translate("새 버전 {0} · {1}", { "0": status.version, "1": formatBytes(status.size) })}</p>}
            {(error || status?.error) && <p role="alert" className="break-words text-red-500">{translateMessage(error || status?.error)}</p>}
            {status?.state === 'downloading' && <>
                <ProgressBar label={translate("업데이트 다운로드 진행률")} value={status.size > 0 ? Math.min(100, status.written / status.size * 100) : undefined} />
                <p className="text-xs text-[var(--text-muted)]">{formatBytes(status.written)} / {formatBytes(status.size)}</p>
            </>}
            <div className="flex flex-wrap gap-2">
                <Button disabled={!status?.supported || working || status.state === 'ready'} onClick={() => void act('check')}>{translate("업데이트 확인")}</Button>
                {status?.version && status.state !== 'ready' && <Button disabled={working} onClick={() => void act('download')}>{translate("업데이트 다운로드")}</Button>}
                {status?.state === 'ready' && <Button disabled={working} onClick={() => void act('restart')}>{translate("재시작하여 적용")}</Button>}
            </div>
            {status?.lastChecked ? <p className="text-xs text-[var(--text-muted)]">{translate("마지막 확인: {0}", { "0": new Date(status.lastChecked).toLocaleString(getLocale()) })}</p> : null}
            {status?.notes && <details><summary className="cursor-pointer">{translate("변경 사항")}</summary><pre className="mt-2 max-h-64 overflow-auto whitespace-pre-wrap break-words font-sans text-[var(--text-muted)]">{status.notes}</pre></details>}
            <p className="text-xs text-[var(--text-muted)]">{translate("기존 라이브러리와 설정은 유지됩니다. 다운로드·가져오기·파일 이동·편집·엔진 설치를 마친 뒤 적용하세요.")}</p>
        </div>
    </SettingSection>
}

export function UpdateNotice({ onOpen }: { onOpen(): void }) {
    useLanguage()
    const vm = useAppUpdate()
    const status = vm?.status
    if (!status || (status.state !== 'available' && status.state !== 'ready')) return null
    return <div role="status" className="flex flex-none items-center justify-between gap-3 border-b border-[var(--border-line)] bg-[var(--bg-sidebar)] px-4 py-2 text-sm">
        <span>StorageBucket {status.version} {status.state === 'ready' ? translate("업데이트 적용 준비 완료") : translate("새 버전이 있습니다.")}</span>
        <button type="button" className="shrink-0 underline" onClick={onOpen}>{translate("업데이트 보기")}</button>
    </div>
}
