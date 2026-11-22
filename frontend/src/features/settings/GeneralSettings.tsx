import { Button } from '@renderer/shared/components/ui/Button'
import { useGeneralSettingsViewModel } from '@renderer/features/settings/useGeneralSettingsViewModel'
import { SettingSection } from '@renderer/shared/components/ui/SettingSection'
import { SettingRow } from '@renderer/shared/components/ui/SettingRow'
import { UpdateSettings } from './UpdateSettings'

export const GeneralSettings = (): React.JSX.Element => {
    const { downloadPath, changePath, importDatabase, importing, importResult, importError } = useGeneralSettingsViewModel()

    return (
        <div className="space-y-6"><UpdateSettings /><SettingSection title="일반 설정" description="앱의 기본 동작을 설정합니다.">
            <SettingRow
                label="다운로드 경로"
                value={downloadPath || '설정되지 않음'}
                action={
                    <Button disabled={importing} onClick={changePath}>변경</Button>
                }
            />
            <SettingRow
                label="Wails 데이터베이스 가져오기"
                value={importing ? 'DB를 확인하고 병합하고 있습니다…' : 'StorageBucket Wails DB 파일 선택 · Electron DB는 지원하지 않습니다.'}
                action={
                    <Button disabled={importing} onClick={() => void importDatabase()}>
                        {importing ? '가져오는 중…' : '가져오기'}
                    </Button>
                }
            />
            <p className="px-4 pb-3 text-xs text-amber-500">미디어나 파일은 가져오지 않습니다</p>
            {importError && (
                <p role="alert" className="px-4 py-2 text-sm text-red-500">{importError}</p>
            )}
            {importResult && (
                <div role="status" className="space-y-2 px-4 py-3 text-sm text-[var(--text-muted)]">
                    <p>묶음 {importResult.addedMedia}개 · 파일 {importResult.addedFiles}개를 추가했습니다.</p>
                    <p>중복 묶음 {importResult.skippedMedia}개 · 중복 파일 {importResult.skippedFiles}개는 유지했습니다.</p>
                    {importResult.addedHistory > 0 && (
                        <p>작업 이력 {importResult.addedHistory}개를 보존했습니다. 가져온 작업은 자동 실행하지 않습니다.</p>
                    )}
                    {importResult.missingFiles > 0 && (
                        <p>파일 {importResult.missingFiles}개의 기존 경로를 찾지 못했습니다. 등록 정보는 보존했습니다.</p>
                    )}
                    <p className="break-all">병합 전 백업: {importResult.backupPath}</p>
                </div>
            )}
        </SettingSection></div>
    )
}
