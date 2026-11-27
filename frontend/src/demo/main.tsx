import { useRef, useState, type ReactNode } from 'react'
import { createRoot } from 'react-dom/client'
import { Heart, Images, Search, Download, Sun, Moon, RotateCcw, ArrowUpRight, Play, Grid2X2, TextCursorInput } from 'lucide-react'
import { GalleryPage } from '@renderer/features/gallery/GalleryPage'
import { SearchPage } from '@renderer/features/search/SearchPage'
import { DownloadPage } from '@renderer/features/download/DownloadPage'
import { DownloadBar } from '@renderer/features/download/components/DownloadBar'
import { useDownloadViewModel } from '@renderer/features/download/useDownloadViewModel'
import { ConfirmProvider } from '@renderer/app/providers/ConfirmProvider'
import { ToastProvider } from '@renderer/app/providers/ToastProvider'
import { DownloadProvider } from '@renderer/app/providers/DownloadProvider'
import { LeaveGuardContext } from '@renderer/shared/hooks/leaveGuardContext'
import { useConfirm } from '@renderer/shared/hooks/useConfirm'
import { useTheme } from '@renderer/shared/hooks/useTheme'
import { useActionDisplay } from '@renderer/shared/hooks/useActionDisplay'
import { LanguageSelect } from '@renderer/shared/i18n/LanguageSelect'
import { useLanguage } from '@renderer/shared/i18n'
import { demo } from './api'
import { BrandMark } from '@renderer/shared/components/BrandMark'
import '@renderer/styles/index.css'
import './demo.css'

type Page = 'gallery' | 'favorites' | 'search' | 'downloads'

function DemoApp({ reset }: { reset(): void }) {
    const [page, setPage] = useState<Page>('gallery')
    const guard = useRef<(() => boolean | Promise<boolean>) | null>(null)
    const { language } = useLanguage(), ko = language === 'ko'
    const { theme, toggleTheme } = useTheme()
    const actions = useActionDisplay()
    const { confirm } = useConfirm()
    const downloads = useDownloadViewModel()
    const text = (korean: string, english: string) => ko ? korean : english
    const navigate = async (next: Page) => { if (!guard.current || await guard.current()) setPage(next) }
    const tryDownload = async () => {
        if (guard.current && !await guard.current()) return
        setPage('gallery')
        await downloads.startDownload('https://www.instagram.com/p/asterodia-demo/')
    }
    const nav: { id: Page; label: string; icon: ReactNode }[] = [
        { id: 'gallery', label: text('갤러리', 'Gallery'), icon: <Images /> },
        { id: 'favorites', label: text('즐겨찾기', 'Favorites'), icon: <Heart /> },
        { id: 'search', label: text('검색', 'Search'), icon: <Search /> },
        { id: 'downloads', label: text('다운로드', 'Downloads'), icon: <Download /> },
    ]
    return <LeaveGuardContext.Provider value={guard}><div className="demo-app">
        <header className="demo-header">
            <a className="demo-brand" href="../"><BrandMark className="size-7" /><span>Asterodia <small>PLAYGROUND</small></span></a>
            <div className="demo-tools">
                <LanguageSelect />
                <button className="demo-icon" onClick={toggleTheme} aria-label={text('테마 변경', 'Change theme')}>{theme === 'dark' ? <Sun /> : <Moon />}</button>
                <button className="demo-icon" onClick={actions.toggle} aria-label={text('버튼 표시 방식 변경', 'Change action display')}>{actions.mode === 'icons' ? <TextCursorInput /> : <Grid2X2 />}</button>
                <button className="demo-reset" aria-label={text('데모 초기화', 'Reset demo')} onClick={async () => {
                    if (await confirm({ title: text('데모 초기화', 'Reset demo'), message: text('변경한 샘플 데이터와 다운로드 진행을 초기화할까요?', 'Reset sample changes and download progress?'), confirmLabel: text('초기화', 'Reset') })) reset()
                }}><RotateCcw size={16} /><span>{text('초기화', 'Reset')}</span></button>
                <a className="demo-install" href="https://github.com/Dev-Wonjoon/Asterodia/releases/latest/download/StorageBucket-windows-amd64.exe">{text('앱 다운로드', 'Get the app')}<ArrowUpRight size={15} /></a>
            </div>
        </header>
        <section className="demo-intro" aria-label={text('데모 안내', 'About this demo')}>
            <div><p className="demo-eyebrow">{text('설치 전에, 가볍게', 'A LITTLE ROOM TO EXPLORE')}</p>
                <h1>{text('나만의 라이브러리를 만들어 보세요.', 'Make a little library of your own.')}</h1>
                <p>{text('샘플 데이터로 동작하는 체험판입니다. 실제 다운로드와 PC 파일 접근은 하지 않으며, 변경 내용은 새로고침하면 사라집니다.', 'An interactive demo with sample data. No real downloads or access to PC files. Refresh to start fresh.')}</p></div>
            <button className="demo-play" disabled={!downloads.ready || downloads.activeJobs.length > 0} onClick={tryDownload}><Play size={16} fill="currentColor" />{text('샘플 다운로드 체험', 'Try a sample download')}</button>
        </section>
        <div className="demo-workspace"><aside className="demo-sidebar"><nav aria-label={text('라이브러리', 'Library')}>
            {nav.map(item => <button key={item.id} aria-current={page === item.id ? 'page' : undefined} onClick={() => void navigate(item.id)}>{item.icon}<span>{item.label}</span>{item.id === 'downloads' && downloads.activeJobs.length > 0 && <small>{downloads.activeJobs.length}</small>}</button>)}
        </nav><p>{text('카드를 클릭해 태그와 즐겨찾기를 바꾸고, 두 번 클릭해 크게 감상해 보세요.', 'Select a card to edit tags and favorites. Double-click to open the viewer.')}</p><a href="../">← {text('소개 페이지', 'Back to the website')}</a></aside>
        <main className="demo-main" id="demo-library">{page === 'gallery' ? <GalleryPage /> : page === 'favorites' ? <GalleryPage favoritesOnly title={text('즐겨찾기', 'Favorites')} /> : page === 'search' ? <SearchPage Gallery={GalleryPage} /> : <DownloadPage />}</main></div>
        <footer className="demo-download"><DownloadBar /></footer>
    </div></LeaveGuardContext.Provider>
}

function Root() {
    const [generation, setGeneration] = useState(0)
    const reset = () => { demo.reset(); setGeneration(value => value + 1) }
    return <ConfirmProvider><ToastProvider><DownloadProvider key={generation}><DemoApp reset={reset} /></DownloadProvider></ToastProvider></ConfirmProvider>
}

window.addEventListener('pagehide', event => { if (!event.persisted) demo.dispose() })
createRoot(document.getElementById('root')!).render(<Root />)
