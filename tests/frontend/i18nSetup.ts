// Existing UI assertions use Korean independently of the test runner's locale.
// Tests exercising detection or English explicitly choose their own preference.
if (globalThis.localStorage) localStorage.setItem('storagebucket:language:v1', 'ko')
if (globalThis.navigator) {
    Object.defineProperty(navigator, 'language', { configurable: true, value: 'ko-KR' })
    Object.defineProperty(navigator, 'languages', { configurable: true, value: ['ko-KR'] })
}
