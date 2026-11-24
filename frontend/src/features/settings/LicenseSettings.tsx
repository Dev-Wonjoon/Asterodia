import { translate, useLanguage } from '@renderer/shared/i18n'
import { SettingSection } from '@renderer/shared/components/ui/SettingSection'
import { useLicenseSettingsViewModel } from '@renderer/features/settings/useLicenseSettingsViewModel'
export const LicenseSettings = (): React.JSX.Element => {
    useLanguage()
    const groups = [
        ['bundled', translate("settings.bundledLibrariesAndResources")],
        ['external', translate("settings.separatelyInstalledExternalTools")],
        ['build', translate("settings.buildDevelopmentAndTestTools")]
    ] as const
    const { licenses, expanded, toggle, loading, error, retry, query, setQuery } = useLicenseSettingsViewModel()
    return (
        <SettingSection title={translate("settings.openSourceLicenses")}
            description={translate("settings.readLicenseCopyrightAndAdditionalNoticeTextsOfflineExternal")}>
            <div className="p-4 space-y-3">
                <input type="search" aria-label={translate("settings.searchLicenses")} placeholder={translate("settings.searchComponentsOrLicenses")}
                    value={query} onChange={event => setQuery(event.target.value)}
                    className="w-full rounded-lg border border-[var(--border-line)] bg-[var(--bg-app)] px-3 py-2 text-sm text-[var(--text-main)]" />
                {loading && <p role="status">{translate("settings.loadingNotices")}</p>}
                {error && <div role="alert">{translate("settings.couldNotLoadNotices")}<button onClick={retry} className="underline">{translate("common.retry")}</button></div>}
                {!loading && !error && licenses.length === 0 && <p>{translate("shortcuts.noSearchResults")}</p>}
            </div>
            {!loading && !error && groups.map(([scope, title]) => {
                const items = licenses.filter(item => item.scope === scope)
                if (!items.length) return null
                return <section key={scope} aria-label={title}>
                    <h3 className="px-4 py-2 text-sm font-semibold text-[var(--text-muted)]">{title} ({items.length})</h3>
                    {items.map(item => <div key={item.id}>
                        <button onClick={() => toggle(item.id)} aria-expanded={expanded === item.id} aria-controls={`license-${item.id}`}
                            className="w-full flex items-center justify-between gap-3 px-4 py-3 text-left text-sm text-[var(--text-main)] hover:bg-[var(--bg-hover)]">
                            <span className="min-w-0 break-words"><strong>{item.name}</strong><br /><span className="text-xs text-[var(--text-muted)]">{item.version} · {item.license}</span></span>
                            <span aria-hidden="true">{expanded === item.id ? '−' : '+'}</span>
                        </button>
                        {expanded === item.id && <div id={`license-${item.id}`} className="px-4 pb-4 space-y-3">
                            <p className="text-xs text-[var(--text-muted)] leading-relaxed">{item.notice}</p>
                            <div className="flex gap-4 text-xs text-[var(--color-primary)]">
                                <a href={item.url} target="_blank" rel="noopener noreferrer" className="underline">{translate("settings.officialProject")}</a>
                                <a href={item.sourceURL} target="_blank" rel="noopener noreferrer" className="underline">{translate("settings.sourceCodeAndDistributionInformation")}</a>
                            </div>
                            <pre tabIndex={0} aria-label={translate("settings.noticeText", { name: item.name })} className="max-h-96 overflow-auto whitespace-pre-wrap break-words rounded-lg bg-[var(--bg-app)] p-3 text-xs text-[var(--text-main)]">{item.text}</pre>
                        </div>}
                    </div>)}
                </section>
            })}
        </SettingSection>
    )
}
