import { translateMessage, translate, useLanguage } from '@renderer/shared/i18n'
import { useState, type ComponentType } from 'react'
import type { MediaFilter } from '@contracts/media-v2'
import { useSearchViewModel } from '@renderer/features/search/useSearchViewModel'
import { SearchSuggestions } from './SearchSuggestions'

export const SearchPage = ({ Gallery }: { Gallery: ComponentType<{ title: string; filter: MediaFilter; emptyMessage: string }> }): React.JSX.Element => {
    useLanguage()
    const vm = useSearchViewModel()
    const [tagInput, setTagInput] = useState('')
    const [platformInput, setPlatformInput] = useState('')
    const [authorInput, setAuthorInput] = useState('')

    // 태그 입력 핸들러
    const handleTagInput = (value: string): void => {
        setTagInput(value)
        vm.suggestTags(value)
    }

    const handleTagKeyDown = (e: React.KeyboardEvent): void => {
        if (!e.nativeEvent.isComposing && e.keyCode !== 229 && e.key === 'Enter' && tagInput.trim()) {
            vm.addTag(tagInput.trim())
            setTagInput('')
        }
    }

    // 플랫폼 입력 핸들러
    const handlePlatformInput = (value: string): void => {
        setPlatformInput(value)
        vm.suggestPlatforms(value)
    }

    const handlePlatformKeyDown = (e: React.KeyboardEvent): void => {
        if (!e.nativeEvent.isComposing && e.keyCode !== 229 && e.key === 'Enter' && platformInput.trim()) {
            vm.addPlatform(platformInput.trim())
            setPlatformInput('')
        }
    }

    // 작성자 입력 핸들러
    const handleAuthorInput = (value: string): void => {
        setAuthorInput(value)
        vm.suggestAuthors(value)
    }

    const handleAuthorKeyDown = (e: React.KeyboardEvent): void => {
        if (!e.nativeEvent.isComposing && e.keyCode !== 229 && e.key === 'Enter' && authorInput.trim()) {
            vm.addAuthor(authorInput.trim())
            setAuthorInput('')
        }
    }

    return (
        <div className="flex h-full min-h-0 min-w-0 flex-col">
            {/* 검색 영역 */}
            <div data-keyboard-region tabIndex={-1} className="shrink-0 p-4 border-b border-[var(--border-line)] space-y-3">
                {/* 키워드 검색 */}
                <SearchSuggestions value={vm.keyword} filter={vm.filter} onChange={vm.setKeyword} onSubmit={vm.submitKeyword} onCompositionChange={vm.setSearchComposing} onSelect={item => {
                    if (item.kind === 'title') vm.submitKeyword(item.value)
                    else { if (item.kind === 'tag') vm.addTag(item.value); else if (item.kind === 'author') vm.addAuthor(item.value); else vm.addPlatform(item.value); vm.submitKeyword('') }
                }} />

                {/* 필터 입력 영역 */}
                <div className="grid min-w-0 grid-cols-1 gap-3 sm:grid-cols-3">
                    {/* 태그 필터 */}
                    <div className="min-w-0 relative">
                        <div className="flex items-center gap-2">
                            <input
                                type="text"
                                aria-label={translate("gallery.addTags")}
                                value={tagInput}
                                maxLength={200}
                                onChange={(e) => handleTagInput(e.target.value)}
                                onKeyDown={handleTagKeyDown}
                                placeholder={translate("search.addTag")}
                                className="min-w-0 flex-1 px-3 py-1.5 text-sm rounded-lg
                                    bg-[var(--bg-hover)] text-[var(--text-main)]
                                    placeholder-[var(--text-placeholder)]
                                    border border-[var(--border-line)]
                                    focus:outline-none focus:border-[var(--color-primary)]"
                            />
                            {/* AND/OR 토글 */}
                            {vm.selectedTags.length > 1 && (
                                <button
                                    onClick={() =>
                                        vm.setTagMode(vm.tagMode === 'and' ? 'or' : 'and')
                                    }
                                    className={`px-2 py-1.5 text-xs font-bold rounded-lg border transition-colors
                                        ${
                                            vm.tagMode === 'and'
                                                ? 'bg-[var(--color-primary)] text-white border-[var(--color-primary)]'
                                                : 'bg-[var(--bg-hover)] text-[var(--text-main)] border-[var(--border-line)]'
                                        }`}
                                >
                                    {vm.tagMode.toUpperCase()}
                                </button>
                            )}
                        </div>
                        {/* 태그 자동완성 */}
                        {vm.tagSuggestions.length > 0 && (
                            <div className="absolute z-20 top-full mt-1 w-full rounded-lg border border-[var(--border-line)] bg-(--bg-popup) shadow-lg overflow-hidden">
                                {vm.tagSuggestions.map((tag) => (
                                    <button
                                        key={tag}
                                        onClick={() => {
                                            vm.addTag(tag)
                                            setTagInput('')
                                        }}
                                        className="w-full px-3 py-2 text-sm text-left text-[var(--text-main)] hover:bg-[var(--bg-hover)]"
                                    >
                                        {tag}
                                    </button>
                                ))}
                            </div>
                        )}
                    </div>

                    {/* 플랫폼 필터 */}
                    <div className="min-w-0 relative">
                        <input
                            type="text"
                            aria-label={translate("search.addPlatform")}
                            maxLength={200}
                            value={platformInput}
                            onChange={(e) => handlePlatformInput(e.target.value)}
                            onKeyDown={handlePlatformKeyDown}
                            placeholder={translate("search.platform.placeholder")}
                            className="w-full px-3 py-1.5 text-sm rounded-lg
                                bg-[var(--bg-hover)] text-[var(--text-main)]
                                placeholder-[var(--text-placeholder)]
                                border border-[var(--border-line)]
                                focus:outline-none focus:border-[var(--color-primary)]"
                        />
                        {vm.platformSuggestions.length > 0 && (
                            <div className="absolute z-20 top-full mt-1 w-full rounded-lg border border-[var(--border-line)] bg-(--bg-popup) shadow-lg overflow-hidden">
                                {vm.platformSuggestions.map((p) => (
                                    <button
                                        key={p}
                                        onClick={() => {
                                            vm.addPlatform(p)
                                            setPlatformInput('')
                                        }}
                                        className="w-full px-3 py-2 text-sm text-left text-[var(--text-main)] hover:bg-[var(--bg-hover)]"
                                    >
                                        {p}
                                    </button>
                                ))}
                            </div>
                        )}
                    </div>

                    {/* 작성자 필터 */}
                    <div className="min-w-0 relative">
                        <input
                            type="text"
                            aria-label={translate("search.addAuthor")}
                            maxLength={200}
                            value={authorInput}
                            onChange={(e) => handleAuthorInput(e.target.value)}
                            onKeyDown={handleAuthorKeyDown}
                            placeholder={translate("search.author.placeholder")}
                            className="w-full px-3 py-1.5 text-sm rounded-lg
                                bg-[var(--bg-hover)] text-[var(--text-main)]
                                placeholder-[var(--text-placeholder)]
                                border border-[var(--border-line)]
                                focus:outline-none focus:border-[var(--color-primary)]"
                        />
                        {vm.authorSuggestions.length > 0 && (
                            <div className="absolute z-20 top-full mt-1 w-full rounded-lg border border-[var(--border-line)] bg-(--bg-popup) shadow-lg overflow-hidden">
                                {vm.authorSuggestions.map((a) => (
                                    <button
                                        key={a}
                                        onClick={() => {
                                            vm.addAuthor(a)
                                            setAuthorInput('')
                                        }}
                                        className="w-full px-3 py-2 text-sm text-left text-[var(--text-main)] hover:bg-[var(--bg-hover)]"
                                    >
                                        {a}
                                    </button>
                                ))}
                            </div>
                        )}
                    </div>
                </div>

                {/* 선택된 필터 칩 */}
                {(vm.selectedTags.length > 0 ||
                    vm.selectedPlatforms.length > 0 ||
                    vm.selectedAuthors.length > 0) && (
                    <div className="flex flex-wrap gap-2 items-center">
                        {vm.selectedTags.map((tag) => (
                            <span
                                key={`tag-${tag}`}
                                className="flex items-center gap-1 px-2 py-1 text-xs rounded-full bg-[var(--color-primary)] text-white"
                            >
                                {tag}
                                <button
                                    aria-label={translate("search.removeTag", { tag: tag })}
                                    onClick={() => vm.removeTag(tag)}
                                    className="hover:opacity-70"
                                >
                                    x
                                </button>
                            </span>
                        ))}
                        {vm.selectedPlatforms.map((p) => (
                            <span
                                key={`platform-${p}`}
                                className="flex items-center gap-1 px-2 py-1 text-xs rounded-full bg-[var(--bg-active)] text-[var(--text-main)]"
                            >
                                {p}
                                <button
                                    aria-label={translate("search.removePlatform", { platform: p })}
                                    onClick={() => vm.removePlatform(p)}
                                    className="hover:opacity-70"
                                >
                                    x
                                </button>
                            </span>
                        ))}
                        {vm.selectedAuthors.map((a) => (
                            <span
                                key={`author-${a}`}
                                className="flex items-center gap-1 px-2 py-1 text-xs rounded-full bg-[var(--bg-active)] text-[var(--text-main)] border border-[var(--border-line)]"
                            >
                                {a}
                                <button
                                    aria-label={translate("search.removeAuthor", { author: a })}
                                    onClick={() => vm.removeAuthor(a)}
                                    className="hover:opacity-70"
                                >
                                    x
                                </button>
                            </span>
                        ))}
                        <button
                            onClick={() => { vm.clearAll(); setTagInput(''); setPlatformInput(''); setAuthorInput('') }}
                            className="text-xs text-[var(--text-muted)] hover:text-[var(--text-main)]"
                        >{translate("search.reset")}</button>
                    </div>
                )}
            </div>

            {/* 결과 헤더 */}
            {vm.suggestionError && (
                <p role="status" className="px-4 py-2 text-sm text-[var(--text-muted)]">
                    {translateMessage(vm.suggestionError)}
                </p>
            )}

            <div className="flex-1 min-h-0 overflow-hidden">
                <Gallery
                    title={translate("search.searchResults")}
                    filter={vm.filter}
                    emptyMessage={translate("shortcuts.noSearchResults")}
                />
            </div>
        </div>
    )
}
