import { translate, useLanguage } from '@renderer/shared/i18n'
import { useEffect, useId, useRef, useState } from 'react'
import { BaseModal } from '@renderer/shared/components/ui/BaseModal'

export function GalleryBulkDialog({ count, remove, kind = 'tags', onApply, onClose }: { count: number; remove: boolean; kind?: 'tags' | 'platforms'; onApply: (names: string[]) => void; onClose: () => void }) {
    useLanguage()
    const [text, setText] = useState('')
    const title = useId(), input = useRef<HTMLTextAreaElement>(null), panel = useRef<HTMLElement>(null)
    const names = [...new Set(text.split(/[,\n]/).map(value => value.trim()).filter(Boolean))]
    useEffect(() => {
        const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null
        const background = document.getElementById('root')
        const wasInert = background?.inert ?? false
        if (background) background.inert = true
        input.current?.focus()
        const trap = (event: KeyboardEvent) => {
            if (event.key !== 'Tab') return
            const items = [...panel.current?.querySelectorAll<HTMLElement>('textarea,button:not(:disabled)') ?? []]
            const first = items[0], last = items[items.length - 1]
            if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus() }
            else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus() }
        }
        document.addEventListener('keydown', trap, true)
        return () => { document.removeEventListener('keydown', trap, true); if (background) background.inert = wasInert; if (previous?.isConnected) previous.focus() }
    }, [])
    return <BaseModal onClose={onClose}>
        <section ref={panel} data-detail-trigger role="dialog" aria-modal="true" aria-labelledby={title} className="w-[min(28rem,calc(100vw-2rem))] space-y-4 p-6 text-[var(--text-main)]">
            <h2 id={title} className="font-semibold">{kind === 'platforms' ? translate(remove ? 'platforms.remove' : 'platforms.add') : translate("gallery.tags", { count: remove ? translate("gallery.action.remove") : translate("gallery.add") })}</h2>
            <p className="text-sm">{translate(kind === 'platforms' ? 'platforms.bulkDescription' : 'gallery.appliesToSelectedPostsOtherTagsAreKept', { count })}</p>
            <label className="block space-y-2 text-sm"><span>{translate(kind === 'platforms' ? 'platforms.names' : 'gallery.tagNamesSeparatedByCommasOrNewLines')}</span><textarea ref={input} value={text} onChange={event => setText(event.target.value)} rows={4} className="w-full rounded-lg border border-[var(--border-line)] bg-[var(--bg-sidebar)] p-2" /></label>
            <div className="flex justify-end gap-2"><button type="button" onClick={onClose} className="rounded-lg border border-[var(--border-line)] px-3 py-2 text-sm">{translate("common.cancel")}</button><button type="button" disabled={!names.length} onClick={() => onApply(names)} className="rounded-lg bg-[var(--color-primary)] px-3 py-2 text-sm text-white disabled:opacity-40">{translate("gallery.apply")}</button></div>
        </section>
    </BaseModal>
}
