import { Button } from '@renderer/shared/components/ui/Button'
import { SettingSection } from '@renderer/shared/components/ui/SettingSection'
import { useWebServerSettings } from './useWebServerSettings'

export function WebServerSettings() {
    const vm = useWebServerSettings()
    return <SettingSection title="로컬 웹 서버" description="갤러리·검색·미디어 재생을 제공합니다. 모바일 편집은 별도로 허용할 수 있습니다.">
        <div className="space-y-3 px-4 py-3">
            <div className="flex items-center justify-between gap-3">
                <span role="status" className="text-sm">{vm.loading ? '상태 확인 중…' : !vm.status ? '상태 확인 실패' : vm.status.running ? '실행 중' : '꺼짐'}</span>
                <Button disabled={vm.loading || vm.busy || !vm.status} onClick={() => void vm.toggle()}>{vm.busy ? '처리 중…' : vm.status?.running ? '서버 종료' : '서버 시작'}</Button>
            </div>
            {vm.error && <p role="alert" className="text-sm text-red-500">{vm.error} <button type="button" disabled={vm.busy} className="underline" onClick={() => void vm.refresh()}>다시 확인</button></p>}
            {vm.status && <>
                <p className="text-xs text-[var(--text-muted)]">로컬 주소 <span className="select-text">{vm.status.url}</span></p>
                <p className="text-xs text-[var(--text-muted)]">앱을 실행할 때는 꺼져 있으며, 앱 종료 시 서버도 종료됩니다.</p>
                <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={Boolean(vm.status.editing)} disabled={vm.loading || vm.busy || !vm.status.running} onChange={() => void vm.toggleEditing()} />모바일 편집 허용</label>
                <p className="text-xs text-[var(--text-muted)]">허용하면 Tailscale 접근 정책상 접속 가능한 기기에서 GIF·영상 편집 작업을 만들고 조회·취소할 수 있습니다. 서버 종료 시 허용도 꺼지며, 이미 등록된 작업은 PC에서 계속 처리합니다.</p>
            </>}
        </div>
    </SettingSection>
}
