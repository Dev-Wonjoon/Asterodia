export function canEditMedia(file?: { kind?: string; detectedFormat?: string | null; filepath?: string } | null): boolean {
    return !!file && (file.kind === 'video' || file.kind === 'image' && (['gif', 'webp'].includes(file.detectedFormat?.toLowerCase() ?? '') || /\.(gif|webp)$/i.test(file.filepath ?? '')))
}
