import { translateError } from '@renderer/shared/i18n'
import { useEffect, useMemo, useState } from 'react'
import type { MediaFilter, MediaSuggestionRequest } from '@contracts/media-v2'
import { getApi } from '@renderer/shared/api'
import { useSuggestions } from '@renderer/shared/hooks/useSuggestions'
import { useContentVisibility } from '@renderer/shared/hooks/useContentVisibility'

async function suggest(kind: MediaSuggestionRequest['kind'], text: string): Promise<string[]> {
    const result = await getApi().media.suggest({
        kind,
        text
    })

    if (!result.ok) {
        throw new Error(translateError(result.error))
    }

    return result.data
}

const fetchTags = (text: string) => suggest('tag', text)

const fetchPlatforms = (text: string) => suggest('platform', text)

const fetchAuthors = (text: string) => suggest('author', text)

export function useSearchViewModel(initial: MediaFilter = {}) {
    const visibility = useContentVisibility()
    const [keyword, setKeyword] = useState(initial.keyword ?? '')
    const [composing, setSearchComposing] = useState(false)

    const [debouncedKeyword, setDebouncedKeyword] = useState(initial.keyword ?? '')

    const [selectedTags, setSelectedTags] = useState<string[]>(initial.tags ?? [])

    const [tagMode, setTagMode] = useState<'and' | 'or'>(initial.tagMode ?? 'and')

    const [selectedPlatforms, setSelectedPlatforms] = useState<string[]>(initial.platforms ?? [])

    const [selectedAuthors, setSelectedAuthors] = useState<string[]>(initial.authors ?? [])

    const tags = useSuggestions(fetchTags, selectedTags)

    const platforms = useSuggestions(fetchPlatforms, selectedPlatforms)

    const authors = useSuggestions(fetchAuthors, selectedAuthors)

    useEffect(() => {
        if (composing) return
        const timer = setTimeout(() => setDebouncedKeyword(keyword), 300)

        return () => clearTimeout(timer)
    }, [keyword, composing])

    const filter = useMemo<MediaFilter>(
        () => ({
            keyword: debouncedKeyword,
            ...(visibility.showHidden ? { showHidden: true } : {}),
            tags: selectedTags,
            tagMode,
            platforms: selectedPlatforms,
            authors: selectedAuthors
        }),
        [debouncedKeyword, selectedTags, tagMode, selectedPlatforms, selectedAuthors, visibility.showHidden]
    )

    const add = (
        value: string,
        update: React.Dispatch<React.SetStateAction<string[]>>,
        clear: () => void
    ) => {
        if (!value.trim()) return

        update((previous) => (previous.includes(value) ? previous : [...previous, value]))

        clear()
    }

    return {
        keyword,
        setKeyword,
        setSearchComposing,
        submitKeyword(value: string) { setKeyword(value); setDebouncedKeyword(value) },
        selectedTags,
        tagMode,
        setTagMode,
        selectedPlatforms,
        selectedAuthors,
        filter,

        suggestionError: tags.error ?? platforms.error ?? authors.error,

        clearAll() {
            setKeyword('')
            setDebouncedKeyword('')
            setSelectedTags([])
            setTagMode('and')
            setSelectedPlatforms([])
            setSelectedAuthors([])

            tags.clear()
            platforms.clear()
            authors.clear()
        },

        addTag: (value: string) => add(value, setSelectedTags, tags.clear),

        removeTag: (value: string) =>
            setSelectedTags((previous) => previous.filter((item) => item !== value)),

        addPlatform: (value: string) => add(value, setSelectedPlatforms, platforms.clear),

        removePlatform: (value: string) =>
            setSelectedPlatforms((previous) => previous.filter((item) => item !== value)),

        addAuthor: (value: string) => add(value, setSelectedAuthors, authors.clear),

        removeAuthor: (value: string) =>
            setSelectedAuthors((previous) => previous.filter((item) => item !== value)),

        tagSuggestions: tags.suggestions,
        suggestTags: tags.suggest,

        platformSuggestions: platforms.suggestions,
        suggestPlatforms: platforms.suggest,

        authorSuggestions: authors.suggestions,
        suggestAuthors: authors.suggest
    }
}
