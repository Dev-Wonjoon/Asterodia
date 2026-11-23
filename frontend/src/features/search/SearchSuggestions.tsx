import { translateMessage, translate, useLanguage } from '@renderer/shared/i18n'
import { useEffect, useId, useRef, useState } from 'react'
import type { MediaFilter, SearchSuggestion } from '@contracts/media-v2'
import { useSearchSuggestions } from './useSearchSuggestions'

const labels = { get title() { return translate("제목") }, get tag() { return translate("태그") }, get author() { return translate("작성자") }, get platform() { return translate("플랫폼") } }

export function SearchSuggestions({ value, filter, onChange, onSubmit, onSelect, onCompositionChange }: {
    value: string
    filter: MediaFilter
    onChange(value: string): void
    onSubmit(value: string): void
    onSelect(item: SearchSuggestion): void
    onCompositionChange(value: boolean): void
}) {
    useLanguage()
    const [open, setOpen] = useState(false)
    const [composing, setComposing] = useState(false)
    const [active, setActive] = useState(-1)
    const listId = useId()
    const root = useRef<HTMLDivElement>(null)
    const vm = useSearchSuggestions(value, filter, open, composing)
    const filterIdentity = JSON.stringify({ ...filter, keyword: undefined })
    const visible = open && !!value.trim() && !composing
    useEffect(() => setActive(-1), [value, filterIdentity])
    useEffect(() => { if (active >= 0) document.getElementById(listId + '-' + active)?.scrollIntoView?.({ block: 'nearest' }) }, [active, listId])
    function choose(item: SearchSuggestion) { root.current?.querySelector('input')?.focus({ preventScroll: true }); setOpen(false); setActive(-1); onSelect(item) }
    return <div ref={root} className="relative" onBlur={event => { if (!root.current?.contains(event.relatedTarget as Node)) setOpen(false) }}>
        <input type="text" role="combobox" aria-label={translate("미디어 검색")} aria-autocomplete="list" aria-expanded={visible} aria-controls={listId} aria-activedescendant={visible && active >= 0 && active < vm.data.length ? listId + '-' + active : undefined} autoComplete="off" maxLength={1000} value={value}
            onFocus={() => setOpen(true)}
            onChange={event => { setActive(-1); setOpen(true); onChange(event.target.value) }}
            onCompositionStart={() => { setComposing(true); onCompositionChange(true) }}
            onCompositionEnd={event => { onChange(event.currentTarget.value); setComposing(false); onCompositionChange(false) }}
            onKeyDown={event => {
                if (event.nativeEvent.isComposing || event.keyCode === 229 || composing) return
                if (event.key === 'Escape') { event.preventDefault(); setOpen(false); setActive(-1) }
                else if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
                    event.preventDefault(); setOpen(true)
                    if (vm.data.length) { const next = event.key === 'ArrowDown' ? Math.min(active + 1, vm.data.length - 1) : Math.max(active - 1, 0); setActive(next); if (next === vm.data.length - 1) vm.more() }
                } else if (event.key === 'Enter') {
                    event.preventDefault()
                    if (visible && active >= 0 && vm.data[active]) choose(vm.data[active])
                    else { setOpen(false); onSubmit(value) }
                }
            }} placeholder={translate("제목·태그·작성자·플랫폼으로 검색…")} className="w-full rounded-lg border border-[var(--border-line)] bg-[var(--bg-hover)] px-4 py-2 text-[var(--text-main)] placeholder-[var(--text-placeholder)] focus:border-[var(--color-primary)] focus:outline-none" />
        {visible && <div className="absolute top-full z-30 mt-1 w-full rounded-lg border border-[var(--border-line)] bg-[var(--bg-sidebar)] shadow-lg">
            <div id={listId} role="listbox" aria-label={translate("검색 추천")} aria-busy={vm.loading} className="max-h-80 overflow-auto overscroll-contain" onScroll={event => { const node = event.currentTarget; if (node.scrollHeight - node.scrollTop - node.clientHeight < 48) vm.more() }}>
                {vm.data.map((item, index) => <button key={item.kind + ':' + item.value} id={listId + '-' + index} type="button" role="option" tabIndex={-1} aria-selected={index === active} onMouseDown={event => event.preventDefault()} onClick={() => choose(item)} className={'flex w-full items-start gap-3 px-3 py-2 text-left hover:bg-[var(--bg-hover)] ' + (index === active ? 'bg-[var(--bg-hover)]' : '')}><span className="shrink-0 text-xs text-[var(--text-muted)]">{labels[item.kind]}</span><span className="min-w-0 break-words text-sm">{item.value}</span></button>)}
            </div>
            {vm.loading && <p role="status" className="p-2 text-xs text-[var(--text-muted)]">{translate("추천을 찾고 있습니다…")}</p>}
            {vm.error ? <div role="alert" className="p-2 text-sm">{translateMessage(vm.error)} <button type="button" onMouseDown={event => event.preventDefault()} onClick={vm.retry} className="underline">{translate("다시 시도")}</button></div> : !vm.loading && !vm.data.length && <p role="status" className="p-3 text-sm text-[var(--text-muted)]">{translate("일치하는 후보가 없습니다.")}</p>}
            {vm.hasNextPage && !vm.loading && !vm.error && <button type="button" onMouseDown={event => event.preventDefault()} onClick={vm.more} className="w-full border-t border-[var(--border-line)] p-2 text-sm">{translate("추천 더 보기")}</button>}
        </div>}
    </div>
}
