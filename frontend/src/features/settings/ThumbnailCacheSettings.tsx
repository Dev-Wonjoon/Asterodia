import { useState } from 'react'
import { getApi } from '@renderer/shared/api'
import { translate, useLanguage } from '@renderer/shared/i18n'
import { useConfirm } from '@renderer/shared/hooks/useConfirm'
import { SettingSection } from '@renderer/shared/components/ui/SettingSection'
import { Button } from '@renderer/shared/components/ui/Button'
import type { ThumbnailCacheUsage } from '@contracts/thumbnail-api'

export function ThumbnailCacheSettings() {
    useLanguage()
    const { confirm } = useConfirm()
    const [usage, setUsage] = useState<ThumbnailCacheUsage | null>(null), [busy, setBusy] = useState(false), [error, setError] = useState('')
    const api = getApi().thumbnails
    const bytes = (value: number) => `${(value / 1024 / 1024).toFixed(1)} MiB`
    async function run(clean: boolean) {
        if (busy || !api.cacheUsage || clean && !api.clearUnusedCache) return
        if (clean && !await confirm({ message: translate('cache.confirm'), confirmLabel: translate('cache.clean') })) return
        setBusy(true); setError('')
        try {
            const result = await (clean ? api.clearUnusedCache!() : api.cacheUsage())
            if (result.ok) setUsage(result.data)
            else setError(translate('cache.failed'))
        } catch { setError(translate('cache.failed')) }
        finally { setBusy(false) }
    }
    if (!api.cacheUsage) return null
    return <SettingSection title={translate('cache.title')} description={translate('cache.description')}>
        <div className="space-y-3 px-4 py-3 text-sm">
            {usage && <><p role="status">{translate('cache.usage', { count: usage.files, size: bytes(usage.bytes) })}</p>
                <p className="text-[var(--text-muted)]">{translate('cache.unused', { count: usage.unusedFiles, size: bytes(usage.unusedBytes) })}</p>
                {usage.removedFiles > 0 && <p>{translate('cache.removed', { count: usage.removedFiles, size: bytes(usage.removedBytes) })}</p>}</>}
            {busy && <p role="status">{translate('cache.working')}</p>}
            {error && <p role="alert">{error}</p>}
            <div className="flex gap-2"><Button disabled={busy} onClick={() => void run(false)}>{translate('cache.inspect')}</Button>
                <Button disabled={busy || !usage?.unusedFiles} onClick={() => void run(true)}>{translate('cache.clean')}</Button></div>
        </div>
    </SettingSection>
}
