import { translate, useLanguage } from '@renderer/shared/i18n'
import { Download } from 'lucide-react'
import { useEffect, useState } from 'react'
import { getApi } from '@renderer/shared/api'
import { toMediaUrl } from '@renderer/shared/utils/media'

export function DownloadThumbnail({
    url,
    localPath,
    completed = false,
    title = translate("download.downloadThumbnail"),
    fullWidth = false
}: {
    url: string
    localPath?: string | null
    completed?: boolean
    title?: string
    fullWidth?: boolean
}): React.JSX.Element {
    useLanguage()
    const [result, setResult] = useState<{
        url: string
        hint: string | null | undefined
        path: string | null
        loading: boolean
    }>({
        url,
        hint: localPath,
        path: localPath ?? null,
        loading: !localPath
    })

    const [failedSrc, setFailedSrc] = useState<string | null>(null)
    const [reload, setReload] = useState(0)

    useEffect(() => {
        if (!completed) return
        return getApi().onMediaThumbnailUpdated(() => setReload((value) => value + 1))
    }, [completed])

    useEffect(() => {
        let disposed = false

        setFailedSrc(null)
        if (localPath && reload === 0) {
            setResult({ url, hint: localPath, path: localPath, loading: false })
            return
        }
        setResult((previous) => ({
            url, hint: localPath,
            path: localPath ?? (previous.url === url ? previous.path : null), loading: true
        }))

        void (async () => {
            try {
                const path = await getApi().getDownloadPreview(url)

                if (!disposed) {
                    setResult({ url, hint: localPath, path, loading: false })
                }
            } catch {
                if (!disposed) {
                    setResult((previous) => {
                        const path = previous.url === url ? previous.path : (localPath ?? null)
                        return { url, hint: localPath, path: toMediaUrl(path) === failedSrc ? null : path, loading: false }
                    })
                }
            }
        })()

        return () => {
            disposed = true
        }
    }, [url, reload, completed, localPath])

    const current = result.url === url && result.hint === localPath ? result : null
    const src = toMediaUrl(current ? current.path : localPath)
    const loading = current ? current.loading : !localPath

    return (
        <div className={'flex aspect-video shrink-0 items-center justify-center overflow-hidden rounded-lg bg-[var(--bg-hover)] text-[var(--text-muted)] ' + (fullWidth ? 'w-full' : 'w-28 sm:w-36')}>
            {src && src !== failedSrc ? (
                <img
                    key={src}
                    src={src}
                    alt={title}
                    loading="lazy"
                    className="h-full w-full object-contain"
                    onError={() => setFailedSrc(src)}
                />
            ) : (
                <div className="flex flex-col items-center gap-0.5">
                    <Download className="size-6" aria-hidden="true" />

                    <span className="text-xs">{loading ? translate("download.preparingPreview") : translate("download.noThumbnail")}</span>

                    {!loading && (
                        <button
                            type="button"
                            data-detail-trigger
                            aria-label={translate("download.reloadThumbnail")}
                            className="text-[11px] leading-4 underline"
                            onClick={(event) => {
                                event.stopPropagation()
                                setReload((value) => value + 1)
                            }}
                        >{translate("common.retry")}</button>
                    )}
                </div>
            )}
        </div>
    )
}
