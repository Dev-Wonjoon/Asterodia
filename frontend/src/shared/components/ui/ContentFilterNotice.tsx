import { translate, useLanguage } from '@renderer/shared/i18n'
import type { useContentVisibility } from '@renderer/shared/hooks/useContentVisibility'

export function ContentFilterNotice({ visibility, disabled = false, onManage, expanded, controls }: { visibility: ReturnType<typeof useContentVisibility>; disabled?: boolean; onManage?: () => void; expanded?: boolean; controls?: string }) {
    useLanguage()
    const platforms = visibility.hiddenPlatforms.length, tags = visibility.hiddenTags.length
    if (!platforms && !tags && !visibility.failed && !onManage) return null
    return <section data-action-boundary className="flex flex-wrap items-center gap-3 rounded-lg border border-[var(--border-line)] px-3 py-2 text-sm" aria-label={translate('contentFilters.title')}>
        <span className="text-[var(--text-muted)]">{translate(visibility.showHidden ? 'contentFilters.showing' : 'contentFilters.active', { platforms, tags })}</span>
        {(platforms > 0 || tags > 0 || visibility.showHidden) && <label className="ml-auto flex items-center gap-2"><input type="checkbox" checked={visibility.showHidden} disabled={disabled} onChange={event => visibility.setShowHidden(event.target.checked)} />{translate('contentFilters.showHidden')}</label>}
        {onManage && <button type="button" disabled={disabled} aria-expanded={expanded} aria-controls={controls} onClick={onManage} className="rounded-lg border border-[var(--border-line)] px-3 py-1 disabled:opacity-40">{translate('contentFilters.manage')}</button>}
        {visibility.failed && <p role="alert" className="w-full text-red-500">{translate('contentFilters.readError')}</p>}
    </section>
}
