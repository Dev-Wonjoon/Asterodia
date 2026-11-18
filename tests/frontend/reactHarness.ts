import { act, type ReactNode } from 'react'
import { createRoot, type Root } from 'react-dom/client'

const mounted = new Map<Root, HTMLElement>()
export async function mount(element: ReactNode) {
    Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
    const host = document.createElement('div')
    document.body.append(host)
    const root = createRoot(host)
    mounted.set(root, host)
    await act(async () => { root.render(element) })
    return async () => {
        await act(async () => { root.unmount() })
        host.remove(); mounted.delete(root)
    }
}
export async function cleanupReact() {
    for (const [root, host] of mounted) { await act(async () => { root.unmount() }); host.remove() }
    mounted.clear()
}
export async function clickButton(text: string) {
    const button = [...document.querySelectorAll('button')].find(button => button.textContent === text || button.getAttribute('aria-label') === text)
    if (!button || button.disabled) throw new Error(`사용 가능한 버튼을 찾지 못했습니다: ${text}`)
    await act(async () => { button.click() })
}
export function deferred<T>() {
    let resolve!: (value: T) => void
    let reject!: (error: unknown) => void
    const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
    return { promise, resolve, reject }
}
