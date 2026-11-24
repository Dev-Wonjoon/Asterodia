import { translate, useLanguage, type LanguagePreference } from './index'

export function LanguageSelect() {
    const { preference, setLanguage } = useLanguage()
    return <select
        aria-label={translate("settings.language")}
        value={preference}
        onChange={event => setLanguage(event.target.value as LanguagePreference)}
        className="max-w-[10rem] rounded-lg border border-[var(--border-line)] bg-[var(--bg-app)] px-2 py-2 text-sm text-[var(--text-main)]"
    >
        <option value="system">{translate("common.systemLanguage")}</option>
        <option value="ko">한국어</option>
        <option value="en">English</option>
    </select>
}
