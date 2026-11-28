import { MediaFormatFields, useOutputFormats } from '../../mediaedit/MediaFormatFields'
import { translate, translateMessage, useLanguage } from '@renderer/shared/i18n'
import {
    useEffect,
    useRef,
    useState,
    type ClipboardEvent,
    type FormEvent
} from 'react'
import { Download, Link, LoaderCircle } from 'lucide-react'
import { useDownloadViewModel } from '../useDownloadViewModel'
import { Button } from '@renderer/shared/components/ui/Button'
import { isHttpUrl } from '@renderer/shared/utils/url'
import type { DownloadJobOptions } from '@contracts/download-api'

interface EnginePreview {
    url: string
    optionsKey: string
    text: string
    error: boolean
}

export function DownloadBar(): React.JSX.Element {
    useLanguage()
    const [optionsOpen, setOptionsOpen] = useState(false)
    const { formats, error: formatError } = useOutputFormats(optionsOpen)
    const [url, setUrl] = useState('')
    const [isSubmitting, setIsSubmitting] = useState(false)
    const [preview, setPreview] = useState<EnginePreview | null>(null)
    const pending = useRef(false)
    const optionsRef = useRef<HTMLDetailsElement>(null)
    const [options, setOptions] = useState<DownloadJobOptions>({ type: 'video', quality: 'best', playlist: false })

    const { startDownload, previewEngine, ready, halted } =
        useDownloadViewModel()

    const currentURL = url.trim()
    const optionsKey = JSON.stringify(options)
    const currentPreview = preview?.url === currentURL && preview.optionsKey === optionsKey ? preview : null

    useEffect(() => {
        const closeOptions = (): void => {
            if (optionsRef.current) optionsRef.current.open = false
        }
        const handleOutsidePointer = (event: PointerEvent): void => {
            if (event.target instanceof Node && !optionsRef.current?.contains(event.target)) closeOptions()
        }
        const handleKeyDown = (event: KeyboardEvent): void => {
            if (event.key === 'Escape' && optionsRef.current?.open) {
                closeOptions()
                optionsRef.current.querySelector('summary')?.focus()
            }
        }
        document.addEventListener('pointerdown', handleOutsidePointer, true)
        document.addEventListener('keydown', handleKeyDown)
        window.addEventListener('blur', closeOptions)
        return () => {
            document.removeEventListener('pointerdown', handleOutsidePointer, true)
            document.removeEventListener('keydown', handleKeyDown)
            window.removeEventListener('blur', closeOptions)
        }
    }, [])

    useEffect(() => {
        if (!currentURL) return

        let disposed = false

        const timer = setTimeout(() => {
            void previewEngine(currentURL, options)
                .then((kind) => {
                    if (disposed) return

                    setPreview({
                        url: currentURL,
                        optionsKey,
                        text: translate("download.engine", { engine: kind }),
                        error: false
                    })
                })
                .catch((error: unknown) => {
                    if (disposed) return

                    setPreview({
                        url: currentURL,
                        optionsKey,
                        text:
                            error instanceof Error
                                ? error.message
                                : translate("download.couldNotCheckTheEngine"),
                        error: true
                    })
                })
        }, 250)

        return () => {
            disposed = true
            clearTimeout(timer)
        }
    }, [currentURL, previewEngine, options, optionsKey])

    const submit = async (value: string): Promise<void> => {
        const trimmed = value.trim()

        if (!ready || halted || !trimmed || pending.current) return

        pending.current = true
        setIsSubmitting(true)

        try {
            if (await startDownload(trimmed, options)) setUrl('')
        } finally {
            pending.current = false
            setIsSubmitting(false)
        }
    }

    const handleSubmit = (event: FormEvent<HTMLFormElement>): void => {
        event.preventDefault()
        void submit(url)
    }

    const handlePaste = (event: ClipboardEvent<HTMLInputElement>): void => {
        const text = event.clipboardData.getData('text').trim()
        if (!isHttpUrl(text)) return

        event.preventDefault()
        setUrl(text)
    }

    const previewText = currentURL
        ? translateMessage(currentPreview?.text) ?? translate("download.checkingEngine")
        : ''

    return (
        <form
            data-keyboard-region tabIndex={-1}
            onSubmit={handleSubmit}
            className="relative mx-auto flex w-full max-w-4xl gap-3"
        >
            <details ref={optionsRef} onToggle={event => setOptionsOpen(event.currentTarget.open)} className="shrink-0">
                <summary className="cursor-pointer rounded-xl border border-[var(--border-line)] px-3 py-2.5 text-sm" aria-label={translate("download.downloadOptions")}>{translate("download.options")}</summary>
                <div className="absolute left-0 top-full z-30 mt-2 max-h-[calc(100dvh-6rem)] w-64 space-y-3 overflow-y-auto rounded-xl border border-[var(--border-line)] bg-[var(--bg-sidebar)] p-4 shadow-xl">
                    <p className="text-xs text-[var(--text-muted)]">{translate("download.instagramSavesPhotosAndVideosTogetherInTheirOriginal")}</p>
                    <label className="block text-sm">{translate("download.type")}<select aria-label={translate("download.downloadType")} value={options.type} onChange={(event) => {
                            const audio = event.target.value === 'audio'
                            setOptions({ ...options, type: audio ? 'audio' : 'video', quality: audio ? 'audio' : 'best', extension: audio ? 'mp3' : undefined, videoCodec: undefined, audioCodec: undefined })
                        }} className="mt-1 w-full rounded border border-[var(--border-line)] bg-[var(--bg-app)] p-2">
                            <option value="video">{translate("media.video")}</option><option value="audio">{translate("conversion.audio")}</option>
                        </select>
                    </label>
                    {options.type !== 'audio' && <>
                        <label className="block text-sm">{translate("download.quality")}<select aria-label={translate("download.downloadQuality")} value={options.quality} onChange={(event) => setOptions({ ...options, quality: event.target.value as DownloadJobOptions['quality'] })} className="mt-1 w-full rounded border border-[var(--border-line)] bg-[var(--bg-app)] p-2">
                                {['best', '8k', '4k', '2k', '1080', '720', '480'].map(value => <option key={value} value={value}>{value === 'best' ? translate("download.bestQuality") : value}</option>)}
                            </select>
                        </label>
                    </>}
                    <MediaFormatFields value={{ type: options.type === 'audio' ? 'audio' : 'video', extension: options.extension, videoCodec: options.videoCodec, audioCodec: options.audioCodec }}
                        formats={formats.length ? formats : [
                            { type: 'video', extension: 'mp4', videoCodecs: [], audioCodecs: [] },
                            { type: 'video', extension: 'mkv', videoCodecs: [], audioCodecs: [] },
                            { type: 'audio', extension: 'mp3', videoCodecs: [], audioCodecs: [] },
                        ]} auto containerLabel={translate('download.downloadFormat')}
                        onChange={format => setOptions({ ...options, extension: format.extension, videoCodec: format.videoCodec, audioCodec: format.audioCodec })} />
                    {formatError && <p role="status" className="text-xs text-[var(--text-muted)]">{translate('conversion.additionalFormatsNeedFFmpeg')}</p>}
                    <label className="flex gap-2 text-sm"><input type="checkbox" checked={Boolean(options.playlist)} onChange={(event) => setOptions({ ...options, playlist: event.target.checked })} />{translate("download.includePlaylist")}</label>
                </div>
            </details>
            <div className="group relative min-w-0 flex-1">
                <div className="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-3">
                    <Link className="size-5 text-[var(--text-muted)]" />
                </div>

                <input
                    data-download-url
                    type="text"
                    aria-label={translate("download.downloadUrl")}
                    value={url}
                    onChange={(event) => setUrl(event.target.value)}
                    onPaste={handlePaste}
                    disabled={isSubmitting || Boolean(halted)}
                    placeholder={translate("download.pasteADownloadLink")}
                    className="w-full min-w-0 rounded-xl border bg-[var(--bg-sidebar)] py-2.5 pl-10 pr-40 text-[var(--text-main)] shadow-sm outline-none transition-all border-[var(--border-line)] placeholder:text-[var(--text-placeholder)] focus:border-[var(--color-primary)] focus:ring-2 focus:ring-[var(--color-primary)]/20"
                />

                <span
                    role="status"
                    aria-live="polite"
                    title={previewText}
                    className={`pointer-events-none absolute inset-y-0 right-3 flex max-w-36 items-center text-xs ${
                        currentPreview?.error
                            ? 'text-red-500'
                            : 'text-[var(--text-muted)]'
                    }`}
                >
                    <span className="truncate">{previewText}</span>
                </span>
            </div>

            <Button
                type="submit"
                size="md"
                disabled={
                    isSubmitting ||
                    !currentURL ||
                    !ready ||
                    Boolean(halted)
                }
                className="flex items-center gap-2 rounded-xl shadow-lg active:scale-95"
            >
                {isSubmitting ? (
                    <LoaderCircle className="size-4 animate-spin" />
                ) : (
                    <Download className="size-4" />
                )}

                <span>{isSubmitting ? translate("download.processing") : translate("navigation.downloads")}</span>
            </Button>
        </form>
    )
}
