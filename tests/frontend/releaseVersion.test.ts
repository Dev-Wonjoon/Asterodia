import { afterEach, expect, it } from 'vitest'
import { mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { parseVersion, readVersion, syncVersionResources, validateTag, versionResources, windowsVersion } from '../../tools/release-version.mjs'

const repository = fileURLToPath(new URL('../../', import.meta.url)), roots: string[] = []
afterEach(() => { for (const root of roots.splice(0)) rmSync(root, { recursive: true, force: true }) })
it('accepts stable versions and rejects invalid or unrepresentable Windows versions', () => {
    expect(parseVersion('2.1.0\r\n')).toBe('2.1.0')
    expect(parseVersion('2.2.6a')).toBe('2.2.6a')
    expect(parseVersion('2.2.6.1')).toBe('2.2.6.1')
    for (const version of ['v2.1.0', '2.1', '02.1.0', '2.1.0-beta.1', '2.1.0\n2.2.0', '65536.1.0', '2.2.6.0', '2.2.6.01', '2.2.6.2428', '2.2.6aa', '2.2.6A', '2.2.6.1a', '2.2.6+build.1', '']) expect(() => parseVersion(version)).toThrow()
    expect(() => validateTag('v2.1.0', '2.1.0')).not.toThrow()
    for (const tag of ['2.1.0', 'v2.1.1', 'v2.1.0-beta']) expect(() => validateTag(tag, '2.1.0')).toThrow()
})
it.each(['7.12.34', '2.2.6a', '2.2.6.1'])('updates version resources from %s and detects stale metadata without changing it', version => {
    const base = path.join(repository, '.test-data', 'version-tests'); mkdirSync(base, { recursive: true })
    const root = mkdtempSync(path.join(base, 'version-')); roots.push(root)
    writeFileSync(path.join(root, 'VERSION'), readVersion(repository))
    for (const name of versionResources(repository).files.keys()) {
        mkdirSync(path.dirname(path.join(root, name)), { recursive: true })
        writeFileSync(path.join(root, name), readFileSync(path.join(repository, name)))
    }
    writeFileSync(path.join(root, 'VERSION'), version + '\n')
    const oldInfo = readFileSync(path.join(root, 'build/windows/info.json'), 'utf8')
    expect(() => syncVersionResources(root, true)).toThrow('Version resources differ')
    expect(readFileSync(path.join(root, 'build/windows/info.json'), 'utf8')).toBe(oldInfo)
    expect(syncVersionResources(root)).toBe(version)
    expect(() => syncVersionResources(root, true)).not.toThrow()
    const read = (name: string) => readFileSync(path.join(root, name), 'utf8')
    const numeric = windowsVersion(version)
    const info = JSON.parse(read('build/windows/info.json'))
    expect(info.fixed.file_version).toBe(numeric)
    expect(info.info['0000'].FileVersion).toBe(version)
    expect(read('build/config.yml')).toContain(`version: "${version}"`)
    expect(read('build/windows/wails.exe.manifest')).toContain(`version="${numeric}"`)
    expect(read('build/windows/msix/app_manifest.xml')).toContain(`Version="${numeric}"`)
    expect(read('build/windows/msix/template.xml')).toContain(`Version="${numeric}"`)
    expect(read('build/windows/nsis/wails_tools.nsh')).toContain(`INFO_PRODUCTVERSION "${version}"`)
    expect(read('build/windows/nsis/wails_tools.nsh')).toContain(`INFO_NUMERICVERSION "${numeric}"`)
    expect(read('build/linux/nfpm/nfpm.yaml')).toContain(`version: "${version}"`)
    expect(read('build/android/app/build.gradle')).toContain(`versionName "${version}"`)
    for (const name of ['build/darwin/Info.plist', 'build/darwin/Info.dev.plist', 'build/ios/Info.plist']) expect(read(name)).toContain(`<string>${version}</string>`)
    expect(read('build/ios/Info.dev.plist')).toContain(`<string>${version}-dev</string>`)
})
it('maps hotfix versions to unique ordered numeric Windows versions', () => {
    const versions = ['2.2.6', '2.2.6a', '2.2.6b', '2.2.6z', '2.2.6.1', '2.2.6.2', '2.2.6.10', '2.2.6.2427']
    expect(versions.map(windowsVersion)).toEqual([0, 1, 2, 26, 27, 54, 270, 65529].map(revision => `2.2.6.${revision}`))
})
