export function formatBytes(value: number): string {
    if (!Number.isFinite(value) || value < 0) return '0 B'
    if (value < 1024) return `${Math.round(value)} B`
    const units = ['KB', 'MB', 'GB', 'TB']
    let size = value / 1024
    let unit = 0
    while (size >= 1024 && unit < units.length - 1) { size /= 1024; unit++ }
    return `${size.toFixed(1)} ${units[unit]}`
}
