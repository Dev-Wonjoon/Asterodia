import { translateError, translateMessage, translate, useLanguage } from '@renderer/shared/i18n'
import { useEffect, useRef, useState } from 'react'
import { getApi } from '@renderer/shared/api'
import type { ThumbnailStatusDto } from '@contracts/thumbnail-api'

export function MediaThumbnailStatus({
    mediaId,
    fileId,
    title,
    disabled = false
}: {
    mediaId: number
    fileId: number
    title: string
    disabled?: boolean
}): React.JSX.Element | null {
    useLanguage()
    const [status, setStatus] = useState<ThumbnailStatusDto | null>(null)
    const [error, setError] = useState<string | null>(null)
    const [requesting, setRequesting] = useState(false)
    const [command, setCommand] = useState<{ mode: 'read' | 'retry' }>({
        mode: 'read'
    })

    // React의 다음 렌더링 전에도 연속 클릭을 막는다.
    const requestLock = useRef(false)

    useEffect(() => {
        let active = true
        let timer: ReturnType<typeof setTimeout> | undefined

        const request = { mediaId, fileId }

        async function readStatus(): Promise<void> {
            if (!active) return

            try {
                const result = await getApi().thumbnails.get(request)

                if (!active) return

                if (!result.ok) {
                    setError(translateError(result.error))
                    return
                }

                setStatus(result.data)

                if (result.data.state === 'pending' || result.data.state === 'running') {
                    timer = setTimeout(() => {
                        void readStatus()
                    }, 2000)
                }
            } catch {
                if (active) {
                    setError(translate("gallery.couldNotCheckThumbnailStatus"))
                }
            }
        }

        async function start(): Promise<void> {
            setError(null)

            try {
                if (command.mode === 'retry') {
                    const result = await getApi().thumbnails.retry(request)

                    if (!active) return

                    if (result.ok) {
                        setStatus(result.data)
                    } else {
                        setError(translateError(result.error))
                    }
                }

                await readStatus()
            } catch {
                if (active) {
                    setError(translate("gallery.couldNotRequestAThumbnailRetry"))
                }
            } finally {
                if (active) {
                    requestLock.current = false
                    setRequesting(false)
                }
            }
        }

        void start()

        return () => {
            active = false

            if (timer !== undefined) {
                clearTimeout(timer)
            }
        }
    }, [mediaId, fileId, command])

    function request(mode: 'read' | 'retry'): void {
        if (disabled || requestLock.current) return
        if (mode === 'retry' && !status?.canRetry) return

        requestLock.current = true
        setRequesting(true)
        setCommand({ mode })
    }

    if (!error && (status?.state === 'unsupported' || status?.state === 'registered')) {
        return null
    }

    let message = translate("gallery.checkingThumbnailStatus")

    switch (status?.state) {
        case 'missing':
            message = translate("gallery.noThumbnailGenerationJobRegistered")
            break

        case 'pending':
            message = translate("gallery.waitingToGenerateThumbnail")
            break

        case 'running':
            message = translate("gallery.generatingThumbnail")
            break

        case 'failed':
            message = status.canRetry
                ? translate("gallery.couldNotLoadTheThumbnail")
                : translate("gallery.couldNotFinishGeneratingTheThumbnail")
            break

        case 'blocked':
            message = translate("gallery.thumbnailGenerationIsOnHold")
            break

        case 'registered':
        case 'unsupported':
            message = ''
            break
    }

    const showRefresh =
        Boolean(error) ||
        status?.state === 'missing' ||
        status?.state === 'blocked' ||
        (status?.state === 'failed' && !status.canRetry)

    const button =
        'rounded-lg border border-[var(--border-line)] px-3 py-2 text-sm disabled:opacity-40'

    return (
        <div
            data-thumbnail-file={fileId}
            data-thumbnail-state={status?.state ?? 'loading'}
            aria-label={translate("gallery.thumbnailStatus", { title: title })}
            aria-busy={requesting}
            className="space-y-2 text-sm"
        >
            {message && (
                <p role="status" className="text-[var(--text-muted)]">
                    {message}
                </p>
            )}

            {error && <p role="alert">{translateMessage(error)}</p>}

            {status?.canRetry && (
                <button
                    type="button"
                    className={button}
                    aria-label={translate("gallery.retryThumbnail", { title: title })}
                    disabled={disabled || requesting}
                    onClick={(event) => {
                        event.stopPropagation()
                        request('retry')
                    }}
                >
                    {requesting ? translate("gallery.operation.requesting") : translate("gallery.thumbnail.retry")}
                </button>
            )}

            {showRefresh && (
                <button
                    type="button"
                    className={button}
                    aria-label={translate("gallery.refreshThumbnailStatus", { title: title })}
                    disabled={disabled || requesting}
                    onClick={(event) => {
                        event.stopPropagation()
                        request('read')
                    }}
                >{translate("gallery.refreshStatus")}</button>
            )}
        </div>
    )
}
