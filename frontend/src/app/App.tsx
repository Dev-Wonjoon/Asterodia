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

export default function App(): React.JSX.Element {
    const [activeMenu, setActiveMenu] = useState<AppMenu>('gallery')
    const [sidebarCollapsed, setSidebarCollapsed] = useState(false)
    const [updateSettingsRequest, setUpdateSettingsRequest] = useState(0)
    const leaveGuard = useRef<(() => boolean | Promise<boolean>) | null>(null)
    const { startDownload } = useDownloadViewModel()
    const { isOpen: confirming } = useConfirm()
    const navigating = useRef(false)
    const alive = useRef(true)
    useEffect(() => { alive.current = true; return () => { alive.current = false } }, [])
    const { showSetup, closeSetup } = useEngineSetup()
    useGlobalPaste(async url => !confirming && await startDownload(url))

    const pages: Record<AppMenu, React.JSX.Element> = {
        gallery: <GalleryPage />,
        downloads: <DownloadPage />,
        search: <SearchPage Gallery={GalleryPage} />,
        favorites: <FavoritesPage />,
        settings: (
            <div className="h-full overflow-y-auto p-6">
                <SettingsPage updateRequest={updateSettingsRequest} />
            </div>
        )
    }

    return (
        <AppUpdateProvider><div className="flex h-screen w-full bg-[var(--bg-app)] text-[var(--text-main)] overflow-hidden font-sans transition-colors duration-200">
            <div
                className={`motion-sidebar h-full shrink-0 ${
                    sidebarCollapsed ? 'w-20' : 'w-64'
                }`}
            >
                <Sidebar
                    activeMenu={activeMenu}
                    onMenuClick={async (menu) => {
                        if (menu === activeMenu || navigating.current || confirming) return
                        navigating.current = true
                        try {
                            if (leaveGuard.current && !await leaveGuard.current()) return
                            if (alive.current) setActiveMenu(menu)
                        } finally { navigating.current = false }
                    }}
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
        </div></AppUpdateProvider>
    )
}
