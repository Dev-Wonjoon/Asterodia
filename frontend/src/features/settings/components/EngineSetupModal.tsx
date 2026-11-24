import { translateMessage, translate, useLanguage } from '@renderer/shared/i18n'
import { BaseModal } from '@renderer/shared/components/ui/BaseModal'
import { useEngineSettingsViewModel } from '../useEngineSettingsViewModel'
import { EngineInstallActions, EngineInstallProgress } from './EngineInstallProgress'

interface EngineStatusProps {
    onComplete: () => void
}

export function EngineSetupModal({ onComplete }: EngineStatusProps): React.JSX.Element | null {
    useLanguage()
    const installer = useEngineSettingsViewModel()
    const { engines, progress, busy, loading, statusError, refreshStatus, allInstalled } = installer

    if (loading) return null

    return (
        <BaseModal onClose={busy ? undefined : onComplete}>
            <div className="max-h-[85vh] w-[min(680px,calc(100vw-32px))] overflow-y-auto border border-[var(--border-line)] p-6">
                <h2 className="text-lg font-semibold mb-1 text-[var(--text-main)]">{translate("settings.engineSetup")}</h2>
                <p className="text-sm mb-5 text-[var(--text-muted)]">{translate("settings.enginesRequiredForDownloadingAreNotInstalled")}</p>
                {statusError && <div role="alert" className="mb-3 text-sm text-red-500">{translateMessage(statusError)} <button type="button" onClick={() => void refreshStatus()}>{translate("download.checkAgain")}</button></div>}
                <EngineInstallActions installer={installer} />

                <div className="space-y-3 mb-5">
                    {Object.entries(engines).map(([name, status]) => (
                        <div
                            key={name}
                            className="rounded-xl bg-[var(--bg-app)]"
                        >
                            <div className="flex items-center justify-between gap-3 px-4 py-3">
                                <div className="min-w-0 flex-1">
                                    <span className="font-medium text-[var(--text-main)]">{name}</span>
                                    {status.installed && (
                                        <span className="ml-2 break-words text-xs text-green-500">
                                            {status.version || translate("download.installed")}
                                        </span>
                                    )}
                                </div>
                                {status.installed ? (
                                    <span className="text-xs text-green-500">✓</span>
                                ) : (
                                    <span className="text-xs text-[var(--text-muted)]">{translate("settings.notInstalled")}</span>
                                )}
                            </div>
                            <EngineInstallProgress name={name} progress={progress[name]} />
                        </div>
                    ))}
                </div>

                <div className="flex gap-2 justify-end">
                    <button
                        onClick={onComplete}
                        disabled={busy}
                        className="px-4 py-2 rounded-xl text-sm border border-[var(--border-line)] text-[var(--text-main)] disabled:opacity-50"
                    >
                        {allInstalled ? translate("settings.getStarted") : translate("settings.later")}
                    </button>
                </div>
            </div>
        </BaseModal>
    )
}
