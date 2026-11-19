export function toMediaUrl(filepath: string | null | undefined): string | null {
    if (!filepath) return null
    return '/media?path=' + encodeURIComponent(filepath)
}

export function formatDuration(seconds: number | null | undefined): string | null {
    if (seconds == null || !Number.isFinite(seconds) || seconds <= 0) return null
    const total = Math.floor(seconds)
    return `${Math.floor(total / 60)}:${String(total % 60).padStart(2, '0')}`
}
