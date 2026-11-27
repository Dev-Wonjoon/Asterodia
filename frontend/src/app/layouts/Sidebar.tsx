import { translateMessage, translate, useLanguage } from '@renderer/shared/i18n'
import type { AppMenu } from '../navigation'
import { useEffect, useState, type ReactNode } from 'react'
import { SidebarItem } from '@renderer/app/layouts/SidebarItem'
import { useTheme } from '@renderer/shared/hooks/useTheme'
import { BrandMark } from '@renderer/shared/components/BrandMark'
import { Download, Moon, PanelLeftClose, PanelLeftOpen, Settings, Sun, TextCursorInput, Grid2X2 } from 'lucide-react'
import { useActionDisplay } from '@renderer/shared/hooks/useActionDisplay'
import { useDownloadViewModel } from '@renderer/features/download/useDownloadViewModel'

const SIDEBAR_MENUS: Array<{ id: AppMenu; label: string; icon: ReactNode }> = [
    {
        id: 'gallery',
        get label() { return translate("navigation.gallery") },
        icon: (
            <svg
                xmlns="http://www.w3.org/2000/svg"
                fill="none"
                viewBox="0 0 24 24"
                strokeWidth="1.5"
                stroke="currentColor"
                className="size-6"
            >
                <path
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    d="M3.75 6A2.25 2.25 0 0 1 6 3.75h2.25A2.25 2.25 0 0 1 10.5 6v2.25a2.25 2.25 0 0 1-2.25 2.25H6a2.25 2.25 0 0 1-2.25-2.25V6ZM3.75 15.75A2.25 2.25 0 0 1 6 13.5h2.25a2.25 2.25 0 0 1 2.25 2.25V18a2.25 2.25 0 0 1-2.25 2.25H6A2.25 2.25 0 0 1 3.75 18v-2.25ZM13.5 6a2.25 2.25 0 0 1 2.25-2.25H18A2.25 2.25 0 0 1 20.25 6v2.25A2.25 2.25 0 0 1 18 10.5h-2.25a2.25 2.25 0 0 1-2.25-2.25V6ZM13.5 15.75a2.25 2.25 0 0 1 2.25-2.25H18a2.25 2.25 0 0 1 2.25 2.25V18A2.25 2.25 0 0 1 18 20.25h-2.25A2.25 2.25 0 0 1 13.5 18v-2.25Z"
                />
            </svg>
        )
    },
    {
        id: 'downloads',
        get label() { return translate("navigation.downloads") },
        icon: <Download className="size-6" />
    },
    {
        id: 'search',
        get label() { return translate("navigation.search") },
        icon: (
            <svg
                xmlns="http://www.w3.org/2000/svg"
                fill="none"
                viewBox="0 0 24 24"
                strokeWidth="1.5"
                stroke="currentColor"
                className="size-6"
            >
                <path
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    d="m21 21-5.197-5.197m0 0A7.5 7.5 0 1 0 5.196 5.196a7.5 7.5 0 0 0 10.607 10.607Z"
                />
            </svg>
        )
    },
    {
        id: 'favorites',
        get label() { return translate("navigation.favorites") },
        icon: (
            <svg
                xmlns="http://www.w3.org/2000/svg"
                fill="none"
                viewBox="0 0 24 24"
                strokeWidth="1.5"
                stroke="currentColor"
                className="size-6"
            >
                <path
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    d="M11.48 3.499a.562.562 0 0 1 1.04 0l2.125 5.111a.563.563 0 0 0 .475.345l5.518.442c.499.04.701.663.321.988l-4.204 3.602a.563.563 0 0 0-.182.557l1.285 5.385a.562.562 0 0 1-.84.61l-4.725-2.885a.562.562 0 0 0-.586 0L6.982 20.54a.562.562 0 0 1-.84-.61l1.285-5.386a.562.562 0 0 0-.182-.557l-4.204-3.602a.562.562 0 0 1 .321-.988l5.518-.442a.563.563 0 0 0 .475-.345L11.48 3.5Z"
                />
            </svg>
        )
    }
]

interface SidebarProps {
    activeMenu: AppMenu
    onMenuClick: (id: AppMenu) => void
    collapsed: boolean
    onToggle: () => void
}

export const Sidebar = ({
    activeMenu,
    onMenuClick,
    collapsed,
    onToggle
}: SidebarProps): React.JSX.Element => {
    useLanguage()
    const { theme, toggleTheme } = useTheme()
    const { mode, toggle: toggleActionDisplay } = useActionDisplay()
    const [modeNotice, setModeNotice] = useState('')
    useEffect(() => {
        if (!modeNotice) return
        const timer = setTimeout(() => setModeNotice(''), 1800)
        return () => clearTimeout(timer)
    }, [modeNotice])
    const { requestedIds } = useDownloadViewModel()
    const themeLabel = theme === 'dark' ? translate("common.switchToLightMode") : translate("common.switchToDarkMode")
    const toggleLabel = collapsed ? translate("common.expandSidebar") : translate("common.collapseSidebar")

    return (
        <aside
            data-keyboard-region tabIndex={-1}
            id="app-sidebar"
            className="w-full h-full flex flex-col shadow-2xl transition-colors duration-200 bg-[var(--bg-sidebar)] border-r border-[var(--border-line)]"
        >
            <div
                className={`h-20 shrink-0 flex items-center mb-2 border-b border-[var(--border-line)] ${
                    collapsed ? 'justify-center' : 'px-3 gap-2'
                }`}
            >
                {!collapsed && (
                    <div className="flex-1 min-w-0 flex items-center gap-2">
                        <BrandMark className="size-7 text-[var(--text-main)]" />

                        <h1 className="text-base font-bold whitespace-nowrap text-transparent bg-clip-text bg-gradient-to-r from-blue-400 to-emerald-400">
                            Asterodia
                        </h1>
                    </div>
                )}

                <button
                    type="button"
                    onClick={onToggle}
                    aria-label={toggleLabel}
                    aria-expanded={!collapsed}
                    aria-controls="app-sidebar"
                    title={toggleLabel}
                    className="size-9 shrink-0 flex items-center justify-center rounded-lg text-[var(--text-muted)] transition-colors hover:bg-[var(--bg-hover)] hover:text-[var(--text-main)]"
                >
                    {collapsed ? (
                        <PanelLeftOpen className="size-5" aria-hidden="true" />
                    ) : (
                        <PanelLeftClose className="size-5" aria-hidden="true" />
                    )}
                </button>
            </div>

            <nav
                aria-label={translate("common.mainNavigation")}
                className="flex-1 min-h-0 px-3 py-4 flex flex-col gap-1 overflow-y-auto"
            >
                {SIDEBAR_MENUS.map((menu) => (
                    <SidebarItem
                        key={menu.id}
                        label={
                            menu.id === 'downloads' && requestedIds.length > 0
                                ? translate("common.downloadsRequested", { count: requestedIds.length })
                                : menu.label
                        }
                        icon={menu.icon}
                        isActive={activeMenu === menu.id}
                        collapsed={collapsed}
                        onClick={() => onMenuClick(menu.id)}
                    />
                ))}
            </nav>

            <div
                className={`p-3 border-t border-[var(--border-line)] flex gap-2 ${
                    collapsed ? 'flex-col items-center' : 'items-center'
                }`}
            >
                <div className={collapsed ? 'w-full' : 'flex-1 min-w-0'}>
                    <SidebarItem
                        label={translate("navigation.settings")}
                        icon={<Settings className="size-6" />}
                        isActive={activeMenu === 'settings'}
                        collapsed={collapsed}
                        onClick={() => onMenuClick('settings')}
                    />
                </div>

                <div className={'relative flex shrink-0 items-center ' + (collapsed ? 'gap-0' : 'gap-1')}>
                    {modeNotice && <p role="status" className="pointer-events-none absolute bottom-full left-0 z-50 mb-2 w-64 max-w-[80vw] rounded-lg border border-[var(--border-line)] bg-[var(--bg-sidebar)] px-3 py-2 text-xs shadow-lg">{translateMessage(modeNotice)}</p>}
                    <button
                        type="button"
                        onClick={toggleTheme}
                        aria-label={themeLabel}
                        title={themeLabel}
                        className={(collapsed ? 'size-7' : 'size-9') + ' shrink-0 flex items-center justify-center rounded-lg text-[var(--text-muted)] hover:bg-[var(--bg-hover)] hover:text-[var(--text-main)]'}
                    >
                        {theme === 'dark' ? (
                            <Sun className="size-5" aria-hidden="true" />
                        ) : (
                            <Moon className="size-5" aria-hidden="true" />
                        )}
                    </button>
                    <button
                        type="button"
                        onClick={() => { setModeNotice(mode === 'icons' ? translate("common.labelModeHoverOverOrFocusAButtonTo") : translate("common.iconMode")); toggleActionDisplay() }}
                        aria-label={translate("common.buttonLabelMode")}
                        aria-pressed={mode === 'details'}
                        title={mode === 'icons' ? translate("common.iconModeSwitchToLabelMode") : translate("common.labelModeSwitchToIconMode")}
                        className={(collapsed ? 'size-7' : 'size-9') + ' shrink-0 flex items-center justify-center rounded-lg text-[var(--text-muted)] hover:bg-[var(--bg-hover)] hover:text-[var(--text-main)]'}
                    >
                        {mode === 'icons' ? <TextCursorInput className="size-5" aria-hidden="true" /> : <Grid2X2 className="size-5" aria-hidden="true" />}
                    </button>
                </div>
            </div>
        </aside>
    )
}
