import type { EngineStatus, EngineInstallProgress, LicenseInfo } from "./types";
import type { MediaApi } from "./media-api";
import type { DownloadApi } from "./download-api";
import type { ThumbnailApi } from "./thumbnail-api";
import type { DatabaseApi } from "./database-api";
import type { LocalImportApi } from "./local-import-api";
import type { WebServerApi } from "./webserver-api";
import type { MediaEditApi } from "./media-edit-api";
import type { UpdateApi } from "./update-api";

export type Unsubscribe = () => void;

import type { FileMoveApi } from "./file-move-api";
import type { ContentFilters } from './media-v2';

export interface AppApi {
    getContentFilters(): Promise<ContentFilters>;
    setContentFilters(value: ContentFilters): Promise<ContentFilters>;
    updates: UpdateApi;
    mediaEdit: MediaEditApi;
    fileMove: FileMoveApi;
    webServer: WebServerApi;
    localImport: LocalImportApi;
    database: DatabaseApi;
    media: MediaApi;
    downloads: DownloadApi;
    thumbnails: ThumbnailApi;

    getDownloadPreview(url: string): Promise<string | null>;
    showItemInFolder(filepath: string): Promise<void>;
    getDoubleClickInterval(): Promise<number | null>;

    getDownloadPath(): Promise<string>;
    setDownloadPath(): Promise<string | null>;
    getWorkingDirectory(): Promise<string>;
    setWorkingDirectory(): Promise<string | null>;
    clearWorkingDirectory(): Promise<boolean>;
    chooseEditOutputDirectory(): Promise<string | null>;

    getEngineStatus(): Promise<Record<string, EngineStatus>>;
    installEngine(engine: string, installId?: string): Promise<boolean>;
    onEngineInstallProgress(
        listener: (progress: EngineInstallProgress) => void,
    ): Unsubscribe;
    getEngineLicenses(): Promise<LicenseInfo[]>;

    getCookieBrowser(): Promise<string>;
    setCookieBrowser(browser: string): Promise<boolean>;

    getCookieFilePath(): Promise<string>;
    setCookieFilePath(): Promise<string | null>;
    clearCookieFilePath(): Promise<boolean>;
    onMediaThumbnailUpdated(listener: (mediaId: number) => void): Unsubscribe;
}
