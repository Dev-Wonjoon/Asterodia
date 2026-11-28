import type { MoveSelection } from './file-move-api'
import type { EditJob, EditQuality } from './media-edit-api'

export interface ConversionFormat {
    type: 'video' | 'audio'
    extension?: string
    videoCodec?: string
    audioCodec?: string
}
export interface OutputFormat {
    type: 'video' | 'audio' | 'image'
    extension: string
    videoCodecs: string[]
    audioCodecs: string[]
}
export interface BatchRequest {
    requestId: string
    selection: MoveSelection[]
    mode: 'convert' | 'webp'
    format: ConversionFormat
    start: number
    duration: number
    width: number
    fps: number
    loop: number
    quality: EditQuality
    outputDirectory?: string
}
export interface BatchOptions {
    id: string
    requestHash: string
    mediaId: number
    fileId: number
    title: string
    mode: 'convert' | 'webp'
    format: ConversionFormat
    start: number
    duration: number
    width: number
    fps: number
    loop: number
}
export interface BatchResult {
    jobs: EditJob[]
    failures: { mediaId: number; fileId: number; message: string }[]
}

export const activeConversionStates = new Set(['pending', 'running', 'verifying', 'publishing', 'stop_unconfirmed'])
export const retryableConversionStates = new Set(['failed', 'cancelled', 'interrupted'])

// The newest attempt represents each source within a batch; successful siblings
// are never submitted again when retrying failures.
export function latestBatchJobs(jobs: EditJob[]): EditJob[] {
    const latest = new Map<string, EditJob>()
    for (const job of jobs) {
        const batch = job.options.batch
        if (!batch) continue
        const key = `${batch.id}:${batch.fileId}`
        const previous = latest.get(key)
        if (!previous || job.createdAt > previous.createdAt) latest.set(key, job)
    }
    return [...latest.values()].sort((a, b) => b.createdAt - a.createdAt)
}
