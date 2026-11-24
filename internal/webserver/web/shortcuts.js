// Shared keyboard definitions and preferences for React and native DOM widgets.
import { translate } from './i18n.js';

const define = (id, scope, label, keys, repeat = false) => ({ id, scope, label, keys, repeat });
export const shortcuts = [
    define('app.gallery', 'app', "navigation.gallery", 'Ctrl+1'),
    define('app.downloads', 'app', "navigation.downloads", 'Ctrl+2'),
    define('app.search', 'app', "navigation.search", 'Ctrl+3'),
    define('app.favorites', 'app', "navigation.favorites", 'Ctrl+4'),
    define('app.settings', 'app', "navigation.settings", 'Alt+,'),
    define('app.find', 'app', "search.media", 'Alt+F'),
    define('app.commands', 'app', "shortcuts.commandSearch", 'Alt+K'),
    define('app.url', 'app', "download.downloadUrl", 'Alt+L'),
    define('app.import', 'app', "gallery.importFolder", 'Alt+O'),
    define('app.nextRegion', 'app', "common.moveToTheNextRegion", 'F6', true),
    define('app.previousRegion', 'app', "common.moveToThePreviousRegion", 'Shift+F6', true),
    define('app.help', 'app', "shortcuts.title", 'F1'),
    define('gallery.left', 'gallery', "common.itemOnTheLeft", 'ArrowLeft', true),
    define('gallery.right', 'gallery', "common.itemOnTheRight", 'ArrowRight', true),
    define('gallery.up', 'gallery', "common.itemAbove", 'ArrowUp', true),
    define('gallery.down', 'gallery', "common.itemBelow", 'ArrowDown', true),
    define('gallery.first', 'gallery', "common.firstItem", 'Home'),
    define('gallery.last', 'gallery', "common.lastItem", 'End'),
    define('gallery.select', 'gallery', "common.toggleSelection", 'Space'),
    define('gallery.all', 'gallery', "gallery.selection.all", 'Ctrl+A'),
    define('gallery.view', 'gallery', "viewer.open", 'Enter'),
    define('gallery.details', 'gallery', "gallery.details", 'Alt+Enter'),
    define('gallery.menu', 'gallery', "common.itemMenu", 'Shift+F10'),
    define('gallery.favorite', 'gallery', "common.toggleFavorite", 'F'),
    define('gallery.tags', 'gallery', "gallery.action.editTags", 'T'),
    define('gallery.createAlbum', 'gallery', "gallery.album.create", 'Ctrl+N'),
    define('gallery.addToAlbum', 'gallery', "gallery.album.add", 'Ctrl+Shift+A'),
    define('gallery.folder', 'gallery', "gallery.openFolder", 'Ctrl+Shift+O'),
    define('gallery.unregister', 'gallery', "gallery.unregister", 'Delete'),
    define('gallery.refresh', 'gallery', 'common.refresh', ''),
    define('gallery.gather', 'gallery', 'gallery.collectInOneFolder', ''),
    define('gallery.merge', 'gallery', 'gallery.merge', ''),
    define('downloads.retryAll', 'downloads', 'download.retryFailedAndCancelled', ''),
    define('downloads.refresh', 'downloads', 'common.refresh', ''),
    define('viewer.previous', 'viewer', "gallery.previousFiles", 'ArrowLeft', true),
    define('viewer.next', 'viewer', "gallery.nextFiles", 'ArrowRight', true),
    define('player.play', 'player', "common.playPause", 'Space'),
    define('player.playAlternative', 'player', "common.playPause", 'K'),
    define('player.backward', 'player', "common.back5Seconds", 'J', true),
    define('player.forward', 'player', "common.forward5Seconds", 'L', true),
    define('player.mute', 'player', "player.mute", 'M'),
    define('player.fullscreen', 'player', "player.fullscreen", 'F'),
    define('editor.previous', 'editor', "editor.previousFrame", 'ArrowLeft', true),
    define('editor.next', 'editor', "editor.nextFrame", 'ArrowRight', true),
    define('editor.start', 'editor', "common.setRangeStart", 'I'),
    define('editor.end', 'editor', "common.setRangeEnd", 'O'),
    define('editor.delete', 'editor', "editor.deleteSelectedFrames", 'Delete'),
    define('editor.undo', 'editor', "editor.undo", 'Ctrl+Z'),
    define('editor.redo', 'editor', "editor.redo", 'Ctrl+Shift+Z'),
    define('editor.export', 'editor', "common.export", 'Ctrl+Enter'),
];
export const shortcutStorageKey = 'storagebucket:shortcuts:v1';
const listeners = new Set(), commands = new Map();
let version = 0, memory;
function notify() { version++; for (const fn of listeners) fn(); }
export function subscribeShortcuts(fn) {
    listeners.add(fn);
    if (listeners.size === 1) globalThis.addEventListener?.('storage', storage);
    return () => { listeners.delete(fn); if (!listeners.size) globalThis.removeEventListener?.('storage', storage); };
}
function storage(event) { if (event.key === shortcutStorageKey || event.key === null) { memory = undefined; notify(); } }
export const shortcutVersion = () => version;
function preferences() {
    if (memory) return memory;
    try { const value = JSON.parse(globalThis.localStorage?.getItem(shortcutStorageKey) || '{}'); memory = value && typeof value === 'object' && !Array.isArray(value) ? value : {}; }
    catch { memory = {}; }
    return memory;
}
export function shortcutKeys(id) { const value = preferences()[id]; return typeof value === 'string' ? value : shortcuts.find(item => item.id === id)?.keys || ''; }
export function formatShortcut(keys) { return keys.replaceAll('ArrowLeft', '←').replaceAll('ArrowRight', '→').replaceAll('ArrowUp', '↑').replaceAll('ArrowDown', '↓'); }
export function shortcutAria(keys) { return keys.replace('Ctrl+', 'Control+').replace(/\+([A-Z])$/, (_, key) => '+' + key.toLowerCase()); }
export function scopeTitle(scope) {
    const labels = { app: 'shortcuts.scope.app', gallery: 'shortcuts.scope.gallery', downloads: 'navigation.downloads', viewer: 'shortcuts.scope.viewer', player: 'shortcuts.scope.player', editor: 'shortcuts.scope.editor' };
    return translate(labels[scope] || scope);
}
export function eventChord(event) {
    let key = event.key;
    if (/^Key[A-Z]$/.test(event.code)) key = event.code.slice(3);
    else if (/^Digit[0-9]$/.test(event.code)) key = event.code.slice(5);
    else if (event.code === 'Comma') key = ',';
    if (['Control', 'Alt', 'Shift', 'Meta', 'Process', 'Dead', 'Unidentified'].includes(key)) return '';
    if (key === ' ') key = 'Space';
    if (key?.length === 1) key = key.toUpperCase();
    return [...(event.ctrlKey || event.metaKey ? ['Ctrl'] : []), ...(event.altKey ? ['Alt'] : []), ...(event.shiftKey ? ['Shift'] : []), key].join('+');
}
export const isComposing = event => event.isComposing || event.keyCode === 229;
export const isTextInput = target => Boolean(target?.closest?.('input,textarea,select,[contenteditable]:not([contenteditable="false"]),[role="slider"]'));
export function blockingDialog(doc = document) { return [...doc.querySelectorAll('dialog[open],[aria-modal="true"],[data-confirm-dialog]')].at(-1); }
export function matchShortcut(event, scope) {
    if (event.defaultPrevented || isComposing(event) || event.key === 'AltGraph' || event.ctrlKey && event.altKey && event.getModifierState?.('AltGraph')) return;
    const chord = eventChord(event);
    if (!chord) return;
    return shortcuts.find(item => item.scope === scope && shortcutKeys(item.id) === chord && (!event.repeat || item.repeat));
}
export function setShortcut(id, keys) {
    const item = shortcuts.find(value => value.id === id);
    if (!item || keys === 'Escape' || keys === 'Tab' || keys === 'Shift+Tab') return 'reserved';
    const scopesOverlap = other => other.scope === item.scope || other.scope === 'app' || item.scope === 'app' || ['viewer', 'player'].includes(item.scope) && ['viewer', 'player'].includes(other.scope);
    const bindings = (entry, key) => {
        if (!key || !['gallery.left', 'gallery.right', 'gallery.up', 'gallery.down', 'gallery.first', 'gallery.last'].includes(entry.id) || key.includes('Shift+')) return [key];
        const parts = key.split('+'); parts.splice(parts.length - 1, 0, 'Shift'); return [key, parts.join('+')];
    };
    const proposed = bindings(item, keys);
    if (keys && shortcuts.some(other => other.id !== id && scopesOverlap(other) && bindings(other, shortcutKeys(other.id)).some(key => proposed.includes(key)))) return 'conflict';
    memory = { ...preferences(), [id]: keys };
    try { globalThis.localStorage?.setItem(shortcutStorageKey, JSON.stringify(memory)); } catch { /* Keep session preferences usable. */ }
    notify(); return null;
}
export function resetShortcuts() { memory = {}; try { globalThis.localStorage?.removeItem(shortcutStorageKey); } catch {} notify(); }
export function registerCommands(entries) {
    const owned = entries.map(entry => { commands.set(entry.id, entry); return entry; }); notify();
    return () => { for (const entry of owned) if (commands.get(entry.id) === entry) commands.delete(entry.id); notify(); };
}
export function availableCommands() { return shortcuts.filter(item => commands.has(item.id) && commands.get(item.id).enabled()).map(item => ({ ...item, title: translate(item.label), run: () => runCommand(item.id) })); }
export function runCommand(id) { const command = commands.get(id); if (!command || !command.enabled()) return false; return command.run() !== false; }
export function commandEnabled(id) { return Boolean(commands.get(id)?.enabled()); }
