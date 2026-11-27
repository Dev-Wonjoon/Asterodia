// Legacy names are read only for migration. Existing new preferences win, and
// old values remain available for recovery if storage is full or unavailable.
const legacyPrefix = 'storagebucket';
export function readPreference(key, suppliedStorage) {
    try {
        const storage = suppliedStorage ?? globalThis.localStorage;
        if (!storage) return null;
        const current = storage.getItem(key);
        if (current !== null) return current;
        const legacyKey = key === 'asterodia:theme:v1' ? 'theme'
            : key.startsWith('asterodia') ? legacyPrefix + key.slice('asterodia'.length) : null;
        if (!legacyKey) return null;
        const previous = storage.getItem(legacyKey);
        if (previous !== null) {
            try { storage.setItem(key, previous); } catch { /* Keep the old value usable in this session. */ }
        }
        return previous;
    } catch { return null; }
}
