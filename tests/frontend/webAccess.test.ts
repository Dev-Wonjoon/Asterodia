// @vitest-environment happy-dom
import { act, createElement as h, useEffect } from 'react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { WebAccessGate } from '@renderer/web/WebAccessGate'
import { cleanupReact, clickButton, deferred, mount } from './reactHarness'

const fetchMock = vi.fn()
const response = (authenticated: boolean, status = 200) => ({ ok: status === 200, status, json: async () => ({ authenticated }) })
beforeEach(() => { vi.stubGlobal('fetch', fetchMock); fetchMock.mockReset(); localStorage.setItem('storagebucket:language:v1', 'ko') })
afterEach(async () => { await cleanupReact(); vi.unstubAllGlobals(); vi.useRealTimers() })
const inputCode = async (value: string) => {
    const node = document.querySelector('input')!
    await act(async () => { Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(node, value); node.dispatchEvent(new Event('input', { bubbles: true })) })
}

it('keeps private children unmounted until the device connects', async () => {
    const mounted = vi.fn()
    function PrivatePage() { useEffect(mounted, []); return h('p', null, 'Private library') }
    fetchMock.mockResolvedValueOnce(response(false)).mockResolvedValueOnce(response(true))
    await mount(h(WebAccessGate, { children: h(PrivatePage) }))
    expect(mounted).not.toHaveBeenCalled()
    await inputCode('12345678')
    await clickButton('연결')
    expect(fetchMock).toHaveBeenLastCalledWith('/api/access/connect', expect.objectContaining({ method: 'POST', credentials: 'same-origin', body: '{"code":"12345678"}' }))
    expect(mounted).toHaveBeenCalledTimes(1)
    expect(document.body.textContent).toContain('Private library')
})

it('stays locked after a rejected code and reports rate limiting', async () => {
    fetchMock.mockResolvedValueOnce(response(false)).mockResolvedValueOnce(response(false, 429))
    await mount(h(WebAccessGate, { children: h('p', null, 'Private library') }))
    await inputCode('12345678'); await clickButton('연결')
    expect(document.querySelector('[role="alert"]')?.textContent).toContain('요청이 많습니다')
    expect(document.body.textContent).not.toContain('Private library')
})

it('locks immediately on session expiry and logs out through the server', async () => {
    fetchMock.mockResolvedValue(response(true))
    await mount(h(WebAccessGate, { children: h('p', null, 'Private library') }))
    await act(async () => { window.dispatchEvent(new Event('storagebucket:disconnect')) })
    expect(fetchMock).toHaveBeenLastCalledWith('/api/access/logout', expect.objectContaining({ method: 'POST', credentials: 'same-origin' }))
    expect(document.body.textContent).not.toContain('Private library')
})

it('does not restore a revoked session from an older status response', async () => {
    vi.useFakeTimers()
    const pending = deferred<ReturnType<typeof response>>()
    fetchMock.mockResolvedValueOnce(response(true)).mockReturnValueOnce(pending.promise)
    await mount(h(WebAccessGate, { children: h('p', null, 'Private library') }))
    await act(async () => { vi.advanceTimersByTime(60_000) })
    await act(async () => { window.dispatchEvent(new Event('storagebucket:access-expired')) })
    await act(async () => { pending.resolve(response(true)) })
    expect(document.body.textContent).not.toContain('Private library')
})

it('reports a failed logout without falsely claiming disconnection', async () => {
    fetchMock.mockResolvedValueOnce(response(true)).mockResolvedValueOnce(response(false, 503))
    await mount(h(WebAccessGate, { children: h('p', null, 'Private library') }))
    await act(async () => { window.dispatchEvent(new Event('storagebucket:disconnect')) })
    expect(document.body.textContent).toContain('Private library')
    expect(document.querySelector('[role="alert"]')?.textContent).toContain('서버에 연결하지 못했습니다')
})
