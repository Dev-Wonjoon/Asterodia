// @vitest-environment happy-dom
import { act, createElement as h } from 'react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { DownloadBar } from '@renderer/features/download/components/DownloadBar'
import { useGlobalPaste } from '@renderer/app/hooks/useGlobalPaste'
import { setLanguage } from '@renderer/shared/i18n'
import { cleanupReact, deferred, mount } from './reactHarness'

const vm = vi.hoisted(() => ({ startDownload: vi.fn(), previewEngine: vi.fn(), ready: true, halted: null as string | null }))
vi.mock('@renderer/features/download/useDownloadViewModel', () => ({ useDownloadViewModel: () => vm }))

beforeEach(() => {
    vi.useFakeTimers(); vi.resetAllMocks(); setLanguage('ko')
    vm.ready = true; vm.halted = null
    vm.startDownload.mockResolvedValue(true); vm.previewEngine.mockResolvedValue('yt-dlp')
})
afterEach(async () => { await cleanupReact(); vi.useRealTimers() })

async function paste(target: EventTarget, text: string) {
    const event = new Event('paste', { bubbles: true, cancelable: true })
    Object.defineProperty(event, 'clipboardData', { value: { getData: () => text } })
    await act(async () => { target.dispatchEvent(event) })
}
async function select(label: string, value: string) {
    const element = document.querySelector<HTMLSelectElement>(`[aria-label="${label}"]`)!
    await act(async () => { element.value = value; element.dispatchEvent(new Event('change', { bubbles: true })) })
}
async function submit() {
    await act(async () => { document.querySelector<HTMLButtonElement>('button[type=submit]')!.click() })
}

it('pastes into the input without starting a download and submits the selected MP3 options', async () => {
    const globalDownload = vi.fn().mockResolvedValue(true)
    function Harness() { useGlobalPaste(globalDownload); return h(DownloadBar) }
    await mount(h(Harness))
    const input = document.querySelector<HTMLInputElement>('input[data-download-url]')!
    await paste(input, '  https://example.com/video  ')
    await act(async () => { await vi.advanceTimersByTimeAsync(250) })
    expect(input.value).toBe('https://example.com/video')
    expect(vm.startDownload).not.toHaveBeenCalled(); expect(globalDownload).not.toHaveBeenCalled()
    await select('다운로드 종류', 'audio')
    expect(document.querySelector('[aria-label="다운로드 화질"]')).toBeNull()
    await submit()
    expect(vm.startDownload).toHaveBeenCalledExactlyOnceWith('https://example.com/video', { type: 'audio', quality: 'audio', extension: 'mp3', playlist: false })
    expect(input.value).toBe('')
    await paste(document.body, 'https://example.com/global')
    expect(globalDownload).toHaveBeenCalledExactlyOnceWith('https://example.com/global')
})

it('keeps video quality choices available and sends the chosen quality and container', async () => {
    await mount(h(DownloadBar))
    const quality = document.querySelector<HTMLSelectElement>('[aria-label="다운로드 화질"]')!
    expect([...quality.options].map(option => option.value)).toEqual(['best', '8k', '4k', '2k', '1080', '720', '480'])
    await paste(document.querySelector('input[data-download-url]')!, 'https://example.com/video')
    await select('다운로드 화질', '1080'); await select('다운로드 형식', 'mkv')
    await submit()
    expect(vm.startDownload).toHaveBeenCalledExactlyOnceWith('https://example.com/video', { type: 'video', quality: '1080', extension: 'mkv', playlist: false })
    await select('다운로드 종류', 'audio'); await select('다운로드 종류', 'video')
    expect(document.querySelector<HTMLSelectElement>('[aria-label="다운로드 화질"]')!.value).toBe('best')
})

it('closes options on an outside pointer, window blur, and Escape, while allowing option controls', async () => {
    await mount(h(DownloadBar))
    const details = document.querySelector('details')!
    const quality = document.querySelector('[aria-label="다운로드 화질"]')!
    details.open = true
    await act(async () => { quality.dispatchEvent(new Event('pointerdown', { bubbles: true })) })
    expect(details.open).toBe(true)
    await select('다운로드 화질', '720')
    expect(details.open).toBe(true)
    await act(async () => { document.body.dispatchEvent(new Event('pointerdown', { bubbles: true })) })
    expect(details.open).toBe(false)
    details.open = true
    await act(async () => { window.dispatchEvent(new Event('blur')) })
    expect(details.open).toBe(false)
    details.open = true
    await act(async () => { quality.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })) })
    expect(details.open).toBe(false); expect(document.activeElement).toBe(details.querySelector('summary'))
})

it('prevents duplicate submissions and retains input when the request fails', async () => {
    const pending = deferred<boolean>(); vm.startDownload.mockReturnValueOnce(pending.promise)
    await mount(h(DownloadBar))
    const input = document.querySelector<HTMLInputElement>('input[data-download-url]')!
    await paste(input, 'https://example.com/video'); await submit()
    await act(async () => { document.querySelector('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })) })
    expect(vm.startDownload).toHaveBeenCalledTimes(1); expect(input.disabled).toBe(true)
    await act(async () => { pending.resolve(false) })
    expect(input.value).toBe('https://example.com/video'); expect(input.disabled).toBe(false)
    await submit(); expect(input.value).toBe('')
})
