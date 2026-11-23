import { useSyncExternalStore } from 'react'
import {
    getLanguage, getLanguagePreference, getLocale, setLanguage, subscribeLanguage,
    translate, translateError, translateMessage, formatNumber, formatDate,
} from '../../../../internal/webserver/web/i18n.js'

export type Language = 'ko' | 'en'
export type LanguagePreference = Language | 'system'
export { getLanguage, getLanguagePreference, getLocale, setLanguage, translate, translateError, translateMessage, formatNumber, formatDate }

export function useLanguage() {
    const preference = useSyncExternalStore(subscribeLanguage, getLanguagePreference, () => 'system') as LanguagePreference
    const language = useSyncExternalStore(subscribeLanguage, getLanguage, () => 'ko') as Language
    return { language, preference, setLanguage }
}
