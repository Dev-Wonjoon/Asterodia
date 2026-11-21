import { Service as FileMoveService } from "../../../bindings/github.com/Dev-Wonjoon/StorageBucket/internal/filemove";
import { Service as MediaEditService } from "../../../bindings/github.com/Dev-Wonjoon/StorageBucket/internal/mediaedit";
import type { AppApi } from "@contracts/api";
import { Service as WebServerService } from "../../../bindings/github.com/Dev-Wonjoon/StorageBucket/internal/webserver";
import { Service as LocalImportService } from "../../../bindings/github.com/Dev-Wonjoon/StorageBucket/internal/localimport";
import { Service as DownloadService } from "../../../bindings/github.com/Dev-Wonjoon/StorageBucket/internal/download";
import { Manager as EngineManager } from "../../../bindings/github.com/Dev-Wonjoon/StorageBucket/internal/engine";
import type { EngineStatus } from "@contracts/types";
import { Events } from "@wailsio/runtime";
import { Service as MediaService } from "../../../bindings/github.com/Dev-Wonjoon/StorageBucket/internal/media";
import { Service as ThumbnailService } from "../../../bindings/github.com/Dev-Wonjoon/StorageBucket/internal/thumbnail";
import { Service as SettingsService } from "../../../bindings/github.com/Dev-Wonjoon/StorageBucket/internal/settings";
import { SystemService, Service as DesktopService } from "../../../bindings/github.com/Dev-Wonjoon/StorageBucket/internal/desktop";
import type { Result } from "../../../bindings/github.com/Dev-Wonjoon/StorageBucket/internal/api/models";

// The Go bindings describe optional JSON fields as required zero values. Keep
// that transport detail at this boundary; renderer callers use the app contracts.
function wire<T>(request: unknown): T { return request as T; }
async function response<T>(pending: Promise<Result>): Promise<T> {
    const value = await pending;
    if (typeof value.ok !== "boolean" || (!value.ok && !value.error)) {
        throw new Error("앱 서비스 응답 형식이 올바르지 않습니다.");
    }
    return value as T;
}

const api: AppApi = {
    mediaEdit: {
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
        setEditing: enabled => response(WebServerService.SetEditing(enabled)),
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
                throw new Error("엔진을 선택하지 못했습니다.");
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
        get: (request) => response(ThumbnailService.Get(request)),
        retry: (request) => response(ThumbnailService.Retry(request)),
    },
    getDownloadPreview: ThumbnailService.GetDownloadPreview,
    showItemInFolder: SettingsService.ShowItemInFolder,
    getDoubleClickInterval: SystemService.GetDoubleClickInterval,
    getDownloadPath: SettingsService.GetDownloadPath,
    setDownloadPath: SettingsService.SetDownloadPath,
    async getEngineStatus(): Promise<Record<string, EngineStatus>> {
        const statuses = await EngineManager.Status();

        if (statuses === null) {
            throw new Error("엔진 상태 응답이 비어 있습니다.");
        }

        const result: Record<string, EngineStatus> = {};

        for (const [name, status] of Object.entries(statuses)) {
            if (status === undefined) {
                throw new Error(`${name}의 상태 응답이 없습니다.`);
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
