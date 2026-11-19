import { createContext } from 'react'
export interface ConfirmOptions { message: string; title?: string; confirmLabel?: string; cancelLabel?: string; danger?: boolean }
export type Confirm = (options: ConfirmOptions | string) => Promise<boolean>
export const ConfirmContext = createContext<{ confirm: Confirm; isOpen: boolean }>({ confirm: async () => false, isOpen: false })
