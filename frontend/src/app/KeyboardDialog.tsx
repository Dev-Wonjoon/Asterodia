import { useEffect, useRef, useState, useSyncExternalStore } from 'react'
import { translate, useLanguage } from '@renderer/shared/i18n'
import { availableCommands, shortcutKeys, shortcuts, shortcutVersion, subscribeShortcuts, formatShortcut, scopeTitle } from '../../../internal/webserver/web/shortcuts.js'

export function KeyboardDialog({ mode, onClose }: { mode: 'commands' | 'help'; onClose(): void }) {
    useLanguage()
    useSyncExternalStore(subscribeShortcuts, shortcutVersion)
    const dialog = useRef<HTMLDialogElement>(null)
    const [query, setQuery] = useState('')
    const [active, setActive] = useState(0)
    const helpScopes = useRef(document.querySelector('.sb-viewer[open]') ? ['viewer', 'player'] : [...new Set(availableCommands().map(item => item.scope))])
    const rows = (mode === 'commands' ? availableCommands().filter(item => !['app.commands', 'app.help'].includes(item.id) && !['gallery.left', 'gallery.right', 'gallery.up', 'gallery.down', 'gallery.first', 'gallery.last', 'gallery.menu'].includes(item.id)) : shortcuts.filter(item => helpScopes.current.includes(item.scope)).map(item => ({ ...item, title: translate(item.label), run: undefined })))
        .filter(item => item.title.toLowerCase().includes(query.toLowerCase()))
    useEffect(() => {
        const previous = document.activeElement as HTMLElement | null
        dialog.current?.showModal()
        dialog.current?.querySelector('input')?.focus()
        return () => { dialog.current?.close(); if (previous?.isConnected) previous.focus({ preventScroll: true }) }
    }, [])
    function execute(index: number) {
        const run = rows[index]?.run
        if (!run) return
        onClose()
        setTimeout(run, 0)
    }
    return <dialog ref={dialog} onCancel={event => { event.preventDefault(); onClose() }}
        aria-labelledby="keyboard-dialog-title" className="m-auto w-[min(92vw,48rem)] max-h-[85dvh] overflow-auto rounded-xl border border-[var(--border-line)] bg-[var(--bg-app)] p-5 text-[var(--text-main)] backdrop:bg-black/60"
        onKeyDown={event => {
            if (event.nativeEvent.isComposing || event.keyCode === 229) return
            if (mode === 'commands' && ['ArrowUp', 'ArrowDown'].includes(event.key)) {
                event.preventDefault(); setActive(index => Math.max(0, Math.min(rows.length - 1, index + (event.key === 'ArrowDown' ? 1 : -1))))
            }
            if (event.key === 'Enter' && event.target instanceof HTMLInputElement && mode === 'commands') { event.preventDefault(); execute(Math.min(active, rows.length - 1)) }
        }}>
        <header className="mb-4 flex items-center justify-between gap-4"><h2 id="keyboard-dialog-title" className="font-semibold">{mode === 'commands' ? translate("shortcuts.commandSearch") : translate("shortcuts.title")}</h2><button type="button" onClick={onClose}>{translate("common.close")} · Esc</button></header>
        <input aria-label={translate("shortcuts.commandSearch")} role={mode === 'commands' ? 'combobox' : undefined} aria-expanded={mode === 'commands' ? true : undefined}
            aria-controls={mode === 'commands' ? 'keyboard-command-list' : undefined} aria-autocomplete={mode === 'commands' ? 'list' : undefined}
            aria-activedescendant={mode === 'commands' && rows[active] ? 'keyboard-command-' + rows[active].id : undefined}
            value={query} onChange={event => { setQuery(event.target.value); setActive(0) }} className="mb-3 w-full rounded-lg border border-[var(--border-line)] bg-[var(--bg-sidebar)] p-3" />
        {mode === 'help' && <p className="mb-3 text-sm text-[var(--text-muted)]">{translate("shortcuts.useTabToMoveBetweenButtonsAndFieldsAnd")}</p>}
        {mode === 'help' && helpScopes.current.includes('gallery') && <p className="mb-3 text-sm text-[var(--text-muted)]">{translate('shortcuts.gallerySelectionHint')}</p>}
        <ul id="keyboard-command-list" role={mode === 'commands' ? 'listbox' : undefined} className="space-y-1">{rows.map((item, index) => <li key={item.id} role={mode === 'commands' ? 'presentation' : undefined}>
            <button type="button" id={'keyboard-command-' + item.id} role={mode === 'commands' ? 'option' : undefined} aria-selected={mode === 'commands' ? index === active : undefined} disabled={!item.run} onClick={() => execute(index)} aria-current={mode === 'commands' && index === active ? 'true' : undefined}
                ref={node => { if (mode === 'commands' && index === active) node?.scrollIntoView?.({ block: 'nearest' }) }}
                className="flex w-full items-center justify-between gap-4 rounded px-3 py-2 text-left aria-[current=true]:bg-[var(--bg-hover)] enabled:hover:bg-[var(--bg-hover)]">
                <span>{item.title}<small className="ml-2 text-[var(--text-muted)]">{scopeTitle(item.scope)}</small></span><kbd className="shrink-0 text-sm">{formatShortcut(shortcutKeys(item.id)) || translate("shortcuts.disabled")}</kbd>
            </button></li>)}</ul>
        {!rows.length && <p role="status">{translate("shortcuts.noSearchResults")}</p>}
    </dialog>
}
