import { useEffect, useState } from 'react'
import type { ConversionFormat, OutputFormat } from '@contracts/conversion'
import { getApi } from '@renderer/shared/api'
import { translate, translateError, useLanguage } from '@renderer/shared/i18n'
import type { WebServerResult } from '@contracts/webserver-api'

export const formatFieldClass = 'mt-1 w-full rounded-lg border border-[var(--border-line)] bg-[var(--bg-app)] p-2'
const codecNames: Record<string, string> = { h264: 'H.264', hevc: 'H.265 / HEVC', vp9: 'VP9', av1: 'AV1', aac: 'AAC', mp3: 'MP3', opus: 'Opus', vorbis: 'Vorbis', flac: 'FLAC', pcm: 'PCM' }

export function MediaFormatFields({ value, onChange, formats, auto = false, containerLabel }: {
    value: ConversionFormat; onChange(value: ConversionFormat): void; formats: OutputFormat[]; auto?: boolean; containerLabel?: string
}) {
    useLanguage()
    const available = formats.filter(item => item.type === value.type)
    const extension = value.extension || (value.type === 'audio' ? 'mp3' : 'mp4')
    const selected = available.find(item => item.extension === extension)
    const label = (codec: string) => codec === 'copy' ? translate('conversion.copy') : codecNames[codec] ?? codec
    function changeContainer(next: string) {
        const format = available.find(item => item.extension === (next || 'mp4'))
        const updated = { ...value, extension: next || undefined }
        if (updated.videoCodec && !format?.videoCodecs.includes(updated.videoCodec)) updated.videoCodec = undefined
        if (updated.audioCodec && !format?.audioCodecs.includes(updated.audioCodec)) updated.audioCodec = undefined
        if (value.type === 'video' && format?.videoCodecs[0] !== 'copy') updated.videoCodec ??= format?.videoCodecs[0]
        if (value.type === 'video' && format?.audioCodecs[0] !== 'copy') updated.audioCodec ??= format?.audioCodecs[0]
        onChange(updated)
    }
    return <>
        <label className="block text-sm">{translate('conversion.container')}
            <select aria-label={containerLabel ?? translate('conversion.container')} value={auto ? value.extension ?? '' : extension} onChange={event => changeContainer(event.target.value)} className={formatFieldClass}>
                {auto && value.type === 'video' && <option value="">{translate('download.auto')}</option>}
                {available.map(item => <option key={item.extension} value={item.extension}>{item.extension.toUpperCase()}</option>)}
            </select>
        </label>
        {value.type === 'video' && <label className="block text-sm">{translate('conversion.videoCodec')}
            <select aria-label={translate('conversion.videoCodec')} value={value.videoCodec ?? ''} onChange={event => onChange({ ...value, videoCodec: event.target.value || undefined })} className={formatFieldClass}>
                <option value="">{translate('conversion.containerDefault')}</option>
                {selected?.videoCodecs.map(codec => <option key={codec} value={codec}>{label(codec)}</option>)}
            </select>
        </label>}
        <label className="block text-sm">{translate('conversion.audioCodec')}
            <select aria-label={translate('conversion.audioCodec')} value={value.audioCodec ?? ''} onChange={event => onChange({ ...value, audioCodec: event.target.value || undefined })} className={formatFieldClass}>
                <option value="">{translate('conversion.containerDefault')}</option>
                {selected?.audioCodecs.map(codec => <option key={codec} value={codec}>{label(codec)}</option>)}
            </select>
        </label>
    </>
}

const loadEditFormats = () => getApi().mediaEdit.formats()
export function useOutputFormats(enabled = true, load: () => Promise<WebServerResult<OutputFormat[]>> = loadEditFormats) {
    const [formats, setFormats] = useState<OutputFormat[]>([])
    const [error, setError] = useState('')
    useEffect(() => {
        if (!enabled) return
        let alive = true
        void (async () => {
            try {
                const result = await load()
                if (!alive) return
                if (result.ok) { setFormats(result.data); setError('') }
                else setError(translateError(result.error))
            } catch { if (alive) setError(translate('conversion.formatsUnavailable')) }
        })()
        return () => { alive = false }
    }, [enabled, load])
    return { formats, error }
}
