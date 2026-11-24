import { translate, useLanguage } from '@renderer/shared/i18n'
import { createRoot } from 'react-dom/client'
import { useState } from 'react'
import { WebDownloadPage } from './WebDownloadPage'
import { SearchPage } from '../features/search/SearchPage'
import { WebGalleryPage } from './WebGalleryPage'
import { useTheme } from '../shared/hooks/useTheme'
import { useActionDisplay } from '../shared/hooks/useActionDisplay'
import { Sun, Moon, Grid2X2, TextCursorInput } from 'lucide-react'
import '../styles/index.css'
import './web.css'
import { LanguageSelect } from '@renderer/shared/i18n/LanguageSelect'

function WebApp() {
    const [page, setPage] = useState<'gallery' | 'downloads'>('gallery')
    const { language } = useLanguage()
    const { theme, toggleTheme } = useTheme()
    const { mode, toggle } = useActionDisplay()
    return <div className="flex h-dvh min-h-0 flex-col bg-[var(--bg-app)] text-[var(--text-main)]">
        <header className="flex shrink-0 flex-wrap items-center justify-between gap-2 border-b border-[var(--border-line)] bg-[var(--bg-sidebar)] px-4 py-3">
            <a href="/" className="min-w-0 truncate font-semibold text-[var(--color-primary)]">Storage Bucket</a>
            <nav className="flex gap-2" aria-label={translate('web.navigation')}>
                <button type="button" className="rounded-lg px-3 py-2 text-sm aria-pressed:bg-[var(--bg-hover)]" aria-pressed={page === 'gallery'} onClick={() => setPage('gallery')}>{translate('navigation.gallery')}</button>
                <button type="button" className="rounded-lg px-3 py-2 text-sm aria-pressed:bg-[var(--bg-hover)]" aria-pressed={page === 'downloads'} onClick={() => setPage('downloads')}>{translate('navigation.downloads')}</button>
            </nav>
            <div className="flex max-w-full shrink-0 flex-wrap items-center gap-2">
                <LanguageSelect />
                <button type="button" className="rounded-lg p-2 hover:bg-[var(--bg-hover)]" aria-label={translate("common.changeTheme")} onClick={toggleTheme}>{theme === 'dark' ? <Sun className="size-5" /> : <Moon className="size-5" />}</button>
                <button type="button" className="rounded-lg p-2 hover:bg-[var(--bg-hover)]" aria-label={mode === 'icons' ? translate("common.switchToLabelMode") : translate("common.switchToIconMode")} onClick={toggle}>{mode === 'icons' ? <TextCursorInput className="size-5" /> : <Grid2X2 className="size-5" />}</button>
                <a href={'/licenses?lang=' + language} className="text-xs text-[var(--text-muted)]">{translate("settings.licenses")}</a>
            </div>
        </header>
        <main className="min-h-0 flex-1">{page === 'downloads' ? <WebDownloadPage onGallery={() => setPage('gallery')} /> : <SearchPage Gallery={WebGalleryPage} />}</main>
    </div>
}
createRoot(document.getElementById('root')!).render(<WebApp />)
