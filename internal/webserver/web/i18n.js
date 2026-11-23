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
    for (const text of new Set([key, catalogs.en[key]])) {
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
    const key = messageKeys.get(message);
    if (key) return translate(key);
    for (const { key, names, pattern } of messagePatterns) {
        const match = pattern.exec(message);
        if (match) return translate(key, Object.fromEntries(names.map((name, i) => [name, match[i + 1]])));
    }
    return message;
}
const errorMessages = {
    INVALID_ARGUMENT: '요청 값을 확인하세요.', NOT_FOUND: '요청한 항목을 찾을 수 없습니다.',
    NOT_READY: '아직 사용할 준비가 되지 않았습니다.', REVISION_CONFLICT: '정보가 변경되었습니다. 새로고침 후 다시 확인하세요.',
    INTERNAL_ERROR: '요청을 처리하지 못했습니다.', DATABASE_ERROR: '데이터베이스 작업을 처리하지 못했습니다.',
    INVALID_LOCATION: '파일 위치를 확인하세요.', JOB_ACTIVE: '이미 처리 중인 작업이 있습니다.',
    FILE_MOVE_FAILED: '파일 복사·이동을 완료하지 못했습니다.', LOCAL_IMPORT_FAILED: '폴더 가져오기를 완료하지 못했습니다.',
    DATABASE_IMPORT_FAILED: '데이터베이스 가져오기를 완료하지 못했습니다.', EDIT_FAILED: '미디어 편집을 완료하지 못했습니다.',
    FINALIZING: '작업을 마무리하고 있습니다. 잠시 후 다시 시도하세요.', LIMIT_EXCEEDED: '허용된 한도를 초과했습니다.',
    NOT_RETRYABLE: '이 작업은 다시 시도할 수 없습니다.', UPDATE_RESTARTING: '업데이트를 적용하기 위해 앱을 다시 시작합니다…',
    UPDATE_BUSY: '업데이트 작업이 진행 중입니다.', UPDATE_NOT_READY: '업데이트 준비가 끝나지 않았습니다.',
    UPDATE_RESTART_FAILED: '업데이트 재시작에 실패했습니다.', UPDATE_UNAVAILABLE: '현재 실행 환경에서는 자동 업데이트를 지원하지 않습니다.',
    UPDATE_WORK_ACTIVE: '진행 중인 작업을 마친 뒤 업데이트를 적용하세요.', SERVER_START_FAILED: '웹 서버를 시작하지 못했습니다.',
};
export function translateError(error) {
    const source = error?.message || errorMessages[error?.code] || '요청을 처리하지 못했습니다.';
    const translated = translateMessage(source);
    if (translated !== source || getLanguage() === 'ko' || !/[가-힣]/.test(source)) return translated;
    const fallback = translate(errorMessages[error?.code] || '요청을 처리하지 못했습니다.');
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
