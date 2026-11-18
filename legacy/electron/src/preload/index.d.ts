import type { ElectronAPI } from '@electron-toolkit/preload'
import type { AppApi } from '../../../../frontend/src/shared/api'

declare global {
    interface Window {
        electron: ElectronAPI
        api: AppApi
    }
}
