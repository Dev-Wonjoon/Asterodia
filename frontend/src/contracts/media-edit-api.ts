import type { WebServerResult } from './webserver-api';
export interface EditPrepare {
    quickVideo?: boolean;
    originalFrames?: boolean;
    fullSource?: boolean;
    nativeVideo?: boolean;
    nativeGIF?: boolean;
    requestId: string;
    fileId: number;
    start: number;
    end: number;
    fps: number;
    width: number;
    kind: 'gif' | 'webp' | 'trim';
}
export interface EditQuality {
    preset: 'small' | 'balanced' | 'high' | 'max' | 'custom';
    quality: number;
    effort: 'fast' | 'normal' | 'precise';
    lossless: boolean;
    dither?: 'auto' | 'sierra2_4a' | 'sierra3' | 'floyd_steinberg' | 'bayer' | 'atkinson' | 'none';
    webpPreset?: 'none' | 'photo' | 'drawing' | 'icon' | 'text';
}
export interface EditUsage {
    known: boolean;
    currentBytes: number;
    observedPeakBytes: number;
    sampledAt: number;
    outputBytes: number;
    outputFrames: number;
}
export interface EditResources {
    memoryKnown: boolean;
    availableRam: number;
    totalRam: number;
    estimateKnown: boolean;
    lowRam: number;
    highRam: number;
    basis: string;
    note: string;
    pressure: boolean;
    diskKnown: boolean;
    diskAvailable: number;
    diskEstimateKnown: boolean;
    diskEstimate: number;
    live: EditUsage;
}
export interface EditRange {
    first: number;
    last: number;
}
export interface EditPage {
    times?: number[];
    durations?: number[];
    offset: number;
    count: number;
    state: string;
    error: string;
}
export interface EditCreate {
    rangeStart?: number;
    rangeEnd?: number;
    trimStart?: number;
    trimEnd?: number;
    requestId: string;
    sessionId: string;
    kind: 'gif' | 'webp' | 'trim';
    crop: {
        x: number;
        y: number;
        width: number;
        height: number;
    };
    deleted: number[];
    mute: boolean;
    quality?: EditQuality;
    selectionId?: string;
    preview?: boolean;
    previewStart?: number;
}
export interface EditSession {
    id: string;
    fileId: number | null;
    title: string;
    options: EditPrepare;
    state: string;
    width: number;
    height: number;
    frameCount: number;
    renderVersion: number;
    progress: number;
    error: string;
    cancelling: boolean;
    createdAt: number;
}
export interface EditJob {
    id: string;
    sessionId: string;
    options: EditCreate;
    state: string;
    progress: number;
    resultFileId: number | null;
    preview: boolean;
    resources: EditUsage;
    error: string;
    cancelling: boolean;
    createdAt: number;
}
export interface MediaEditApi {
    overview(request: { fileId: number; index: number }): Promise<WebServerResult<{
        index: number; count: number; time: number; duration: number; frameCount: number; image: string;
    }>>;
    frames(request: {
        id: string;
        offset: number;
        retry?: boolean;
    }): Promise<WebServerResult<EditPage>>;
    frameStatus(request: {
        id: string;
        offset: number;
    }): Promise<WebServerResult<EditPage>>;
    selection(request: {
        id: string;
        sessionId: string;
        offset: number;
        ranges: EditRange[];
        final: boolean;
    }): Promise<WebServerResult<string>>;
    resources(request: EditCreate): Promise<WebServerResult<EditResources>>;
    resultPath(request: {
        id: string;
    }): Promise<WebServerResult<string>>;
    prepare(request: EditPrepare): Promise<WebServerResult<EditSession>>;
    getSession(request: {
        id: string;
    }): Promise<WebServerResult<EditSession>>;
    create(request: EditCreate): Promise<WebServerResult<EditJob>>;
    getJob(request: {
        id: string;
    }): Promise<WebServerResult<EditJob>>;
    list(): Promise<WebServerResult<{
        sessions: EditSession[];
        jobs: EditJob[];
    }>>;
    cancel(request: {
        id: string;
    }): Promise<WebServerResult<null>>;
    release(request: {
        id: string;
    }): Promise<WebServerResult<null>>;
}
