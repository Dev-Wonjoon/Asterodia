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
    text: string
    error: boolean
}

export function DownloadBar(): React.JSX.Element {
    const [url, setUrl] = useState('')
    const [isSubmitting, setIsSubmitting] = useState(false)
    const [preview, setPreview] = useState<EnginePreview | null>(null)
    const pending = useRef(false)
    const [options, setOptions] = useState<DownloadJobOptions>({ type: 'video', quality: 'best', playlist: false })

    const { startDownload, previewEngine, ready, halted } =
        useDownloadViewModel()

    const currentURL = url.trim()
    const currentPreview = preview?.url === currentURL ? preview : null

    useEffect(() => {
        if (!currentURL) return

        let disposed = false

        const timer = setTimeout(() => {
            void previewEngine(currentURL)
                .then((kind) => {
                    if (disposed) return

                    setPreview({
                        url: currentURL,
                        text: `엔진: ${kind}`,
                        error: false
                    })
                })
                .catch((error: unknown) => {
                    if (disposed) return

                    setPreview({
                        url: currentURL,
                        text:
                            error instanceof Error
                                ? error.message
                                : '엔진을 확인하지 못했습니다.',
                        error: true
                    })
                })
        }, 250)

        return () => {
            disposed = true
            clearTimeout(timer)
        }
    }, [currentURL, previewEngine])

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

        if (ready && !halted) void submit(text)
    }

    const previewText = currentURL
        ? currentPreview?.text ?? '엔진 확인 중…'
        : ''

    return (
        <form
            onSubmit={handleSubmit}
            className="relative mx-auto flex w-full max-w-4xl gap-3"
        >
            <details className="shrink-0">
                <summary className="cursor-pointer rounded-xl border border-[var(--border-line)] px-3 py-2.5 text-sm" aria-label="다운로드 옵션">옵션</summary>
                <div className="absolute left-0 top-full z-30 mt-2 w-64 space-y-3 rounded-xl border border-[var(--border-line)] bg-[var(--bg-sidebar)] p-4 shadow-xl">
                    <label className="block text-sm">종류
                        <select aria-label="다운로드 종류" value={options.type} onChange={(event) => {
                            const audio = event.target.value === 'audio'
                            setOptions({ ...options, type: audio ? 'audio' : 'video', quality: audio ? 'audio' : 'best', extension: audio ? 'mp3' : undefined })
                        }} className="mt-1 w-full rounded border border-[var(--border-line)] bg-[var(--bg-app)] p-2">
                            <option value="video">영상</option><option value="audio">오디오 (MP3)</option>
                        </select>
                    </label>
                    {options.type !== 'audio' && <>
                        <label className="block text-sm">화질
                            <select aria-label="다운로드 화질" value={options.quality} onChange={(event) => setOptions({ ...options, quality: event.target.value as DownloadJobOptions['quality'] })} className="mt-1 w-full rounded border border-[var(--border-line)] bg-[var(--bg-app)] p-2">
                                {['best', '8k', '4k', '2k', '1080', '720', '480'].map(value => <option key={value} value={value}>{value === 'best' ? '최고 화질' : value}</option>)}
                            </select>
                        </label>
                        <label className="block text-sm">형식
                            <select aria-label="다운로드 형식" value={options.extension ?? ''} onChange={(event) => setOptions({ ...options, extension: (event.target.value || undefined) as 'mp4' | 'mkv' | undefined })} className="mt-1 w-full rounded border border-[var(--border-line)] bg-[var(--bg-app)] p-2">
                                <option value="">자동</option><option value="mp4">MP4</option><option value="mkv">MKV</option>
                            </select>
                        </label>
                    </>}
                    <label className="flex gap-2 text-sm"><input type="checkbox" checked={Boolean(options.playlist)} onChange={(event) => setOptions({ ...options, playlist: event.target.checked })} />재생목록 포함</label>
                </div>
            </details>
            <div className="group relative min-w-0 flex-1">
                <div className="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-3">
                    <Link className="size-5 text-[var(--text-muted)]" />
                </div>

                <input
                    type="text"
                    aria-label="다운로드할 URL"
                    value={url}
                    onChange={(event) => setUrl(event.target.value)}
                    onPaste={handlePaste}
                    disabled={isSubmitting || Boolean(halted)}
                    placeholder="다운로드할 링크를 붙여넣으세요..."
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

                <span>{isSubmitting ? '처리 중' : '다운로드'}</span>
            </Button>
        </form>
    )
}
