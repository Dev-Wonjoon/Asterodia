import { afterEach, expect, it } from 'vitest'
import { mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { parseVersion, readVersion, syncVersionResources, validateTag, versionResources } from '../../tools/release-version.mjs'

const repository = fileURLToPath(new URL('../../', import.meta.url)), roots: string[] = []
afterEach(() => { for (const root of roots.splice(0)) rmSync(root, { recursive: true, force: true }) })
it('accepts stable versions and rejects invalid or unrepresentable Windows versions', () => {
    expect(parseVersion('2.1.0\r\n')).toBe('2.1.0')
    for (const version of ['v2.1.0', '2.1', '02.1.0', '2.1.0-beta.1', '2.1.0\n2.2.0', '65536.1.0', '']) expect(() => parseVersion(version)).toThrow()
    expect(() => validateTag('v2.1.0', '2.1.0')).not.toThrow()
    for (const tag of ['2.1.0', 'v2.1.1', 'v2.1.0-beta']) expect(() => validateTag(tag, '2.1.0')).toThrow()
})
it('updates every platform from VERSION and detects stale metadata without changing it', () => {
    const base = path.join(repository, '.test-data', 'version-tests'); mkdirSync(base, { recursive: true })
    const root = mkdtempSync(path.join(base, 'version-')); roots.push(root)
    writeFileSync(path.join(root, 'VERSION'), readVersion(repository))
    for (const name of versionResources(repository).files.keys()) {
        mkdirSync(path.dirname(path.join(root, name)), { recursive: true })
        writeFileSync(path.join(root, name), readFileSync(path.join(repository, name)))
    }
    writeFileSync(path.join(root, 'VERSION'), '7.12.34\n')
    const oldInfo = readFileSync(path.join(root, 'build/windows/info.json'), 'utf8')
    expect(() => syncVersionResources(root, true)).toThrow('Version resources differ')
    expect(readFileSync(path.join(root, 'build/windows/info.json'), 'utf8')).toBe(oldInfo)
    expect(syncVersionResources(root)).toBe('7.12.34')
    expect(() => syncVersionResources(root, true)).not.toThrow()
    const read = (name: string) => readFileSync(path.join(root, name), 'utf8')
    expect(JSON.parse(read('build/windows/info.json')).fixed.file_version).toBe('7.12.34.0')
    expect(read('build/config.yml')).toContain('version: "7.12.34"')
    expect(read('build/windows/wails.exe.manifest')).toContain('version="7.12.34.0"')
    expect(read('build/windows/msix/app_manifest.xml')).toContain('Version="7.12.34.0"')
    expect(read('build/windows/msix/template.xml')).toContain('Version="7.12.34.0"')
    expect(read('build/windows/nsis/wails_tools.nsh')).toContain('INFO_PRODUCTVERSION "7.12.34"')
    expect(read('build/linux/nfpm/nfpm.yaml')).toContain('version: "7.12.34"')
    expect(read('build/android/app/build.gradle')).toContain('versionName "7.12.34"')
    for (const name of ['build/darwin/Info.plist', 'build/darwin/Info.dev.plist', 'build/ios/Info.plist']) expect(read(name)).toContain('<string>7.12.34</string>')
    expect(read('build/ios/Info.dev.plist')).toContain('<string>7.12.34-dev</string>')
})
