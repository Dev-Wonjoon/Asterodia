import { useEffect, useRef } from 'react'
import { blockingDialog, isTextInput, matchShortcut, registerCommands } from '../../../../internal/webserver/web/shortcuts.js'

type Command = { run: (event?: KeyboardEvent) => boolean | void; enabled?: () => boolean }
export function useShortcutCommands(scope: string, commands: Record<string, Command>, accepts?: (event: KeyboardEvent) => boolean) {
    const current = useRef({ commands, accepts })
    current.current = { commands, accepts }
    useEffect(() => {
        const unregister = registerCommands(Object.keys(current.current.commands).map(id => ({ id,
            enabled: () => current.current.commands[id]?.enabled?.() ?? true,
            run: () => current.current.commands[id]?.run(),
        })))
        const keydown = (event: KeyboardEvent) => {
            const dialog = blockingDialog()
            const helpInViewer = scope === 'app' && matchShortcut(event, scope)?.id === 'app.help' && dialog?.classList.contains('sb-viewer')
            if (dialog && !helpInViewer || current.current.accepts?.(event) === false) return
            if (scope !== 'app' && isTextInput(event.target)) return
            let item = matchShortcut(event, scope)
            // Shift + navigation extends gallery selection using the same configurable direction.
            if (!item && scope === 'gallery' && event.shiftKey && !event.defaultPrevented) {
                item = matchShortcut({ key: event.key, code: event.code, shiftKey: false, ctrlKey: event.ctrlKey, altKey: event.altKey, metaKey: event.metaKey,
                    repeat: event.repeat, isComposing: event.isComposing, keyCode: event.keyCode, getModifierState: event.getModifierState.bind(event) }, scope)
                if (item && !['gallery.left', 'gallery.right', 'gallery.up', 'gallery.down', 'gallery.first', 'gallery.last'].includes(item.id)) item = undefined
            }
            if (!item) return
            const command = current.current.commands[item.id]
            if (!command || command.enabled?.() === false) return
            if (command.run(event) !== false) { event.preventDefault(); event.stopPropagation() }
        }
        document.addEventListener('keydown', keydown)
        return () => { unregister(); document.removeEventListener('keydown', keydown) }
    }, [scope])
}
