import { describe, expect, it } from 'vitest'
import { readPreference } from '../../internal/webserver/web/preferences.js'

function storage(values: Record<string, string>, failWrites = false) {
    return { getItem: (key: string) => values[key] ?? null, setItem: (key: string, value: string) => {
        if (failWrites) throw new Error('Storage full')
        values[key] = value
    } }
}
describe('preference migration into Asterodia keys', () => {
    it('copies language, album, player and editor settings and preserves original recovery values', () => {
        for (const suffix of [':language:v1', ':album-view:12', ':player-audio:v1', '-quality:webp', '-pages-theme']) {
            const old = `storagebucket${suffix}`, next = `asterodia${suffix}`
            const values = { [old]: 'saved value' }
            expect(readPreference(next, storage(values))).toBe('saved value')
            expect(values).toEqual({ [old]: 'saved value', [next]: 'saved value' })
        }
    })
    it('keeps an existing new value and migrates the old generic theme key', () => {
        const values = { theme: 'dark', 'storagebucket:language:v1': 'ko', 'asterodia:language:v1': 'en' }
        expect(readPreference('asterodia:language:v1', storage(values))).toBe('en')
        expect(readPreference('asterodia:theme:v1', storage(values))).toBe('dark')
        expect(values['asterodia:theme:v1']).toBe('dark')
    })
    it('retains readable legacy settings when writes are blocked, without throwing on unavailable storage', () => {
        const values = { 'storagebucket:language:v1': 'ko' }
        expect(readPreference('asterodia:language:v1', storage(values, true))).toBe('ko')
        expect(values['asterodia:language:v1']).toBeUndefined()
        expect(readPreference('asterodia:language:v1', { getItem: () => { throw new Error('Blocked') } })).toBeNull()
    })
})
