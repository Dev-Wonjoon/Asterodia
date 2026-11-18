import { Button } from '@renderer/shared/components/ui/Button'
import { SettingSection } from '@renderer/shared/components/ui/SettingSection'
import { useWebServerSettings } from './useWebServerSettings'

export function WebServerSettings() {
    const vm = useWebServerSettings()
    return <SettingSection title="로컬 웹 서버" description="갤러리·검색·미디어 재생을 읽기 전용으로 제공합니다.">
        <div className="space-y-3 px-4 py-3">
            <div className="flex items-center justify-between gap-3">
                <span role="status" className="text-sm">{vm.loading ? '상태 확인 중…' : vm.status?.running ? '실행 중' : '꺼짐'}</span>
                <Button disabled={vm.loading || vm.busy || !vm.status} onClick={() => void vm.toggle()}>{vm.busy ? '처리 중…' : vm.status?.running ? '서버 종료' : '서버 시작'}</Button>
            </div>
            {vm.error && <p role="alert" className="text-sm text-red-500">{vm.error} <button type="button" disabled={vm.busy} className="underline" onClick={() => void vm.refresh()}>다시 확인</button></p>}
            {vm.status && <>
                <p className="text-xs text-[var(--text-muted)]">로컬 주소 <span className="select-text">{vm.status.url}</span></p>
                <p className="text-xs text-[var(--text-muted)]">앱을 실행할 때는 꺼져 있으며, 앱 종료 시 서버도 종료됩니다. 모바일 연결과 HTTPS는 Tailscale Serve에서 설정하세요.</p>
                <code className="block select-text overflow-x-auto rounded-lg bg-[var(--bg-app)] p-3 text-xs">tailscale serve --bg {vm.status.url}</code>
            </>}
        </div>
    </SettingSection>
}
