import { useEffect, useRef, useState } from 'react'
import { getApi } from '@renderer/shared/api'
import { translate, useLanguage } from '@renderer/shared/i18n'
import { useConfirm } from '@renderer/shared/hooks/useConfirm'
import { useToast } from '@renderer/shared/hooks/useToast'
import { Button } from '@renderer/shared/components/ui/Button'
import { SettingSection } from '@renderer/shared/components/ui/SettingSection'
import type { CookieEntry, CookieState } from '@contracts/cookie-api'

export function CookieSettings(): React.JSX.Element {
    const { language } = useLanguage()
    const { confirm } = useConfirm()
    const { showToast } = useToast()
    const [state, setState] = useState<CookieState>({ groups: [], warning: '' })
    const [site, setSite] = useState('')
    const [loading, setLoading] = useState(true)
    const [busy, setBusy] = useState(false)
    const lock = useRef(false)

    useEffect(() => {
        let active = true
        void getApi().cookies.list().then(value => { if (active) setState(value) })
            .catch(() => { if (active) showToast(translate('cookies.loadFailed'), 'error') })
            .finally(() => { if (active) setLoading(false) })
        return () => { active = false }
    }, [showToast])

    async function change(action: () => Promise<CookieState | null>): Promise<void> {
        if (lock.current) return
        lock.current = true; setBusy(true)
        try { const next = await action(); if (next) setState(next) }
        catch { showToast(translate('cookies.changeFailed'), 'error') }
        finally { lock.current = false; setBusy(false) }
    }
    const entryName = (entry: CookieEntry): string => entry.name || translate('cookies.unnamed')
    async function removeEntry(entry: CookieEntry): Promise<void> {
        if (await confirm({ message: translate('cookies.confirmEntry', { name: entryName(entry), domain: entry.domain }), danger: true })) {
            await change(() => getApi().cookies.deleteEntry(entry.id))
        }
    }
    async function removeSite(value: string): Promise<void> {
        if (await confirm({ message: translate('cookies.confirmSite', { site: value }), danger: true })) {
            await change(() => getApi().cookies.deleteSite(value))
        }
    }
    async function excludeSite(): Promise<void> {
        const value = site.trim()
        if (!value) return
        await change(async () => { const next = await getApi().cookies.setEnabled(value, false); setSite(''); return next })
    }
    const disabled = busy || loading || state.warning === 'store-unavailable'
    const expiry = (entry: CookieEntry): string => entry.expires === 0 ? translate('cookies.session') : new Date(entry.expires * 1000).toLocaleString(language === 'ko' ? 'ko-KR' : 'en-US')
    const source = (value: string): string => value.startsWith('file:') ? translate('cookies.fileSource', { name: value.slice(5) }) : translate('cookies.browserSource', { name: value.startsWith('browser:') ? value.slice(8) : '' })

    return <SettingSection title={translate('cookies.title')} description={translate('cookies.description')}>
        <div className="space-y-4 py-3">
            <div className="flex flex-wrap items-center gap-3">
                <Button disabled={disabled} onClick={() => void change(() => getApi().cookies.importFile())}>{translate('cookies.import')}</Button>
                <span className="text-xs text-[var(--text-muted)]">{translate('cookies.importDescription')}</span>
            </div>
            {state.warning && <p role="alert" className="text-sm text-amber-600">{translate(state.warning === 'store-unavailable' ? 'cookies.storeUnavailable' : 'cookies.legacyFailed')}</p>}
            <form className="flex flex-wrap gap-2" onSubmit={event => { event.preventDefault(); void excludeSite() }}>
                <input aria-label={translate('cookies.siteAddress')} value={site} disabled={disabled} maxLength={253} onChange={event => setSite(event.target.value)} placeholder="example.com" className="min-w-0 flex-1 rounded-lg border border-[var(--border-line)] bg-[var(--bg-hover)] px-3 py-2 text-sm" />
                <Button type="submit" variant="ghost" disabled={disabled || !site.trim()}>{translate('cookies.exclude')}</Button>
            </form>
            {loading ? <p role="status" className="text-sm">{translate('cookies.loading')}</p> : state.groups.length === 0 ? <p className="text-sm text-[var(--text-muted)]">{translate('cookies.empty')}</p> : state.groups.map(group => <details key={group.site} className="rounded-xl border border-[var(--border-line)] bg-[var(--bg-hover)]">
                <summary className="cursor-pointer px-4 py-3 text-sm"><strong>{group.site}</strong><span className="ml-3 text-[var(--text-muted)]">{translate('cookies.count', { count: group.count, active: group.active })} · {translate(group.enabled ? 'cookies.enabled' : 'cookies.disabled')}</span></summary>
                <div className="space-y-3 border-t border-[var(--border-line)] px-4 py-3">
                    <div className="flex flex-wrap items-center justify-between gap-2">
                        <label className="flex items-center gap-2 text-sm"><input type="checkbox" aria-label={translate('cookies.useSite', { site: group.site })} checked={group.enabled} disabled={disabled} onChange={event => void change(() => getApi().cookies.setEnabled(group.site, event.target.checked))} />{translate('cookies.use')}</label>
                        <Button variant="danger" disabled={disabled} onClick={() => void removeSite(group.site)}>{translate('cookies.deleteSite')}</Button>
                    </div>
                    {group.entries.length > 0 && <div className="overflow-x-auto"><table className="w-full text-left text-xs">
                        <thead><tr>{['cookies.name', 'cookies.domain', 'cookies.path', 'cookies.expiry', 'cookies.source', 'cookies.actions'].map(key => <th key={key} className="px-2 py-2 font-medium text-[var(--text-muted)]">{translate(key)}</th>)}</tr></thead>
                        <tbody>{group.entries.map(entry => <tr key={entry.id} className="border-t border-[var(--border-line)]">
                            <td className="max-w-48 break-all px-2 py-2">{entryName(entry)}</td>
                            <td className="px-2 py-2">{entry.subdomains ? '.' : ''}{entry.domain}</td>
                            <td className="max-w-32 break-all px-2 py-2">{entry.path}</td>
                            <td className="px-2 py-2">{expiry(entry)}{entry.expired && <span className="ml-1 text-amber-600">{translate('cookies.expired')}</span>}</td>
                            <td className="max-w-40 break-all px-2 py-2">{source(entry.source)}</td>
                            <td className="px-2 py-2"><Button variant="ghost" disabled={disabled} aria-label={translate('cookies.deleteEntry', { name: entryName(entry), domain: entry.domain, path: entry.path })} onClick={() => void removeEntry(entry)}>{translate('cookies.delete')}</Button></td>
                        </tr>)}</tbody>
                    </table></div>}
                </div>
            </details>)}
        </div>
    </SettingSection>
}
