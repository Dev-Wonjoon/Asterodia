import { translateError, translate, useLanguage } from '@renderer/shared/i18n'
import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from 'react'
import type { UpdateStatus, UpdateApi, UpdateResult } from '@contracts/update-api'
import { getApi } from '@renderer/shared/api'
import { useConfirm } from '@renderer/shared/hooks/useConfirm'
import type { ConfirmOptions } from '@renderer/shared/hooks/confirmContext'
import { LeaveGuardContext, type LeaveGuardRef } from '@renderer/shared/hooks/leaveGuardContext'
import { formatBytes } from '@renderer/shared/utils/formatBytes'

type Action = Exclude<keyof UpdateApi, 'status'>
type PromptContent =
    | { kind: 'available' | 'ready'; status: UpdateStatus }
    | { kind: 'latest' }
    | { kind: 'error'; message: string; retry?: 'check' | 'download' }
type Prompt = PromptContent & { id: number }

function promptOptions(prompt: Prompt): ConfirmOptions {
    switch (prompt.kind) {
        case 'available': return {
            title: translate("settings.newVersionUpdate"),
            message: translate("settings.currentVersionNewVersionDownloadTheUpdate", { currentVersion: prompt.status.currentVersion, newVersion: prompt.status.version, size: formatBytes(prompt.status.size), notes: prompt.status.notes || translate("settings.noReleaseNotesAvailable") }),
            confirmLabel: translate("settings.update"), cancelLabel: translate("settings.later"),
        }
        case 'ready': return {
            title: translate("settings.updateReady"),
            message: translate("settings.storagebucketHasBeenDownloadedAndVerifiedRestartTheApp", { version: prompt.status.version }),
            confirmLabel: translate("settings.restartNow"), cancelLabel: translate("settings.later"),
        }
        case 'latest': return { title: translate("settings.checkForUpdates"), message: translate("settings.youAreUpToDate"), confirmLabel: translate("common.confirm"), cancelLabel: translate("common.close") }
        case 'error': return { title: translate("settings.updateFailed"), message: prompt.message, confirmLabel: prompt.retry ? translate("common.retry") : translate("common.confirm"), cancelLabel: translate("common.close") }
    }
}

interface UpdateView {
    status: UpdateStatus | null
    checking: boolean
    pending: Action | null
    error: string
    act(action: Action): Promise<void>
}
const UpdateContext = createContext<UpdateView | null>(null)
export const useAppUpdate = () => useContext(UpdateContext)

export function AppUpdateProvider({ children, blocked = false, leaveGuard }: { children: ReactNode; blocked?: boolean; leaveGuard?: LeaveGuardRef }) {
    useLanguage()
    const [status, setStatus] = useState<UpdateStatus | null>(null)
    const [pending, setPending] = useState<Action | null>(null)
    const [error, setError] = useState('')
    const [initialCheckPending, setInitialCheckPending] = useState(true)
    const [prompts, setPrompts] = useState<Prompt[]>([])
    const { confirm, isOpen } = useConfirm()
    const contextGuard = useContext(LeaveGuardContext)
    const guard = leaveGuard ?? contextGuard
    const alive = useRef(false)
    const generation = useRef(0)
    const locked = useRef(false)
    const startupHandled = useRef(false)
    const prompting = useRef(false)
    const promptSequence = useRef(0)
    const enqueue = useCallback((prompt: PromptContent) => {
        const entry = { ...prompt, id: ++promptSequence.current }
        setPrompts(previous => {
            if ((prompt.kind === 'available' || prompt.kind === 'ready') && previous.some(queued =>
                (queued.kind === 'available' || queued.kind === 'ready') && queued.kind === prompt.kind && queued.status.version === prompt.status.version)) return previous
            return [...previous, entry]
        })
    }, [])
    useEffect(() => {
        alive.current = true
        let stopped = false
        let timer: ReturnType<typeof setTimeout> | undefined
        const startupDeadline = Date.now() + 25_000
        async function poll() {
            const own = generation.current
            let delay = 5000
            try {
                const result = await getApi().updates.status()
                if (stopped || own !== generation.current) return
                if (result.ok) {
                    setStatus(result.data)
                    setError(result.data.error)
                    if (['checking', 'downloading', 'verifying', 'installing', 'restarting'].includes(result.data.state)) delay = 750
                    if (result.data.supported && ['idle', 'unconfigured', 'checking'].includes(result.data.state) && Date.now() < startupDeadline && !startupHandled.current) delay = 250
                    else setInitialCheckPending(false)
                } else { setError(translateError(result.error)); setInitialCheckPending(false) }
            } catch {
                if (!stopped && own === generation.current) {
                    setError(translate("settings.couldNotCheckUpdateStatus"))
                    setInitialCheckPending(false)
                }
            } finally {
                if (!stopped) timer = setTimeout(() => void poll(), delay)
            }
        }
        void poll()
        return () => { stopped = true; alive.current = false; generation.current++; if (timer) clearTimeout(timer) }
    }, [])

    const request = useCallback(async (action: Action): Promise<UpdateResult | null> => {
        if (locked.current) return null
        locked.current = true
        generation.current++
        setPending(action); setError('')
        try {
            const result = await getApi().updates[action]()
            if (!alive.current) return null
            generation.current++
            if (result.ok) { setStatus(result.data); setError(result.data.error) }
            else setError(translateError(result.error))
            return result
        } catch {
            if (!alive.current) return null
            const message = translate("settings.noUpdateResponseReceivedCheckStatusBeforeTryingAgain")
            setError(message)
            return { ok: false, error: { code: 'UPDATE_REQUEST_FAILED', message } }
        } finally {
            locked.current = false
            if (alive.current) setPending(null)
        }
    }, [])

    const run = useCallback(async (action: Action) => {
        const result = await request(action)
        if (!result || !alive.current) return
        if (!result.ok || result.data.error || result.data.state === 'error') {
            enqueue({ kind: 'error', retry: action === 'restart' ? undefined : action, message: result.ok ? result.data.error || translate("settings.couldNotCompleteTheUpdate") : translateError(result.error) })
            return
        }
        const data = result.data
        if (action === 'check' && data.state === 'available') enqueue({ kind: 'available', status: data })
        else if (action === 'check' && data.state === 'up-to-date') enqueue({ kind: 'latest' })
        else if (action !== 'restart' && data.state === 'ready') enqueue({ kind: 'ready', status: data })
    }, [request, enqueue])

    const act = useCallback(async (action: Action) => {
        startupHandled.current = true
        setInitialCheckPending(false)
        if (action === 'restart') {
            if (status?.state === 'ready' && !prompting.current && prompts.length === 0) enqueue({ kind: 'ready', status })
        } else await run(action)
    }, [status, prompts.length, enqueue, run])

    useEffect(() => {
        if (startupHandled.current || !status || pending) return
        if (status.state === 'available' || status.state === 'ready') {
            startupHandled.current = true
            enqueue({ kind: status.state, status })
        } else if (['up-to-date', 'error', 'unsupported'].includes(status.state)) startupHandled.current = true
    }, [status, pending, enqueue])

    useEffect(() => {
        const prompt = prompts[0]
        if (!prompt || blocked || isOpen || prompting.current) return
        prompting.current = true
        void (async () => {
            try {
                if (!alive.current) return
                if (!await confirm(promptOptions(prompt)) || !alive.current) return
                if (prompt.kind === 'available') await run('download')
                else if (prompt.kind === 'ready') {
                    if (guard?.current && !await guard.current()) return
                    if (alive.current) await run('restart')
                } else if (prompt.kind === 'error' && prompt.retry) await run(prompt.retry)
            } catch {
                if (alive.current) {
                    const message = translate("settings.couldNotConfirmUpdateProgressSaveYourWorkAnd")
                    setError(message)
                    enqueue({ kind: 'error', message })
                }
            } finally {
                prompting.current = false
                if (alive.current) setPrompts(previous => previous.filter(entry => entry.id !== prompt.id))
            }
        })()
    }, [prompts, blocked, isOpen, guard, confirm, run, enqueue])

    const checking = !error && (initialCheckPending || status?.state === 'checking' || pending === 'check')
    return <UpdateContext.Provider value={{ status, checking, pending, error, act }}>{children}</UpdateContext.Provider>
}
