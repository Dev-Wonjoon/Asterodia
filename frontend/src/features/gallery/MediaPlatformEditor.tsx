import { useEffect, useId, useState } from 'react'
import { getApi } from '@renderer/shared/api'
import { translate, useLanguage } from '@renderer/shared/i18n'

export function MediaPlatformEditor({ values, disabled, onChange }: {
    values: { id: number; name: string }[]; disabled: boolean
    onChange(change: { addNames?: string[]; removeNames?: string[] }): Promise<boolean>
}) {
    useLanguage()
    const [text, setText] = useState(''), [suggestions, setSuggestions] = useState<string[]>([])
    const listId = useId()
    useEffect(() => {
        let active = true
        const timer = setTimeout(() => {
            void getApi().media.suggest({ kind: 'platform', text, limit: 20 }).then(result => {
                if (active) setSuggestions(result.ok ? result.data : [])
            }).catch(() => { if (active) setSuggestions([]) })
        }, 150)
        return () => { active = false; clearTimeout(timer) }
    }, [text])
    return <section className="space-y-2" aria-label={translate('platforms.title')}>
        <h3 className="text-sm font-medium">{translate('platforms.title')}</h3>
        <p className="text-xs text-[var(--text-muted)]">{translate('platforms.description')}</p>
        <div className="flex flex-wrap gap-2">{values.map(value => <span key={value.id} className="inline-flex items-center gap-2 rounded-full border border-[var(--border-line)] px-3 py-1 text-sm">
            {value.name}<button type="button" disabled={disabled} aria-label={translate('platforms.unlink', { name: value.name })} onClick={() => void onChange({ removeNames: [value.name] })}>×</button>
        </span>)}</div>
        <form className="flex gap-2" onSubmit={event => {
            event.preventDefault()
            const submitted = text.trim()
            if (!submitted || disabled) return
            void onChange({ addNames: [submitted] }).then(saved => { if (saved) setText(current => current.trim() === submitted ? '' : current) })
        }}>
            <input aria-label={translate('platforms.input')} list={listId} maxLength={100} value={text} disabled={disabled} onChange={event => setText(event.target.value)} className="min-w-0 flex-1 rounded-lg border border-[var(--border-line)] bg-[var(--bg-app)] px-3 py-2 text-sm" />
            <datalist id={listId}>{suggestions.map(name => <option key={name} value={name} />)}</datalist>
            <button type="submit" disabled={disabled || !text.trim()} className="rounded-lg border border-[var(--border-line)] px-3 py-2 text-sm disabled:opacity-40">{translate('gallery.add')}</button>
        </form>
    </section>
}
