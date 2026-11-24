import { useState, useSyncExternalStore } from 'react'
import { translate, useLanguage } from '@renderer/shared/i18n'
import { eventChord, isComposing, resetShortcuts, setShortcut, shortcutKeys, shortcuts, shortcutVersion, subscribeShortcuts, scopeTitle } from '../../../../internal/webserver/web/shortcuts.js'

export function ShortcutSettings() {
    useLanguage()
    useSyncExternalStore(subscribeShortcuts, shortcutVersion)
    const [error, setError] = useState('')
    return <section className="space-y-4">
        <h2 className="text-lg font-semibold">{translate("shortcuts.title")}</h2>
        <p className="text-sm text-[var(--text-muted)]">{translate("shortcuts.focusAShortcutFieldAndPressTheDesiredKey")}</p>
        <button type="button" onClick={() => { resetShortcuts(); setError('') }} className="rounded border border-[var(--border-line)] px-3 py-2">{translate("shortcuts.restoreDefaults")}</button>
        {error && <p role="alert">{error === 'conflict' ? translate("shortcuts.thisShortcutIsAlreadyAssignedToAnotherFunctionUsed") : translate("shortcuts.tabAndEscAreReservedForStandardNavigation")}</p>}
        <div className="space-y-2">{shortcuts.map(item => <label key={item.id} className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-[var(--border-line)] px-3 py-2">
            <span>{translate(item.label)} <small className="text-[var(--text-muted)]">{scopeTitle(item.scope)}</small></span>
            <input readOnly value={shortcutKeys(item.id) || translate("shortcuts.disabled")} aria-label={translate(item.label) + ' · ' + scopeTitle(item.scope)}
                className="w-48 rounded border border-[var(--border-line)] bg-[var(--bg-sidebar)] px-3 py-2 text-center"
                onKeyDown={event => {
                    if (event.key === 'Tab') return
                    event.preventDefault(); event.stopPropagation()
                    if (isComposing(event.nativeEvent)) return
                    if (event.key === 'Escape') { event.currentTarget.blur(); return }
                    const keys = event.key === 'Backspace' ? '' : eventChord(event.nativeEvent)
                    if (!keys && event.key !== 'Backspace') return
                    setError(setShortcut(item.id, keys) || '')
                }} />
        </label>)}</div>
    </section>
}
