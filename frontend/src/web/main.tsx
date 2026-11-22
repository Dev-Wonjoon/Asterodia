import { createRoot } from 'react-dom/client'
import { SearchPage } from '../features/search/SearchPage'
import { WebGalleryPage } from './WebGalleryPage'
import { useTheme } from '../shared/hooks/useTheme'
import { useActionDisplay } from '../shared/hooks/useActionDisplay'
import { Sun, Moon, Grid2X2, TextCursorInput } from 'lucide-react'
import '../styles/index.css'
import './web.css'

function WebApp() {
    const { theme, toggleTheme } = useTheme()
    const { mode, toggle } = useActionDisplay()
    return <div className="flex h-dvh min-h-0 flex-col bg-[var(--bg-app)] text-[var(--text-main)]">
        <header className="flex shrink-0 items-center justify-between gap-2 border-b border-[var(--border-line)] bg-[var(--bg-sidebar)] px-4 py-3">
            <a href="/" className="min-w-0 truncate font-semibold text-[var(--color-primary)]">Storage Bucket</a>
            <div className="flex shrink-0 items-center gap-2">
                <button type="button" className="rounded-lg p-2 hover:bg-[var(--bg-hover)]" aria-label="테마 변경" onClick={toggleTheme}>{theme === 'dark' ? <Sun className="size-5" /> : <Moon className="size-5" />}</button>
                <button type="button" className="rounded-lg p-2 hover:bg-[var(--bg-hover)]" aria-label={mode === 'icons' ? '설명 모드로 전환' : '아이콘 모드로 전환'} onClick={toggle}>{mode === 'icons' ? <TextCursorInput className="size-5" /> : <Grid2X2 className="size-5" />}</button>
                <a href="/licenses" className="text-xs text-[var(--text-muted)]">라이선스</a>
            </div>
        </header>
        <main className="min-h-0 flex-1"><SearchPage Gallery={WebGalleryPage} /></main>
    </div>
}
createRoot(document.getElementById('root')!).render(<WebApp />)
