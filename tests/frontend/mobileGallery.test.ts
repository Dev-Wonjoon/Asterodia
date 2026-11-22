// @vitest-environment happy-dom
import html from '../../internal/webserver/web/index.html?raw'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

let intersect: (entries: { isIntersecting: boolean }[]) => void
const flush = async () => { for (let i = 0; i < 25; i++) await Promise.resolve() }
beforeEach(() => {
    vi.resetModules(); vi.useFakeTimers(); localStorage.clear()
    document.body.innerHTML = html.split('<body>')[1].split('</body>')[0]
    vi.stubGlobal('IntersectionObserver', class {
        constructor(callback: typeof intersect) { intersect = callback }
        observe() {}
        disconnect() {}
    })
})
afterEach(() => { vi.clearAllTimers(); vi.useRealTimers(); vi.unstubAllGlobals(); document.body.replaceChildren() })

async function setup() {
    const fetcher = vi.fn(async (url: string) => {
        if (url === '/api/edit/status') return { ok: true, json: async () => ({ enabled: false }) }
        const query = new URL(url, 'http://localhost').searchParams
        const offset = Number(query.get('offset')), limit = Number(query.get('limit'))
        const expanded = query.get('expanded') === '1'
        const rows = [
            ...(expanded ? Array.from({ length: 53 }, (_, i) => ({ id: 1, title: '앨범', fileCount: 53, galleryFileOffset: i, galleryFile: { id: 100 + i, title: '사진 ' + i, kind: 'image' } })) : [{ id: 1, title: '앨범', fileCount: 53 }]),
            ...Array.from({ length: 29 }, (_, i) => ({ id: i + 2, title: '다른 앨범 ' + i, fileCount: 1 }))
        ]
        return { ok: true, json: async () => ({ data: rows.slice(offset, offset + limit), offset, total: rows.length, hasNextPage: offset + limit < rows.length }) }
    })
    vi.stubGlobal('fetch', fetcher)
    await import('../../internal/webserver/web/app.js'); await flush()
    return fetcher
}
it('automatically appends the next batch and keeps expanded files in the same gallery feed', async () => {
    const fetcher = await setup()
    expect(document.querySelectorAll('.album-card')).toHaveLength(24)
    expect(document.querySelector('[aria-label="갤러리 페이지"]')).toBeNull()
    document.querySelector<HTMLButtonElement>('button[aria-label="앨범 앨범 펼치기"]')!.click(); await flush()
    expect(document.querySelectorAll('.album-card')).toHaveLength(24)
    expect(document.getElementById('count')!.textContent).toBe('총 82개 항목')
    intersect([{ isIntersecting: true }]); await flush()
    expect(document.querySelectorAll('.album-card')).toHaveLength(48)
    expect(document.querySelector('.album-card h2')!.textContent).toBe('사진 0')
    expect(fetcher.mock.calls.at(-1)![0]).toContain('offset=24')
    intersect([{ isIntersecting: true }]); await flush()
    expect(document.querySelectorAll('.album-card')).toHaveLength(72)
    expect(document.getElementById('gallery')!.textContent).toContain('다른 앨범 0')
    intersect([{ isIntersecting: true }]); await flush()
    expect(document.querySelectorAll('.album-card')).toHaveLength(82)
    expect(document.querySelector<HTMLButtonElement>('#load-more')!.hidden).toBe(true)
    document.querySelector<HTMLButtonElement>('button[aria-label="앨범 앨범 닫기"]')!.click(); await flush()
    expect(document.querySelector('.album-card h2')!.textContent).toBe('앨범')
    expect(document.querySelectorAll('.album-card')).toHaveLength(30)
})
it('preserves loaded cards on a failed append and retries without duplicating them', async () => {
    const fetcher = await setup()
    fetcher.mockRejectedValueOnce(new Error('offline'))
    intersect([{ isIntersecting: true }]); await flush()
    expect(document.querySelectorAll('.album-card')).toHaveLength(24)
    expect(document.getElementById('load-more')!.textContent).toBe('다시 시도')
    const calls = fetcher.mock.calls.length
    intersect([{ isIntersecting: true }]); await flush()
    expect(fetcher).toHaveBeenCalledTimes(calls)
    document.querySelector<HTMLButtonElement>('#load-more')!.click(); await flush()
    expect(document.querySelectorAll('.album-card')).toHaveLength(30)
    expect(fetcher.mock.calls.at(-1)![0]).toContain('offset=24')
})
