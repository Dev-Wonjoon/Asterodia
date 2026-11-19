import { afterEach, describe, expect, it, vi } from 'vitest'
import { confirmDiscard } from '@renderer/shared/utils/leaveGuard'

afterEach(() => vi.unstubAllGlobals())

describe('unsaved detail guard', () => {
    it('초안이 없으면 확인하지 않으며 거부·수락 결과를 보존한다', async () => {
        const confirm = vi.fn().mockReturnValueOnce(false).mockReturnValueOnce(true)
        expect(confirmDiscard(false, confirm)).toBe(true)
        expect(confirm).not.toHaveBeenCalled()
        expect(await confirmDiscard(true, confirm)).toBe(false)
        expect(await confirmDiscard(true, confirm)).toBe(true)
        expect(confirm.mock.calls[0][0].message).toContain('태그')
    })
})
