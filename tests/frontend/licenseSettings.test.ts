// @vitest-environment happy-dom
import { act, createElement as h } from 'react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { LicenseSettings } from '@renderer/features/settings/LicenseSettings'
import { cleanupReact, clickButton, deferred, mount } from './reactHarness'

const api = vi.hoisted(() => ({ getEngineLicenses: vi.fn() }))
vi.mock('@renderer/shared/api', () => ({ getApi: () => api }))
const entries = ['bundled', 'external', 'build'].map((scope, index) => ({
    id: `notice-${index}`, name: `component-${index}`, version: '1.0', license: 'MIT', scope,
    url: 'https://example.com', sourceURL: 'https://example.com/source', notice: 'copyright notice',
    text: '<script>untrusted text</script>\nCopyright original author\nPermission is hereby granted'
}))
beforeEach(() => { vi.resetAllMocks(); api.getEngineLicenses.mockResolvedValue(entries) })
afterEach(cleanupReact)

it('shows each scope and renders complete offline text without interpreting markup', async () => {
    await mount(h(LicenseSettings))
    expect(document.querySelectorAll('section')).toHaveLength(3)
    const button = document.querySelector('button')!
    await act(async () => button.click())
    expect(button.getAttribute('aria-expanded')).toBe('true')
    expect(document.querySelector('pre')?.textContent).toBe(entries[0].text)
    expect(document.querySelector('script')).toBeNull()
    expect(document.querySelector('pre a')).toBeNull()
    await act(async () => button.click())
    expect(document.querySelector('pre')).toBeNull()
    const input = document.querySelector('input')!
    await act(async () => {
        Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, 'component-1')
        input.dispatchEvent(new Event('input', { bubbles: true }))
    })
    expect(document.querySelectorAll('section')).toHaveLength(1)
    expect(document.body.textContent).toContain('component-1')
    expect(document.body.textContent).not.toContain('component-0')
})

it('reports loading and failure and allows retrying the embedded catalog', async () => {
    const pending = deferred<typeof entries>(); api.getEngineLicenses.mockReturnValueOnce(pending.promise)
    await mount(h(LicenseSettings))
    expect(document.querySelector('[role=status]')).not.toBeNull()
    await act(async () => pending.reject(new Error('binding failed')))
    expect(document.querySelector('[role=alert]')).not.toBeNull()
    await clickButton('다시 시도')
    expect(document.querySelector('[role=alert]')).toBeNull()
    expect(document.querySelectorAll('section')).toHaveLength(3)
})

it('ignores a request that finishes after the settings screen has unmounted', async () => {
    const pending = deferred<typeof entries>(); api.getEngineLicenses.mockReturnValueOnce(pending.promise)
    const unmount = await mount(h(LicenseSettings)); await unmount()
    await act(async () => pending.resolve(entries))
    expect(document.querySelector('section')).toBeNull()
})
