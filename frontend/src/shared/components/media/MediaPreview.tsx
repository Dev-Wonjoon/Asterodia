import { translate, useLanguage } from '@renderer/shared/i18n'
import { useEffect, useState, type ReactNode } from 'react'
import { toMediaUrl } from '@renderer/shared/utils/media'

interface MediaPreviewProps {
    path: string | null
    fallbackPath?: string | null
    resolveUrl?: typeof toMediaUrl
    title: string
    draggable?: boolean
    className?: string
    fallback?: ReactNode
    identity?: string
    retryKey?: number
}

export function MediaPreview({ identity, retryKey = 0, ...props }: MediaPreviewProps) {
    return <PreviewImage key={JSON.stringify([identity, retryKey])} {...props} />
}

function PreviewImage({
    path,
    fallbackPath = null,
    resolveUrl = toMediaUrl,
    title,
    draggable,
    className = '',
    fallback
}: MediaPreviewProps) {
    useLanguage()
    const candidates = [resolveUrl(path), resolveUrl(fallbackPath)]
    const [failedUrls, setFailedUrls] = useState<string[]>([])
    const [loadedUrl, setLoadedUrl] = useState<string | null>(null)
    const url = candidates.find(candidate => candidate && !failedUrls.includes(candidate))
    const hasCandidates = candidates.some(Boolean)

    useEffect(() => {
        if (!hasCandidates) {
            setLoadedUrl(null)
            return
        }
        if (!loadedUrl || !url || loadedUrl === url) return

        // Keep the already displayed image until its replacement is decoded.
        // Initial images still use native lazy loading.
        let active = true
        const image = new Image()
        image.onload = async () => {
            try {
                await image.decode()
                if (active) setLoadedUrl(url)
            } catch {
                if (active) setFailedUrls(previous => [...previous, url])
            }
        }
        image.onerror = () => {
            if (active) setFailedUrls(previous => [...previous, url])
        }
        image.src = url
        return () => {
            active = false
            image.onload = null
            image.onerror = null
        }
    }, [hasCandidates, loadedUrl, url])

    const displayedUrl = hasCandidates ? loadedUrl ?? url : null

    if (!displayedUrl) {
        if (fallback !== undefined) return <>{fallback}</>
        return (
            <div
                className={
                    className + ' flex items-center justify-center text-sm text-[var(--text-muted)]'
                }
            >{translate("common.noPreview")}</div>
        )
    }

    return (
        <img
            draggable={draggable}
            src={displayedUrl}
            alt={title}
            loading="lazy"
            className={className}
            onLoad={() => setLoadedUrl(displayedUrl)}
            onError={() => {
                setFailedUrls(previous => [...previous, displayedUrl])
                setLoadedUrl(previous => previous === displayedUrl ? null : previous)
            }}
        />
    )
}
