import { Window } from "@wailsio/runtime";
import type { CookieState } from "@contracts/cookie-api";
import type { CookieState as CookieTransport } from "../../../bindings/github.com/Dev-Wonjoon/Asterodia/internal/settings/models";
import { translate } from '@renderer/shared/i18n'
import { Service as FileMoveService } from "../../../bindings/github.com/Dev-Wonjoon/Asterodia/internal/filemove";
import { Service as UpdateService } from "../../../bindings/github.com/Dev-Wonjoon/Asterodia/internal/update";
import { Service as MediaEditService } from "../../../bindings/github.com/Dev-Wonjoon/Asterodia/internal/mediaedit";
import type { AppApi } from "@contracts/api";
import { Service as WebServerService } from "../../../bindings/github.com/Dev-Wonjoon/Asterodia/internal/webserver";
import { Service as LocalImportService } from "../../../bindings/github.com/Dev-Wonjoon/Asterodia/internal/localimport";
import { Service as DownloadService } from "../../../bindings/github.com/Dev-Wonjoon/Asterodia/internal/download";
import { Manager as EngineManager } from "../../../bindings/github.com/Dev-Wonjoon/Asterodia/internal/engine";
import type { EngineStatus } from "@contracts/types";
import { Events } from "@wailsio/runtime";
import { Service as MediaService } from "../../../bindings/github.com/Dev-Wonjoon/Asterodia/internal/media";
import { Service as ThumbnailService } from "../../../bindings/github.com/Dev-Wonjoon/Asterodia/internal/thumbnail";
import { Service as SettingsService } from "../../../bindings/github.com/Dev-Wonjoon/Asterodia/internal/settings";
import { SystemService, Service as DesktopService } from "../../../bindings/github.com/Dev-Wonjoon/Asterodia/internal/desktop";
import type { Result } from "../../../bindings/github.com/Dev-Wonjoon/Asterodia/internal/api/models";

// The Go bindings describe optional JSON fields as required zero values. Keep
// that transport detail at this boundary; renderer callers use the app contracts.
function wire<T>(request: unknown): T { return request as T; }
async function response<T>(pending: Promise<Result>): Promise<T> {
    const value = await pending;
    if (typeof value.ok !== "boolean" || (!value.ok && !value.error)) {
        throw new Error(translate("common.invalidAppServiceResponse"));
    }
    return value as T;
}

function cookieState(value: CookieTransport): CookieState {
    return { ...value, groups: (value.groups ?? []).map(group => ({ ...group, entries: group.entries ?? [] })) };
}

const api: AppApi = {
    onMediaMetadataUpdated: listener => Events.On('media:metadata-updated', () => listener()),
    windowFullscreen: {
        enter: async () => { if (await Window.IsFullscreen()) return false; await Window.Fullscreen(); return true; },
        exit: () => Window.UnFullscreen(),
        onExit: listener => Events.On('common:WindowUnFullscreen', () => listener()),
    },
    cookies: {
        list: async () => cookieState(await SettingsService.GetCookies()),
        importFile: async () => { const value = await SettingsService.ImportCookies(); return value ? cookieState(value) : null; },
        deleteEntry: async id => cookieState(await SettingsService.DeleteCookie(id)),
        deleteSite: async site => cookieState(await SettingsService.DeleteCookieSite(site)),
        setEnabled: async (site, enabled) => cookieState(await SettingsService.SetCookieSiteEnabled(site, enabled)),
    },
    getContentFilters: async () => { const value = await SettingsService.GetContentFilters(); return { hiddenPlatforms: value.hiddenPlatforms ?? [], hiddenTags: value.hiddenTags ?? [] } },
    setContentFilters: async request => { const value = await SettingsService.SetContentFilters(wire(request)); return { hiddenPlatforms: value.hiddenPlatforms ?? [], hiddenTags: value.hiddenTags ?? [] } },
    updates: {
        status: () => response(UpdateService.Status()),
        check: () => response(UpdateService.Check()),
        download: () => response(UpdateService.Download()),
        restart: () => response(UpdateService.Restart()),
    },
    mediaEdit: {
        outputOptions: request => response(MediaEditService.OutputOptions(wire(request))),
        overview: request => response(MediaEditService.Overview(wire(request))),
        frames: request => response(MediaEditService.Frames(wire(request))),
        frameStatus: request => response(MediaEditService.FrameStatus(wire(request))),
        selection: request => response(MediaEditService.Selection(wire(request))),
        resources: request => response(MediaEditService.Resources(wire(request))),
        resultPath: request => response(MediaEditService.ResultPath(request)),
        prepare: request => response(MediaEditService.Prepare(wire(request))),
        getSession: request => response(MediaEditService.GetSession(request)),
        create: request => response(MediaEditService.Create(wire(request))),
        getJob: request => response(MediaEditService.GetJob(request)),
        list: () => response(MediaEditService.List()),
        cancel: request => response(MediaEditService.Cancel(request)),
        release: request => response(MediaEditService.Release(request)),
    },
 fileMove: {
  create: request => response(FileMoveService.Create(wire(request))),
  get: (request = {}) => response(FileMoveService.Get(wire(request))),
  run: request => response(FileMoveService.Run(wire(request))),
  pause: request => response(FileMoveService.Pause(wire(request))),
  retry: request => response(FileMoveService.Retry(wire(request))),
 },
    webServer: {
        setRequireCode: required => response(WebServerService.SetRequireCode(required)),
        revokeAccess: () => response(WebServerService.RevokeAccess()),
        setEditing: enabled => response(WebServerService.SetEditing(enabled)),
        setDownloading: enabled => response(WebServerService.SetDownloading(enabled)),
        status: () => response(WebServerService.Status()),
        start: () => response(WebServerService.Start()),
        stop: () => response(WebServerService.Stop()),
    },
    localImport: {
        choose: () => response(LocalImportService.Choose()),
        get: (request = {}) => response(LocalImportService.Get(wire(request))),
        run: (request) => response(LocalImportService.Run(wire(request))),
        cancel: (request) => response(LocalImportService.Cancel(wire(request))),
        rescan: (request) => response(LocalImportService.Rescan(wire(request))),
        pause: (request) => response(LocalImportService.Pause(wire(request))),
        resume: (request) => response(LocalImportService.Resume(wire(request))),
        files: (request) => response(LocalImportService.Files(wire(request))),
        retry: (request) => response(LocalImportService.Retry(wire(request))),
        exclude: (request) => response(LocalImportService.Exclude(wire(request))),
    },
    database: { import: () => response(DesktopService.ImportDatabase()) },
    media: {
        bulkEdit: request => response(MediaService.BulkEdit(wire(request))),
        metadata: request => response(MediaService.Metadata(wire(request))),
        exploreAlbum: request => response(MediaService.ExploreAlbum(wire(request))),
        albums: (request = {}) => response(MediaService.Albums(wire(request))),
        saveAlbum: request => response(MediaService.SaveAlbum(wire(request))),
        mergeAlbum: request => response(MediaService.MergeAlbum(wire(request))),
        sequence: request => response(MediaService.Sequence(wire(request))),
        readFile: request => response(MediaService.ReadFile(wire(request))),
        directories: (request = {}) => response(MediaService.Directories(wire(request))),
        list: (request = {}) => response(MediaService.List(wire(request))),
        get: (request) => response(MediaService.Get(wire(request))),
        edit: (request) => response(MediaService.Edit(wire(request))),
        unregisterFiles: (request) => response(MediaService.UnregisterFiles(wire(request))),
        unregisterMedia: (request) => response(MediaService.UnregisterMedia(wire(request))),
        suggest: (request) => response(MediaService.Suggest(wire(request))),
        searchSuggest: (request) => response(MediaService.SearchSuggest(wire(request))),
    },
    downloads: {
        remove: (request) => response(DownloadService.Delete(request)),
        logs: (request) => response(DownloadService.Logs(wire(request))),
        async previewEngine(url, options = {}) {
            const kind = await DownloadService.PreviewEngine(url, {
                type: options.type ?? "",
                quality: options.quality ?? "",
                extension: options.extension ?? "",
                playlist: options.playlist,
            });

            if (kind !== "yt-dlp" && kind !== "gallery-dl") {
                throw new Error(translate("common.couldNotSelectAnEngine"));
            }

            return kind;
        },

        enqueue: (request) => response(DownloadService.Enqueue(wire(request))),
        list: (request = {}) => response(DownloadService.List(wire(request))),
        get: (request) => response(DownloadService.Get(request)),
        run: (request) => response(DownloadService.Run(request)),
        cancel: (request) => response(DownloadService.Cancel(request)),
        cleanup: (request) => response(DownloadService.Cleanup(request)),
    },
    thumbnails: {
        cacheUsage: () => response(ThumbnailService.CacheUsage()),
        clearUnusedCache: () => response(ThumbnailService.ClearUnusedCache()),
        get: (request) => response(ThumbnailService.Get(request)),
        retry: (request) => response(ThumbnailService.Retry(request)),
    },
    getDownloadPreview: ThumbnailService.GetDownloadPreview,
    showItemInFolder: SettingsService.ShowItemInFolder,
    getDoubleClickInterval: SystemService.GetDoubleClickInterval,
    getDownloadPath: SettingsService.GetDownloadPath,
    getWorkingDirectory: SettingsService.GetWorkingDirectory,
    setWorkingDirectory: SettingsService.SetWorkingDirectory,
    clearWorkingDirectory: SettingsService.ClearWorkingDirectory,
    chooseEditOutputDirectory: SettingsService.ChooseEditOutputDirectory,
    setDownloadPath: SettingsService.SetDownloadPath,
    async getEngineStatus(): Promise<Record<string, EngineStatus>> {
        const statuses = await EngineManager.Status();

        if (statuses === null) {
            throw new Error(translate("common.theEngineStatusResponseIsEmpty"));
        }

        const result: Record<string, EngineStatus> = {};

        for (const [name, status] of Object.entries(statuses)) {
            if (status === undefined) {
                throw new Error(translate("common.noStatusResponseFor", { engine: name }));
            }

            result[name] = status;
        }

        return result;
    },
    installEngine: (name, installId = crypto.randomUUID()) =>
        EngineManager.Install(name, installId),
    onEngineInstallProgress: (listener) =>
        Events.On("engine:install-progress", (event) => {
            listener(event.data);
        }),
    getEngineLicenses: async () => (await SettingsService.GetEngineLicenses()) ?? [],
    getCookieBrowser: SettingsService.GetCookieBrowser,
    setCookieBrowser: SettingsService.SetCookieBrowser,
    getCookieFilePath: SettingsService.GetCookieFilePath,
    setCookieFilePath: SettingsService.SetCookieFilePath,
    clearCookieFilePath: SettingsService.ClearCookieFilePath,
    onMediaThumbnailUpdated: (listener) => Events.On("media:thumbnail-updated", (event) => listener(event.data)),
};

export function getApi(): AppApi {
    return api;
}
