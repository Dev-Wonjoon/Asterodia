import { translate, useLanguage } from '@renderer/shared/i18n'
import { getApi } from '@renderer/shared/api'
import { useState, useEffect } from 'react'
import { SettingSection } from '@renderer/shared/components/ui/SettingSection'
import { SettingRow } from '@renderer/shared/components/ui/SettingRow'
import { CookieSettings } from './CookieSettings'
import { useToast } from '@renderer/shared/hooks/useToast'
import { WebServerSettings } from './WebServerSettings'

const BROWSER_OPTIONS = [
    { value: '', get label() { return translate("shortcuts.disabled") } },
    { value: 'chrome', label: 'Chrome' },
    { value: 'firefox', label: 'Firefox' },
    { value: 'edge', label: 'Edge' }
]

export const NetworkSettings = (): React.JSX.Element => {
    useLanguage()
    const [browser, setBrowser] = useState('')
    const { showToast } = useToast()
    const [saving, setSaving] = useState(false)

    useEffect(() => {
        let active = true
        void getApi().getCookieBrowser()
            .then(browser => { if (active) setBrowser(browser) })
            .catch(() => { if (active) showToast(translate("settings.couldNotReadNetworkSettings"), 'error') })
        return () => { active = false }
    }, [showToast])

    async function save(action: () => Promise<void>): Promise<void> {
        if (saving) return
        setSaving(true)
        try { await action() } catch { showToast(translate("settings.couldNotSaveNetworkSettings"), 'error') }
        finally { setSaving(false) }
    }

    const handleBrowserChange = (value: string): void => {
        void save(async () => { if (await getApi().setCookieBrowser(value)) setBrowser(value) })
    }

    return (
        <div className="space-y-6">
        <WebServerSettings />
        <SettingSection
            title={translate("settings.networkSettings")}
            description={translate("settings.networkSettingsUsedForDownloads")}
        >
            <SettingRow
                label={translate("settings.cookieBrowser")}
                description={translate("settings.automaticallyReadCookiesFromABrowser")}
                value={BROWSER_OPTIONS.find((o) => o.value === browser)?.label || translate("shortcuts.disabled")}
                action={
                    <select
                        disabled={saving}
                        value={browser}
                        onChange={(e) => handleBrowserChange(e.target.value)}
                        className="bg-[var(--bg-active)] text-[var(--text-main)] text-xs rounded-lg px-3 py-1.5 border border-[var(--border-line)] outline-none"
                    >
                        {BROWSER_OPTIONS.map((opt) => (
                            <option key={opt.value} value={opt.value}>
                                {opt.label}
                            </option>
                        ))}
                    </select>
                }
            />
        </SettingSection>
        <CookieSettings />
        </div>
    )
}
