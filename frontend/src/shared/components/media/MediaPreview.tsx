import { useState } from 'react'
import { toMediaUrl } from '@renderer/shared/utils/media'

export function MediaPreview({
    path,
    fallbackPath = null,
    title,
    className = ''
}: {
    path: string | null
    fallbackPath?: string | null
    title: string
    className?: string
}) {
    const candidates = [toMediaUrl(path), toMediaUrl(fallbackPath)]
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
            >
                미리보기 없음
            </div>
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
