export interface CookieEntry {
    id: string;
    domain: string;
    subdomains: boolean;
    path: string;
    name: string;
    secure: boolean;
    httpOnly: boolean;
    expires: number;
    expired: boolean;
    source: string;
}
export interface CookieGroup { site: string; count: number; active: number; enabled: boolean; entries: CookieEntry[] }
export interface CookieState { groups: CookieGroup[]; warning: string }
export interface CookieApi {
    list(): Promise<CookieState>;
    importFile(): Promise<CookieState | null>;
    deleteEntry(id: string): Promise<CookieState>;
    deleteSite(site: string): Promise<CookieState>;
    setEnabled(site: string, enabled: boolean): Promise<CookieState>;
}
