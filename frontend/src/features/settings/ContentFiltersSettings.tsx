import { useCallback, useEffect, useRef, useState } from 'react'
import { getApi } from '@renderer/shared/api'
import { translate, translateError, useLanguage } from '@renderer/shared/i18n'
import { contentFiltersChanged } from '@renderer/shared/hooks/useContentVisibility'
import { useSuggestions } from '@renderer/shared/hooks/useSuggestions'
import { SettingSection } from '@renderer/shared/components/ui/SettingSection'
import type { ContentFilters } from '@contracts/media-v2'

type Field = keyof ContentFilters
function Picker({ kind, field, values, disabled, change }: { kind: 'tag' | 'platform'; field: Field; values: string[]; disabled: boolean; change(field: Field, values: string[]): Promise<boolean> }) {
    const [text, setText] = useState('')
    const fetch = useCallback(async (value: string) => {
        const result = await getApi().media.suggest({ kind, text: value, limit: 10 })
        if (!result.ok) throw Error(translateError(result.error))
        return result.data
    }, [kind])
    const suggestions = useSuggestions(fetch, values)
    const label = translate(kind === 'platform' ? 'contentFilters.platforms' : 'contentFilters.tags')
    async function add(value: string) {
        value = value.trim()
        if (!value || disabled) return
        if (await change(field, [...new Set([...values, value])])) { setText(''); suggestions.clear() }
    }
    return <div className="space-y-2">
        <form className="flex flex-wrap gap-2" onSubmit={event => { event.preventDefault(); void add(text) }}>
            <label className="min-w-0 flex-1"><span className="mb-1 block text-sm">{label}</span><input aria-label={label} disabled={disabled} maxLength={200} value={text} onChange={event => { setText(event.target.value); void suggestions.suggest(event.target.value) }} onKeyDown={event => { if (event.nativeEvent.isComposing || event.keyCode === 229) { if (event.key === 'Enter') event.preventDefault() } else if (event.key === 'Escape') suggestions.clear() }} className="w-full rounded-lg border border-[var(--border-line)] bg-[var(--bg-hover)] px-3 py-2" placeholder={translate('contentFilters.input')} /></label>
            <button type="submit" disabled={disabled || !text.trim() || values.length >= 100} className="self-end rounded-lg border border-[var(--border-line)] px-3 py-2 text-sm disabled:opacity-40">{translate('contentFilters.add')}</button>
        </form>
        {suggestions.suggestions.length > 0 && <div className="flex flex-wrap gap-2" aria-label={translate('contentFilters.suggestions', { label })}>{suggestions.suggestions.map(value => <button key={value} type="button" disabled={disabled} className="rounded-lg border border-[var(--border-line)] px-3 py-1 text-sm" onClick={() => void add(value)}>{value}</button>)}</div>}
        {suggestions.error && <p role="alert" className="text-sm text-red-500">{suggestions.error}</p>}
        <ul className="flex flex-wrap gap-2" aria-label={label}>{values.map(value => <li key={value}><button type="button" disabled={disabled} aria-label={translate('contentFilters.remove', { name: value })} className="break-all rounded-full border border-[var(--border-line)] px-3 py-1 text-sm disabled:opacity-40" onClick={() => void change(field, values.filter(item => item !== value))}>{value} <span aria-hidden="true">×</span></button></li>)}</ul>
    </div>
}
export function ContentFiltersSettings({ disabled = false }: { disabled?: boolean }) {
    useLanguage()
    const supported = typeof getApi().getContentFilters === 'function'
    const [value, setValue] = useState<ContentFilters>({ hiddenPlatforms: [], hiddenTags: [] }), [loaded, setLoaded] = useState(false), [busy, setBusy] = useState(false), [failed, setFailed] = useState(false)
    const live = useRef(false), lock = useRef(false), serial = useRef(0)
    const load = useCallback(async () => {
        if (lock.current) return
        const own = ++serial.current
        try { const value = await getApi().getContentFilters(); if (live.current && own === serial.current) { setValue(value); setLoaded(true); setFailed(false) } }
        catch { if (live.current && own === serial.current) setFailed(true) }
    }, [])
    useEffect(() => {
        live.current = true
        if (!supported) return () => { live.current = false }
        void load()
        const focus = () => { void load() }
        const timer = setInterval(() => { if (!document.hidden) void load() }, 10000)
        window.addEventListener('focus', focus)
        return () => { live.current = false; ++serial.current; clearInterval(timer); window.removeEventListener('focus', focus) }
    }, [supported, load])
    async function change(field: Field, values: string[]) {
        if (lock.current || disabled || !loaded) return false
        lock.current = true; ++serial.current; setBusy(true); setFailed(false)
        try {
            const saved = await getApi().setContentFilters({ ...value, [field]: values })
            contentFiltersChanged(saved)
            if (live.current) setValue(saved)
            return true
        } catch { if (live.current) setFailed(true); return false }
        finally { lock.current = false; if (live.current) setBusy(false) }
    }
    if (!supported) return null
    return <SettingSection title={translate('contentFilters.title')} description={translate('contentFilters.help')}>
        <div className="space-y-4 px-4 pb-4">
            <p className="text-xs text-[var(--text-muted)]">{translate('contentFilters.matching')}</p>
            <Picker kind="platform" field="hiddenPlatforms" values={value.hiddenPlatforms} disabled={disabled || busy || !loaded} change={change} />
            <Picker kind="tag" field="hiddenTags" values={value.hiddenTags} disabled={disabled || busy || !loaded} change={change} />
            {busy && <p role="status" className="text-sm">{translate('contentFilters.saving')}</p>}
            {failed && <p role="alert" className="text-sm text-red-500">{translate('contentFilters.saveError')} {!loaded && <button type="button" className="underline" onClick={() => void load()}>{translate('common.retry')}</button>}</p>}
        </div>
    </SettingSection>
}
