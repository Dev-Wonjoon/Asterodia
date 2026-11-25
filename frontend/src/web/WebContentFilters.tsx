import { useId, useState } from 'react'
import { ContentFiltersSettings } from '../features/settings/ContentFiltersSettings'
import { ContentFilterNotice } from '../shared/components/ui/ContentFilterNotice'
import type { useContentVisibility } from '../shared/hooks/useContentVisibility'
import { translate, useLanguage } from '../shared/i18n'

export function WebContentFilters({ visibility, disabled }: { visibility: ReturnType<typeof useContentVisibility>; disabled: boolean }) {
    useLanguage()
    const [open, setOpen] = useState(false)
    const id = useId()
    return <div data-action-boundary className="space-y-3">
        <ContentFilterNotice visibility={visibility} disabled={disabled} onManage={() => setOpen(value => !value)} expanded={open} controls={id} />
        {open && <section id={id} aria-label={translate('contentFilters.manage')} className="space-y-3 rounded-lg border border-[var(--border-line)] p-4">
            <p className="text-sm text-[var(--text-muted)]">{translate('contentFilters.shared')}</p>
            <ContentFiltersSettings disabled={disabled} />
        </section>}
    </div>
}
