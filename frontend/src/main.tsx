import { ConfirmProvider } from "@renderer/app/providers/ConfirmProvider"
import '@renderer/styles/index.css'
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import App from '@renderer/app/App'
import { ToastProvider } from '@renderer/app/providers/ToastProvider'
import { DownloadProvider } from '@renderer/app/providers/DownloadProvider'

createRoot(document.getElementById('root')!).render(
    <StrictMode>
        <ConfirmProvider>
        <ToastProvider>
            <DownloadProvider>
                <App />
            </DownloadProvider>
        </ToastProvider>
        </ConfirmProvider>
    </StrictMode>
)
