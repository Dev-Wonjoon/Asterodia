import { translateMessage, translate, useLanguage } from '@renderer/shared/i18n'
import { Button } from '@renderer/shared/components/ui/Button'
import { SettingSection } from '@renderer/shared/components/ui/SettingSection'
import { useWebServerSettings } from './useWebServerSettings'

export function WebServerSettings() {
    useLanguage()
    const vm = useWebServerSettings()
    return <SettingSection title={translate("settings.localWebServer")} description={translate("settings.providesGalleryBrowsingSearchAndMediaPlaybackMobileEditing")}>
        <div className="space-y-3 px-4 py-3">
            <div className="flex items-center justify-between gap-3">
                <span role="status" className="text-sm">{vm.loading ? translate("settings.checkingStatus") : !vm.status ? translate("settings.couldNotCheckStatus") : vm.status.running ? translate("settings.running") : translate("settings.off")}</span>
                <Button variant={vm.status?.running ? 'danger' : 'primary'} disabled={vm.loading || vm.busy || !vm.status} onClick={() => void vm.toggle()}>{vm.busy ? translate("gallery.processing") : vm.status?.running ? translate("settings.serverRunningStop") : translate("settings.startServer")}</Button>
            </div>
            {vm.error && <p role="alert" className="text-sm text-red-500">{translateMessage(vm.error)} <button type="button" disabled={vm.busy} className="underline" onClick={() => void vm.refresh()}>{translate("download.checkAgain")}</button></p>}
            {vm.status && <>
                <p className="text-xs text-[var(--text-muted)]">{translate("settings.localAddress")}<span className="select-text">{vm.status.url}</span></p>
                <p className="text-xs text-[var(--text-muted)]">{translate("settings.offWhenTheAppStartsTheServerAlsoStops")}</p>
                <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={vm.status.requireCode !== false} disabled={vm.loading || vm.busy} onChange={() => void vm.toggleRequireCode()} />{translate('security.requireCode')}</label>
                <p className="text-xs text-[var(--text-muted)]">{translate('security.requireCodeHelp')}</p>
                {vm.status.running && vm.status.requireCode !== false && <div className="space-y-2 rounded-lg border border-[var(--border-line)] p-3">
                    <p className="text-sm">{translate('security.code')}: <code className="select-all font-mono text-lg tracking-widest">{vm.status.pairCode}</code></p>
                    <p className="text-xs text-[var(--text-muted)]">{translate('security.codeHelp')}</p>
                    <p className="text-xs">{translate('security.connected', { count: vm.status.connected ?? 0 })}</p>
                    <Button disabled={vm.busy} onClick={() => void vm.revokeAccess()}>{translate('security.revoke')}</Button>
                </div>}

                <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={Boolean(vm.status.editing)} disabled={vm.loading || vm.busy || !vm.status.running} onChange={() => void vm.toggleEditing()} />{translate("settings.allowMobileEditing")}</label>
                <p className="text-xs text-[var(--text-muted)]">{translate("settings.devicesPermittedByYourTailscaleAccessPolicyCanCreate")}</p>
                <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={Boolean(vm.status.downloading)} disabled={vm.loading || vm.busy || !vm.status.running} onChange={() => void vm.toggleDownloading()} />{translate('settings.mobileDownloads.allow')}</label>
                <p className="text-xs text-[var(--text-muted)]">{translate('settings.mobileDownloads.help')}</p>
            </>}
        </div>
    </SettingSection>
}
