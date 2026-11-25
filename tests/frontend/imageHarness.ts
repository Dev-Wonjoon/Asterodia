import { vi } from 'vitest'

// happy-dom does not fetch/decode images. Keep replacement loads controllable.
export function mockImageLoading() {
    const requests: PendingImage[] = []
    class PendingImage {
        src = ''
        onload: (() => Promise<void>) | null = null
        onerror: (() => void) | null = null
        decode = vi.fn().mockResolvedValue(undefined)
        constructor() { requests.push(this) }
    }
    vi.stubGlobal('Image', PendingImage)
    return requests
}
