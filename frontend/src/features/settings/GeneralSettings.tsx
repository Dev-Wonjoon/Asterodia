import { translateMessage, translate, useLanguage } from '@renderer/shared/i18n'
import { Button } from '@renderer/shared/components/ui/Button'
import { useGeneralSettingsViewModel } from '@renderer/features/settings/useGeneralSettingsViewModel'
import { SettingSection } from '@renderer/shared/components/ui/SettingSection'
import { SettingRow } from '@renderer/shared/components/ui/SettingRow'
import { UpdateSettings } from './UpdateSettings'
import { LanguageSelect } from '@renderer/shared/i18n/LanguageSelect'
import { WorkingDirectorySettings } from './WorkingDirectorySettings'
import { ThumbnailCacheSettings } from './ThumbnailCacheSettings'
import { ContentFiltersSettings } from './ContentFiltersSettings'

export const GeneralSettings = (): React.JSX.Element => {
    useLanguage()
    const { downloadPath, changePath, importDatabase, importing, importResult, importError } = useGeneralSettingsViewModel()

    return (
        <div className="space-y-6"><UpdateSettings /><SettingSection title={translate("settings.generalSettings")} description={translate("settings.configureTheAppSBasicBehavior")}>
            <SettingRow label={translate("settings.language")} value={translate("settings.chooseTheDisplayLanguageChangesApplyImmediately")} action={<LanguageSelect />} />
            <SettingRow
                label={translate("settings.downloadLocation")}
                value={downloadPath || translate("settings.notSet")}
                action={
                    <Button disabled={importing} onClick={changePath}>{translate("settings.change")}</Button>
                }
            />
            <WorkingDirectorySettings disabled={importing} />
            <SettingRow
                label={translate("settings.importWailsDatabase")}
                value={importing ? translate("settings.checkingAndMergingTheDatabase") : translate("settings.selectAnAsterodiaWailsDatabaseElectronDatabasesAreNot")}
                action={
                    <Button disabled={importing} onClick={() => void importDatabase()}>
                        {importing ? translate("settings.importing") : translate("settings.import")}
                    </Button>
                }
            />
            <p className="px-4 pb-3 text-xs text-amber-500">{translate("settings.mediaAndFilesAreNotImported")}</p>
            {importError && (
                <p role="alert" className="px-4 py-2 text-sm text-red-500">{translateMessage(importError)}</p>
            )}
            {importResult && (
                <div role="status" className="space-y-2 px-4 py-3 text-sm text-[var(--text-muted)]">
                    <p>{translate("settings.addedCollectionsAndFiles", { collections: importResult.addedMedia, files: importResult.addedFiles })}</p>
                    <p>{translate("settings.keptDuplicateCollectionsAndDuplicateFiles", { collections: importResult.skippedMedia, files: importResult.skippedFiles })}</p>
                    {importResult.addedHistory > 0 && (
                        <p>{translate("settings.preservedJobHistoryEntriesImportedJobsDoNotRun", { count: importResult.addedHistory })}</p>
                    )}
                    {importResult.missingFiles > 0 && (
                        <p>{translate("settings.couldNotFindThePreviousPathsOfFilesTheir", { count: importResult.missingFiles })}</p>
                    )}
                    <p className="break-all">{translate("settings.backupBeforeMerge", { path: importResult.backupPath })}</p>
                </div>
            )}
        </SettingSection><ThumbnailCacheSettings /><ContentFiltersSettings disabled={importing} /></div>
    )
}
