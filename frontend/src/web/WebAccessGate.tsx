import { createContext, useContext, useEffect, useRef, useState, type ReactNode } from 'react'
import { translate, useLanguage } from '@renderer/shared/i18n'
import { LanguageSelect } from '@renderer/shared/i18n/LanguageSelect'

const CodeRequiredContext = createContext(true)
export const useCodeRequired = () => useContext(CodeRequiredContext)

export function WebAccessGate({ children }: { children: ReactNode }) {
    useLanguage()
    const [state, setState] = useState<'checking' | 'locked' | 'ready'>('checking')
    const [code, setCode] = useState('')
    const [busy, setBusy] = useState(false)
    const [error, setError] = useState('')
    const [required, setRequired] = useState(true)
    const revision = useRef(0)
    const connecting = useRef(false)
    useEffect(() => {
        let mounted = true
        let checking = false
        const check = async () => {
            if (connecting.current || checking) return
            checking = true
            const own = ++revision.current
            try {
                const response = await fetch('/api/access/status', { credentials: 'same-origin', cache: 'no-store', signal: AbortSignal.timeout(10_000) })
                if (!response.ok) throw new Error()
                const access = await response.json() as { authenticated: boolean; required?: boolean }
                if (mounted && own === revision.current) { setRequired(access.required !== false); setState(access.authenticated ? 'ready' : 'locked') }
            } catch { if (mounted && own === revision.current) { setState('locked'); setError(translate('security.connectionError')) } }
            finally { checking = false }
        }
        const lock = () => { revision.current++; connecting.current = false; setBusy(false); setCode(''); setError(''); setState('locked') }
        const logout = async () => {
            try {
                const response = await fetch('/api/access/logout', { method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json' }, body: '{}' })
                if (!response.ok) throw new Error()
                const access = await response.json() as { authenticated: boolean; required?: boolean }
                if (mounted) { lock(); setRequired(access.required !== false); if (access.authenticated) setState('ready') }
            } catch { if (mounted) setError(translate('security.connectionError')) }
        }
        void check()
        const timer = setInterval(() => void check(), 2000)
        window.addEventListener('storagebucket:access-expired', lock)
        window.addEventListener('storagebucket:disconnect', logout)
        return () => { mounted = false; revision.current++; clearInterval(timer); window.removeEventListener('storagebucket:access-expired', lock); window.removeEventListener('storagebucket:disconnect', logout) }
    }, [])
    const connect = async (event: React.FormEvent) => {
        event.preventDefault()
        if (busy || code.length !== 8) return
        const own = ++revision.current
        connecting.current = true
        setBusy(true); setError('')
        try {
            const response = await fetch('/api/access/connect', { method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ code }) })
            if (own !== revision.current) return
            if (!response.ok) { setError(translate(response.status === 429 ? 'security.rateLimited' : 'security.invalidCode')); return }
            setCode(''); setState('ready')
        } catch { if (own === revision.current) setError(translate('security.connectionError')) }
        finally { if (own === revision.current) { connecting.current = false; setBusy(false) } }
    }
    if (state === 'ready') return <CodeRequiredContext.Provider value={required}>{error && <p role="alert" className="fixed bottom-4 right-4 z-50 max-w-sm rounded border border-red-500 bg-[var(--bg-app)] p-3 text-sm text-red-500">{error}</p>}{children}</CodeRequiredContext.Provider>
    return <main className="flex min-h-dvh items-center justify-center bg-[var(--bg-app)] p-6 text-[var(--text-main)]">
        <form onSubmit={event => void connect(event)} className="w-full max-w-sm space-y-4 rounded-xl border border-[var(--border-line)] bg-[var(--bg-sidebar)] p-6">
            <div className="flex items-center justify-between gap-4"><h1 className="text-lg font-semibold">StorageBucket</h1><LanguageSelect /></div>
            <p>{translate(state === 'checking' ? 'settings.checkingStatus' : 'security.connectHelp')}</p>
            <label className="block space-y-2"><span>{translate('security.code')}</span><input autoComplete="one-time-code" inputMode="numeric" pattern="[0-9]{8}" maxLength={8} value={code} onChange={event => setCode(event.target.value.replace(/\D/g, ''))} disabled={busy || state === 'checking'} className="w-full rounded border border-[var(--border-line)] bg-[var(--bg-app)] px-3 py-2 text-xl tracking-widest" /></label>
            {error && <p role="alert" className="text-sm text-red-500">{error}</p>}
            <button type="submit" disabled={busy || state === 'checking' || code.length !== 8} className="rounded bg-[var(--color-primary)] px-4 py-2 text-white disabled:opacity-50">{translate('security.connect')}</button>
        </form>
    </main>
}
