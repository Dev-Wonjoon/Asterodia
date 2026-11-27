import type { AppApi } from '@contracts/api'
import { getLanguage } from '@renderer/shared/i18n'
import { createDemoModel } from './model'

export const demo = createDemoModel()
const unavailable = () => getLanguage() === 'ko'
    ? '이 기능은 데스크톱 앱에서 이용할 수 있습니다. 데모에서는 샘플 미디어를 정리하고 재생해 보세요.'
    : 'This feature is available in the desktop app. Try organizing and playing the sample media here.'
// Unsupported native operations always return a visible, local error; never a real request.
function desktopOnly<T>(overrides: Partial<T> = {}): T {
    return new Proxy(overrides as T & object, { get(target, key) {
        if (key in target) return Reflect.get(target, key)
        return async () => ({ ok: false, error: { code: 'DEMO_ONLY', message: unavailable(), context: {} } })
    } }) as T
}
const api: AppApi = {
    media: demo.media, downloads: demo.downloads,
    localImport: desktopOnly<AppApi['localImport']>({ get: async () => ({ ok: true, data: { job: null, folders: [], total: 0, hasNextPage: false } }) }),
    fileMove: desktopOnly<AppApi['fileMove']>({ get: async () => ({ ok: true, data: null }) }),
    updates: desktopOnly(), mediaEdit: desktopOnly(), database: desktopOnly(), webServer: desktopOnly(),
    thumbnails: { get: async () => ({ ok: true, data: { state: 'registered', taskId: null, canRetry: false } }),
        retry: async () => ({ ok: true, data: { state: 'registered', taskId: null, canRetry: false } }) },
    getContentFilters: async () => ({ hiddenPlatforms: [], hiddenTags: [] }),
    setContentFilters: async value => value,
    getDownloadPreview: async () => 'sample:coast',
    showItemInFolder: async () => { throw new Error(unavailable()) },
    getDoubleClickInterval: async () => 400,
    getDownloadPath: async () => '', setDownloadPath: async () => null,
    getWorkingDirectory: async () => '', setWorkingDirectory: async () => null,
    clearWorkingDirectory: async () => true, chooseEditOutputDirectory: async () => null,
    getEngineStatus: async () => Object.fromEntries(['yt-dlp', 'gallery-dl', 'ffmpeg', 'ffprobe', 'magick'].map(name => [name, { installed: true, version: 'Demo', path: '' }])),
    installEngine: async () => false, onEngineInstallProgress: () => () => {}, getEngineLicenses: async () => [],
    getCookieBrowser: async () => '', setCookieBrowser: async () => false,
    getCookieFilePath: async () => '', setCookieFilePath: async () => null, clearCookieFilePath: async () => true,
    onMediaThumbnailUpdated: listener => demo.subscribe(listener),
}
export const getApi = () => api
