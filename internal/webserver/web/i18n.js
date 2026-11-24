import { catalogs } from './translations.js';

export const languageKey = 'storagebucket:language:v1';
const listeners = new Set();
let memoryPreference = 'system', volatile = false;
export function getLanguagePreference() {
    if (volatile) return memoryPreference;
    try {
        const saved = globalThis.localStorage?.getItem(languageKey);
        return saved === 'ko' || saved === 'en' ? saved : 'system';
    } catch { return memoryPreference; }
}
export function resolveLanguage(preference, languages = globalThis.navigator?.languages ?? [globalThis.navigator?.language ?? 'ko']) {
    if (preference === 'ko' || preference === 'en') return preference;
    // The primary system language determines the fallback, even if Korean is secondary.
    return /^ko(?:-|$)/i.test(languages[0] ?? '') ? 'ko' : 'en';
}
export function getLanguage() { return resolveLanguage(getLanguagePreference()); }
export function getLocale() { return getLanguage() === 'ko' ? 'ko-KR' : 'en-US'; }
function updateDocument() {
    if (globalThis.document) document.documentElement.lang = getLanguage();
}
function notify() { updateDocument(); for (const listener of listeners) listener(); }
export function setLanguage(preference) {
    if (!['system', 'ko', 'en'].includes(preference)) return;
    memoryPreference = preference;
    try { globalThis.localStorage?.setItem(languageKey, preference); volatile = false; }
    catch { volatile = true; }
    notify();
}
function storage(event) {
    if (event.key === languageKey || event.key === null) { volatile = false; notify(); }
}
export function subscribeLanguage(listener) {
    if (!listeners.size && globalThis.window) {
        window.addEventListener('storage', storage);
        window.addEventListener('languagechange', notify);
    }
    listeners.add(listener);
    updateDocument();
    return () => {
        listeners.delete(listener);
        if (!listeners.size && globalThis.window) {
            window.removeEventListener('storage', storage);
            window.removeEventListener('languagechange', notify);
        }
    };
}
export function translate(key, values = {}) {
    const catalog = catalogs[getLanguage()];
    const text = Object.hasOwn(catalog, key) ? catalog[key] : Object.hasOwn(catalogs.en, key) ? catalogs.en[key] : key;
    // Replace once so braces in user titles and paths remain literal.
    return text.replace(/\{(\w+)\}/g, (token, name) => Object.hasOwn(values, name) ? String(values[name]) : token);
}
const escapeRegExp = value => value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
const messagePatterns = [];
const messageKeys = new Map();
for (const key of Object.keys(catalogs.en)) {
    for (const text of new Set([catalogs.ko[key], catalogs.en[key]])) {
        messageKeys.set(text, key);
        if (!/\{\w+\}/.test(text)) continue;
        const names = [], pieces = []; let end = 0;
        for (const match of text.matchAll(/\{(\w+)\}/g)) {
            pieces.push(escapeRegExp(text.slice(end, match.index)), '([\\s\\S]*?)');
            names.push(match[1]); end = match.index + match[0].length;
        }
        pieces.push(escapeRegExp(text.slice(end)));
        messagePatterns.push({ key, names, pattern: new RegExp('^' + pieces.join('') + '$') });
    }
}
// Used only for application messages held in state, never user media metadata.
export function translateMessage(message) {
    if (!message) return message;
    if (typeof message === 'object' && typeof message.key === 'string') return translate(message.key, message.values);
    const key = messageKeys.get(message);
    if (key) return translate(key);
    for (const { key, names, pattern } of messagePatterns) {
        const match = pattern.exec(message);
        if (match) return translate(key, Object.fromEntries(names.map((name, i) => [name, match[i + 1]])));
    }
    return message;
}
const errorMessages = {
    INVALID_ARGUMENT: "common.checkTheRequestValues", NOT_FOUND: "common.theRequestedItemWasNotFound",
    NOT_READY: "common.thisFeatureIsNotReadyYet", REVISION_CONFLICT: "common.theInformationHasChangedRefreshAndCheckAgain",
    INTERNAL_ERROR: "settings.couldNotProcessTheRequest", DATABASE_ERROR: "common.couldNotProcessTheDatabaseOperation",
    INVALID_LOCATION: "common.checkTheFileLocation", JOB_ACTIVE: "common.aJobIsAlreadyRunning",
    FILE_MOVE_FAILED: "common.couldNotFinishCopyingOrMovingFiles", LOCAL_IMPORT_FAILED: "common.couldNotCompleteTheFolderImport",
    DATABASE_IMPORT_FAILED: "common.couldNotCompleteTheDatabaseImport", EDIT_FAILED: "common.couldNotCompleteMediaEditing",
    FINALIZING: "common.theJobIsFinishingTryAgainShortly", LIMIT_EXCEEDED: "common.theAllowedLimitWasExceeded",
    NOT_RETRYABLE: "common.thisJobCannotBeRetried", UPDATE_RESTARTING: "settings.restartingTheAppToApplyTheUpdate",
    UPDATE_BUSY: "common.anUpdateIsInProgress", UPDATE_NOT_READY: "common.theUpdateIsNotReadyYet",
    UPDATE_RESTART_FAILED: "common.couldNotRestartForTheUpdate", UPDATE_UNAVAILABLE: "settings.automaticUpdatesAreNotSupportedInThisEnvironment",
    UPDATE_WORK_ACTIVE: "common.finishActiveJobsBeforeApplyingTheUpdate", SERVER_START_FAILED: "common.couldNotStartTheWebServer",
};
export function translateError(error) {
    const fallbackKey = errorMessages[error?.code] || errorMessages.INTERNAL_ERROR;
    const source = error?.message || translate(fallbackKey);
    const translated = translateMessage(source);
    if (translated !== source || getLanguage() === 'ko' || !/[가-힣]/.test(source)) return translated;
    const fallback = translate(fallbackKey);
    return error?.code ? `${fallback} (${error.code})` : fallback;
}
export function formatNumber(value) { return new Intl.NumberFormat(getLocale()).format(value); }
export function formatDate(value, options) { return new Date(value).toLocaleString(getLocale(), options); }

// DOM widgets register only their own UI labels. User titles and media metadata
// never pass through a document-wide translation observer.
const bindings = new WeakMap();
function bind(node, key, render) {
    let entries = bindings.get(node);
    if (!entries) { entries = new Map(); bindings.set(node, entries); }
    entries.set(key, render); render();
}
export function bindText(node, read) {
    const text = node.ownerDocument.createTextNode('');
    node.replaceChildren(text);
    bind(node, 'text', () => { text.data = read(); });
}
export function bindAttribute(node, name, read) { bind(node, name, () => { node.setAttribute(name, read()); }); }
export function refreshTranslations(root) {
    for (const node of [root, ...root.querySelectorAll('*')]) for (const render of bindings.get(node)?.values() ?? []) render();
}
export function localizeWidget(root) { return subscribeLanguage(() => refreshTranslations(root)); }
updateDocument();
