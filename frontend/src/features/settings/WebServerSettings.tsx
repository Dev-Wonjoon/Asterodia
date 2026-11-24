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
                <Button disabled={vm.loading || vm.busy || !vm.status} onClick={() => void vm.toggle()}>{vm.busy ? translate("gallery.processing") : vm.status?.running ? translate("settings.stopServer") : translate("settings.startServer")}</Button>
            </div>
            {vm.error && <p role="alert" className="text-sm text-red-500">{translateMessage(vm.error)} <button type="button" disabled={vm.busy} className="underline" onClick={() => void vm.refresh()}>{translate("download.checkAgain")}</button></p>}
            {vm.status && <>
                <p className="text-xs text-[var(--text-muted)]">{translate("settings.localAddress")}<span className="select-text">{vm.status.url}</span></p>
                <p className="text-xs text-[var(--text-muted)]">{translate("settings.offWhenTheAppStartsTheServerAlsoStops")}</p>
                <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={Boolean(vm.status.editing)} disabled={vm.loading || vm.busy || !vm.status.running} onChange={() => void vm.toggleEditing()} />{translate("settings.allowMobileEditing")}</label>
                <p className="text-xs text-[var(--text-muted)]">{translate("settings.devicesPermittedByYourTailscaleAccessPolicyCanCreate")}</p>
            </>}
        </div>
    </SettingSection>
}
