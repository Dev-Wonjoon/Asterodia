import { useEffect, useRef, useState } from 'react'
import { getApi } from '@renderer/shared/api'
import { translate, useLanguage } from '@renderer/shared/i18n'
import { SettingRow } from '@renderer/shared/components/ui/SettingRow'
import { Button } from '@renderer/shared/components/ui/Button'

export function WorkingDirectorySettings({ disabled = false }: { disabled?: boolean }) {
    useLanguage()
    const [path, setPath] = useState(''), [busy, setBusy] = useState(false), [failed, setFailed] = useState(false)
    const generation = useRef(0), changing = useRef(false)
    useEffect(() => {
        let live = true
        const own = generation.current
        void Promise.resolve().then(() => getApi().getWorkingDirectory()).then(value => { if (live && own === generation.current) setPath(value || '') }).catch(() => { if (live && own === generation.current) setFailed(true) })
        return () => { live = false }
    }, [])
    async function change(clear = false) {
        if (changing.current) return
        changing.current = true; generation.current++
        setBusy(true); setFailed(false)
        try {
            if (clear) { if (!await getApi().clearWorkingDirectory()) throw Error(); setPath('') }
            else { const value = await getApi().setWorkingDirectory(); if (value) setPath(value) }
        } catch { setFailed(true) }
        finally { changing.current = false; setBusy(false) }
    }
    return <>
        <SettingRow label={translate('settings.workingDirectory')} value={path || translate('settings.workingDirectoryFallback')}
            action={<div className="flex gap-2"><Button disabled={disabled || busy} onClick={() => void change()}>{translate('settings.change')}</Button>
                {path && <Button disabled={disabled || busy} onClick={() => void change(true)}>{translate('settings.workingDirectoryReset')}</Button>}</div>} />
        <p className="px-4 text-xs text-[var(--text-muted)]">{translate('settings.workingDirectoryHelp')}</p>
        {failed && <p role="alert" className="px-4 text-sm text-red-500">{translate('settings.workingDirectoryError')}</p>}
    </>
}
