import { useConfirm } from '@renderer/shared/hooks/useConfirm'
import { useEffect, useRef, useState } from 'react'
import { Sidebar } from './layouts/Sidebar'
import { DownloadBar } from '@renderer/features/download/components/DownloadBar'
import { GalleryPage } from '@renderer/features/gallery/GalleryPage'
import { FavoritesPage } from '@renderer/features/favorites/FavoritesPage'
import { SearchPage } from '@renderer/features/search/SearchPage'
import { SettingsPage } from '@renderer/features/settings/SettingsPage'
import { EngineSetupModal } from '@renderer/features/settings/components/EngineSetupModal'
import { useDownloadViewModel } from '@renderer/features/download/useDownloadViewModel'
import { useEngineSetup } from './hooks/useEngineSetup'
import { useGlobalPaste } from './hooks/useGlobalPaste'
import type { AppMenu } from './navigation'
import { DownloadPage } from '@renderer/features/download/DownloadPage'
import { LeaveGuardProvider } from './providers/LeaveGuardProvider'
import { AppUpdateProvider } from '@renderer/features/settings/useAppUpdate'
import { UpdateNotice } from '@renderer/features/settings/UpdateSettings'
import { useShortcutCommands } from '@renderer/shared/hooks/useShortcutCommands'
import { KeyboardDialog } from './KeyboardDialog'
import { commandEnabled, runCommand } from '../../../internal/webserver/web/shortcuts.js'

export default function App(): React.JSX.Element {
    const [activeMenu, setActiveMenu] = useState<AppMenu>('gallery')
    const [sidebarCollapsed, setSidebarCollapsed] = useState(false)
    const [updateSettingsRequest, setUpdateSettingsRequest] = useState(0)
    const [importRequest, setImportRequest] = useState(0)
    const importSequence = useRef(0)
    const [focusRequest, setFocusRequest] = useState<{ target: 'search' | 'url'; serial: number } | null>(null)
    const [keyboardDialog, setKeyboardDialog] = useState<'commands' | 'help' | null>(null)
    const leaveGuard = useRef<(() => boolean | Promise<boolean>) | null>(null)
    const { startDownload } = useDownloadViewModel()
    const { isOpen: confirming } = useConfirm()
    const navigating = useRef(false)
    const alive = useRef(true)
    useEffect(() => { alive.current = true; return () => { alive.current = false } }, [])
    const { showSetup, checkingSetup, closeSetup } = useEngineSetup()
    useGlobalPaste(async url => !confirming && await startDownload(url))

    async function navigate(menu: AppMenu): Promise<boolean> {
        if (navigating.current || confirming || showSetup) return false
        if (menu === activeMenu) return true
        navigating.current = true
        try {
            if (leaveGuard.current && !await leaveGuard.current()) return false
            if (!alive.current) return false
            setActiveMenu(menu); return true
        } finally { navigating.current = false }
    }
    useEffect(() => {
        if (!focusRequest) return
        document.querySelector<HTMLInputElement>(focusRequest.target === 'search' ? '[data-search-input]' : '[data-download-url]')?.focus()
    }, [focusRequest, activeMenu])
    const canNavigate = () => !confirming && !showSetup && !checkingSetup && !navigating.current
    function region(direction: number) {
        const regions = [...document.querySelectorAll<HTMLElement>('[data-keyboard-region]')].filter(node => !node.closest('[inert]'))
        const index = regions.findIndex(node => node.contains(document.activeElement))
        const next = index < 0 ? (direction > 0 ? 0 : regions.length - 1) : (index + direction + regions.length) % regions.length
        const target = regions[next]
        const control = target?.querySelector<HTMLElement>('[data-card-focus][tabindex="0"]')
            ?? target?.querySelector<HTMLElement>('button:not(:disabled),input:not(:disabled),[tabindex="0"]')
        ;(control ?? target)?.focus()
    }
    useShortcutCommands('app', {
        ...Object.fromEntries((['gallery', 'downloads', 'search', 'favorites', 'settings'] as AppMenu[]).map(menu => ['app.' + menu, { enabled: canNavigate, run: () => { void navigate(menu) } }])),
        'app.find': { enabled: canNavigate, run: () => {
            const input = document.querySelector<HTMLInputElement>('[data-search-input]')
            if (input) input.focus()
            else void navigate('search').then(ok => { if (ok && alive.current) setFocusRequest(value => ({ target: 'search', serial: (value?.serial ?? 0) + 1 })) })
        } },
        'app.url': { enabled: canNavigate, run: () => { void navigate('downloads').then(ok => { if (ok && alive.current) setFocusRequest(value => ({ target: 'url', serial: (value?.serial ?? 0) + 1 })) }) } },
        'app.import': { enabled: () => canNavigate() && (activeMenu !== 'gallery' || commandEnabled('gallery.import')), run: () => {
            if (activeMenu === 'gallery') return runCommand('gallery.import')
            void navigate('gallery').then(ok => { if (ok && alive.current) setImportRequest(++importSequence.current) })
        } },
        'app.commands': { enabled: canNavigate, run: () => setKeyboardDialog('commands') },
        'app.help': { enabled: canNavigate, run: () => setKeyboardDialog('help') },
        'app.nextRegion': { enabled: canNavigate, run: () => region(1) },
        'app.previousRegion': { enabled: canNavigate, run: () => region(-1) },
    })

    const pages: Record<AppMenu, React.JSX.Element> = {
        gallery: <GalleryPage importRequest={importRequest} onImportHandled={() => setImportRequest(0)} />,
        downloads: <DownloadPage />,
        search: <SearchPage Gallery={GalleryPage} />,
        favorites: <FavoritesPage />,
        settings: (
            <div data-keyboard-region tabIndex={-1} className="h-full overflow-y-auto p-6">
                <SettingsPage updateRequest={updateSettingsRequest} />
            </div>
        )
    }

    return (
        <AppUpdateProvider blocked={checkingSetup || showSetup} leaveGuard={leaveGuard}><div className="flex h-screen w-full bg-[var(--bg-app)] text-[var(--text-main)] overflow-hidden font-sans transition-colors duration-200">
            <div
                className={`motion-sidebar h-full shrink-0 ${
                    sidebarCollapsed ? 'w-20' : 'w-64'
                }`}
            >
                <Sidebar
                    activeMenu={activeMenu}
                    onMenuClick={menu => { void navigate(menu) }}
                    collapsed={sidebarCollapsed}
                    onToggle={() => setSidebarCollapsed((previous) => !previous)}
                />
            </div>
            <div className="flex-1 flex flex-col h-full min-w-0 relative">
                <div className="flex-none p-4 h-20 border-b border-[var(--border-line)] bg-[var(--bg-app)]/95 backdrop-blur-sm z-10">
                    <DownloadBar />
                </div>
                <UpdateNotice onOpen={() => {
                    if (navigating.current || confirming) return
                    navigating.current = true
                    void (async () => {
                        try {
                            if (!leaveGuard.current || await leaveGuard.current()) {
                                if (alive.current) { setUpdateSettingsRequest(value => value + 1); setActiveMenu('settings') }
                            }
                        }
                        finally { navigating.current = false }
                    })()
                }} />
                <main className="min-h-0 flex-1 overflow-hidden">
                    <div key={activeMenu} className="motion-page h-full min-h-0">
                        <LeaveGuardProvider guard={leaveGuard}>{pages[activeMenu]}</LeaveGuardProvider>
                    </div>
                </main>
            </div>
            {showSetup && <EngineSetupModal onComplete={closeSetup} />}
            {keyboardDialog && <KeyboardDialog mode={keyboardDialog} onClose={() => setKeyboardDialog(null)} />}
        </div></AppUpdateProvider>
    )
}
