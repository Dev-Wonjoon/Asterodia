import { translate, useLanguage } from '@renderer/shared/i18n'
import { useState } from 'react'
import { toMediaUrl } from '@renderer/shared/utils/media'

export function MediaPreview({
    path,
    fallbackPath = null,
    resolveUrl = toMediaUrl,
    title,
    className = ''
}: {
    path: string | null
    fallbackPath?: string | null
    resolveUrl?: typeof toMediaUrl
    title: string
    className?: string
}) {
    useLanguage()
    const candidates = [resolveUrl(path), resolveUrl(fallbackPath)]
    const sourceKey = JSON.stringify(candidates)
    const [failure, setFailure] = useState<{ sourceKey: string; urls: string[] } | null>(null)
    const failedUrls = failure?.sourceKey === sourceKey ? failure.urls : []
    const url = candidates.find(candidate => candidate && !failedUrls.includes(candidate))

    if (!url) {
        return (
            <div
                className={
                    className + ' flex items-center justify-center text-sm text-[var(--text-muted)]'
                }
            >{translate("미리보기 없음")}</div>
        )
    }

    return (
        <img
            src={url}
            alt={title}
            loading="lazy"
            className={className}
            onError={() => setFailure(previous => ({
                sourceKey,
                urls: [...(previous?.sourceKey === sourceKey ? previous.urls : []), url]
            }))}
        />
    )
}
