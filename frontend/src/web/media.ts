// Browser DTOs contain server URLs, never PC filesystem paths.
export function toMediaUrl(value: string | null | undefined): string | null {
    return value && /^\/api\/files\/[1-9]\d*\/(content|preview|thumbnail)$/.test(value) ? value : null
}
