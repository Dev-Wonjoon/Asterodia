const key = 'storagebucket:expanded-albums:v1';
export function readExpandedAlbums() {
    try {
        const value = JSON.parse(localStorage.getItem(key) || '[]');
        return Array.isArray(value) ? [...new Set(value.filter(id => Number.isSafeInteger(id) && id > 0))].slice(0, 10000) : [];
    } catch { return []; }
}
export function saveExpandedAlbums(ids) {
    try { localStorage.setItem(key, JSON.stringify(ids)); } catch { /* Still usable when browser storage is unavailable. */ }
}
