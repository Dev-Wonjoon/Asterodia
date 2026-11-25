import { translate, useLanguage } from '@renderer/shared/i18n'
import { Download } from 'lucide-react'
import { useEffect, useState } from 'react'
import { getApi } from '@renderer/shared/api'
import { MediaPreview } from '@renderer/shared/components/media/MediaPreview'

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
        imageRetry: number
    }>({
        url,
        hint: localPath,
        path: localPath ?? null,
        loading: !localPath,
        imageRetry: 0
    })

    const [reload, setReload] = useState(0)
    const [retry, setRetry] = useState(0)

    useEffect(() => {
        if (!completed) return
        return getApi().onMediaThumbnailUpdated(() => setReload((value) => value + 1))
    }, [completed])

    useEffect(() => {
        let disposed = false

        if (localPath && reload === 0 && retry === 0) {
            setResult({ url, hint: localPath, path: localPath, loading: false, imageRetry: 0 })
            return
        }
        setResult((previous) => ({
            url, hint: localPath,
            path: previous.url !== url ? localPath ?? null
                : localPath && localPath !== previous.hint ? localPath : previous.path ?? localPath ?? null,
            loading: true,
            imageRetry: previous.url === url ? previous.imageRetry : 0
        }))

        void (async () => {
            try {
                const path = await getApi().getDownloadPreview(url)

                if (!disposed) {
                    setResult(previous => ({
                        url, hint: localPath,
                        path: path ?? (previous.url === url ? previous.path : localPath ?? null),
                        loading: false,
                        imageRetry: path ? retry : previous.url === url ? previous.imageRetry : 0
                    }))
                }
            } catch {
                if (!disposed) {
                    setResult((previous) => {
                        const path = previous.url === url ? previous.path : (localPath ?? null)
                        return { url, hint: localPath, path, loading: false, imageRetry: previous.url === url ? previous.imageRetry : 0 }
                    })
                }
            }
        })()

        return () => {
            disposed = true
        }
    }, [url, reload, completed, localPath, retry])

    const current = result.url === url ? result : null
    const path = current
        ? localPath && localPath !== current.hint ? localPath : current.path
        : localPath ?? null
    const loading = current ? current.loading : !localPath

    return (
        <div className={'flex aspect-video shrink-0 items-center justify-center overflow-hidden rounded-lg bg-[var(--bg-hover)] text-[var(--text-muted)] ' + (fullWidth ? 'w-full' : 'w-28 sm:w-36')}>
            <MediaPreview
                identity={url}
                retryKey={current?.imageRetry ?? 0}
                path={path}
                title={title}
                className="h-full w-full object-contain"
                fallback={<div className="flex flex-col items-center gap-0.5">
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
                                setRetry((value) => value + 1)
                            }}
                        >{translate("common.retry")}</button>
                    )}
                </div>}
            />
        </div>
    )
}
