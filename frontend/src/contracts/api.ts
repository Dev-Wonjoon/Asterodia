import type { EngineStatus, EngineInstallProgress, LicenseInfo } from "./types";
import type { MediaApi } from "./media-api";
import type { DownloadApi } from "./download-api";
import type { ThumbnailApi } from "./thumbnail-api";
import type { DatabaseApi } from "./database-api";
import type { LocalImportApi } from "./local-import-api";
import type { WebServerApi } from "./webserver-api";

export type Unsubscribe = () => void;

export interface AppApi {
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
