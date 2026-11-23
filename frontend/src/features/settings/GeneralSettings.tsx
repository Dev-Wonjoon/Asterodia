import { translateMessage, translate, useLanguage } from '@renderer/shared/i18n'
import { Button } from '@renderer/shared/components/ui/Button'
import { useGeneralSettingsViewModel } from '@renderer/features/settings/useGeneralSettingsViewModel'
import { SettingSection } from '@renderer/shared/components/ui/SettingSection'
import { SettingRow } from '@renderer/shared/components/ui/SettingRow'
import { UpdateSettings } from './UpdateSettings'
import { LanguageSelect } from '@renderer/shared/i18n/LanguageSelect'

export const GeneralSettings = (): React.JSX.Element => {
    useLanguage()
    const { downloadPath, changePath, importDatabase, importing, importResult, importError } = useGeneralSettingsViewModel()

    return (
        <div className="space-y-6"><UpdateSettings /><SettingSection title={translate("일반 설정")} description={translate("앱의 기본 동작을 설정합니다.")}>
            <SettingRow label={translate('언어')} value={translate('표시 언어를 선택합니다. 변경 사항은 바로 적용됩니다.')} action={<LanguageSelect />} />
            <SettingRow
                label={translate("다운로드 경로")}
                value={downloadPath || translate("설정되지 않음")}
                action={
                    <Button disabled={importing} onClick={changePath}>{translate("변경")}</Button>
                }
            />
            <SettingRow
                label={translate("Wails 데이터베이스 가져오기")}
                value={importing ? translate("DB를 확인하고 병합하고 있습니다…") : translate("StorageBucket Wails DB 파일 선택 · Electron DB는 지원하지 않습니다.")}
                action={
                    <Button disabled={importing} onClick={() => void importDatabase()}>
                        {importing ? translate("가져오는 중…") : translate("가져오기")}
                    </Button>
                }
            />
            <p className="px-4 pb-3 text-xs text-amber-500">{translate("미디어나 파일은 가져오지 않습니다")}</p>
            {importError && (
                <p role="alert" className="px-4 py-2 text-sm text-red-500">{translateMessage(importError)}</p>
            )}
            {importResult && (
                <div role="status" className="space-y-2 px-4 py-3 text-sm text-[var(--text-muted)]">
                    <p>{translate("묶음 {0}개 · 파일 {1}개를 추가했습니다.", { "0": importResult.addedMedia, "1": importResult.addedFiles })}</p>
                    <p>{translate("중복 묶음 {0}개 · 중복 파일 {1}개는 유지했습니다.", { "0": importResult.skippedMedia, "1": importResult.skippedFiles })}</p>
                    {importResult.addedHistory > 0 && (
                        <p>{translate("작업 이력 {0}개를 보존했습니다. 가져온 작업은 자동 실행하지 않습니다.", { "0": importResult.addedHistory })}</p>
                    )}
                    {importResult.missingFiles > 0 && (
                        <p>{translate("파일 {0}개의 기존 경로를 찾지 못했습니다. 등록 정보는 보존했습니다.", { "0": importResult.missingFiles })}</p>
                    )}
                    <p className="break-all">{translate("병합 전 백업: {0}", { "0": importResult.backupPath })}</p>
                </div>
            )}
        </SettingSection></div>
    )
}
