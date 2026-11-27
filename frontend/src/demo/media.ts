import { sampleNames } from './seed'

const names = new Set<string>([...sampleNames, 'orbit-video'])
export function toMediaUrl(filepath: string | null | undefined): string | null {
    if (!filepath?.startsWith('sample:')) return null
    const name = filepath.slice(7)
    if (!names.has(name)) return null
    const asset = name === 'orbit-video' ? 'orbit.mp4' : `${name}.svg`
    return `${import.meta.env.BASE_URL}samples/${asset}`
}
export function formatDuration(seconds: number | null | undefined): string | null {
    if (seconds == null || !Number.isFinite(seconds) || seconds <= 0) return null
    const total = Math.floor(seconds)
    return `${Math.floor(total / 60)}:${String(total % 60).padStart(2, '0')}`
}
