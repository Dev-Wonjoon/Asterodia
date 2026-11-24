import type { DownloadJobDto, DownloadJobOptions, DownloadPageDto, DownloadResponse } from '@contracts/download-api'
import { translate } from '@renderer/shared/i18n'

let csrf = ''
async function call<T>(name: string, value?: unknown, read = false): Promise<T> {
    const response = await fetch('/api/download/' + name, read ? { credentials: 'same-origin' } : {
        method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json', 'X-StorageBucket-CSRF': csrf }, body: JSON.stringify(value),
    })
    if (!response.headers.get('Content-Type')?.includes('application/json')) {
        throw new Error(translate(response.status === 403 ? 'web.download.permission' : response.status === 429 ? 'web.download.rateLimited' : 'common.couldNotConnectToTheServerTryAgainShortly'))
    }
    return response.json()
}
export const webDownloads = {
    async status() { const access = await call<{ enabled: boolean; csrf: string }>('status', undefined, true); csrf = access.csrf; return access },
    list: (offset = 0) => call<DownloadResponse<DownloadPageDto>>('list?offset=' + offset, undefined, true),
    start: (url: string, options: DownloadJobOptions) => call<DownloadResponse<DownloadJobDto>>('enqueue', { url, options }),
    retry: (jobId: string) => call<DownloadResponse<DownloadJobDto>>('retry', { jobId }),
    cancel: (jobId: string) => call<DownloadResponse<{ accepted: true }>>('cancel', { jobId }),
    cleanup: (jobId: string) => call<DownloadResponse<{ accepted: true }>>('cleanup', { jobId }),
}
