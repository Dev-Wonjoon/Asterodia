import { readFileSync, writeFileSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const repository = fileURLToPath(new URL('../', import.meta.url))

export function parseVersion(value) {
    const version = value.trim()
    const match = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:\.([1-9]\d*))?([a-z])?$/.exec(version)
    const revision = match ? Number(match[4] || 0) * 27 + (match[5] ? match[5].charCodeAt(0) - 96 : 0) : 0
    if (!match || match.slice(1, 4).some(part => Number(part) > 65535) || revision > 65535) {
        throw new Error('VERSION must be major.minor.patch[letter] or major.minor.patch.revision[letter], with components 0–65535 and revision*27+letter <= 65535.')
    }
    return version
}

export function windowsVersion(value) {
    const version = parseVersion(value)
    const match = /^(\d+)\.(\d+)\.(\d+)(?:\.(\d+))?([a-z])?$/.exec(version)
    const revision = Number(match[4] || 0) * 27 + (match[5] ? match[5].charCodeAt(0) - 96 : 0)
    return `${match[1]}.${match[2]}.${match[3]}.${revision}`
}

export function readVersion(root = repository) {
    return parseVersion(readFileSync(path.join(root, 'VERSION'), 'utf8'))
}

export function validateTag(tag, version) {
    if (tag !== `v${version}`) throw new Error(`Release tag ${JSON.stringify(tag)} must match VERSION: v${version}`)
}

function replaceOne(text, pattern, replacement, filename) {
    if (!pattern.test(text)) throw new Error(`Version field is missing in ${filename}`)
    return text.replace(pattern, replacement)
}

export function versionResources(root = repository) {
    const version = readVersion(root), numeric = windowsVersion(version), files = new Map()
    const read = name => readFileSync(path.join(root, name), 'utf8')
    const config = 'build/config.yml'
    files.set(config, replaceOne(read(config), /^(  version: ")[^"]+(".*)$/m, (_, before, after) => `${before}${version}${after}`, config))
    const infoName = 'build/windows/info.json', info = JSON.parse(read(infoName))
    info.fixed = { ...info.fixed, file_version: numeric, product_version: numeric }
    for (const locale of Object.values(info.info)) {
        locale.FileVersion = version
        locale.ProductVersion = version
    }
    files.set(infoName, JSON.stringify(info, null, '\t') + '\n')
    const manifest = 'build/windows/wails.exe.manifest'
    files.set(manifest, replaceOne(read(manifest), /(<assemblyIdentity type="win32" name="com\.storagebucket\.app" version=")[^"]+/, (_, before) => `${before}${numeric}`, manifest))
    for (const msix of ['build/windows/msix/app_manifest.xml', 'build/windows/msix/template.xml']) {
        files.set(msix, replaceOne(read(msix), /(\bVersion=")[^"]+/, (_, before) => `${before}${numeric}`, msix))
    }
    const nsis = 'build/windows/nsis/wails_tools.nsh'
    let nsisText = replaceOne(read(nsis), /(!define INFO_PRODUCTVERSION ")[^"]+/, (_, before) => `${before}${version}`, nsis)
    nsisText = replaceOne(nsisText, /(!define INFO_NUMERICVERSION ")[^"]+/, (_, before) => `${before}${numeric}`, nsis)
    files.set(nsis, nsisText)
    for (const name of ['build/darwin/Info.plist', 'build/darwin/Info.dev.plist', 'build/ios/Info.plist', 'build/ios/Info.dev.plist']) {
        let text = read(name)
        for (const key of ['CFBundleShortVersionString', 'CFBundleVersion']) {
            const value = name === 'build/ios/Info.dev.plist' && key === 'CFBundleShortVersionString' ? `${version}-dev` : version
            text = replaceOne(text, new RegExp(`(<key>${key}</key>\\s*<string>)[^<]+`), (_, before) => before + value, name)
        }
        files.set(name, text)
    }
    const linux = 'build/linux/nfpm/nfpm.yaml'
    files.set(linux, replaceOne(read(linux), /^(version: ")[^"]+(")/m, (_, before, after) => before + version + after, linux))
    const android = 'build/android/app/build.gradle'
    files.set(android, replaceOne(read(android), /^(\s*versionName ")[^"]+(")/m, (_, before, after) => before + version + after, android))
    return { version, files }
}

export function syncVersionResources(root = repository, check = false) {
    const { version, files } = versionResources(root), changed = []
    for (const [name, expected] of files) {
        if (readFileSync(path.join(root, name), 'utf8').replaceAll('\r\n', '\n') === expected.replaceAll('\r\n', '\n')) continue
        changed.push(name)
        if (!check) writeFileSync(path.join(root, name), expected)
    }
    if (check && changed.length) throw new Error(`Version resources differ from VERSION. Run node tools/release-version.mjs --sync: ${changed.join(', ')}`)
    return version
}

function main(args) {
    if (args.length === 2 && args[0] === '--windows-version') {
        process.stdout.write(windowsVersion(args[1]))
        return
    }
    let sync = false, check = false, tag
    for (let i = 0; i < args.length; i++) {
        if (args[i] === '--sync' || args[i] === '--sync-windows') sync = true
        else if (args[i] === '--check') check = true
        else if (args[i] === '--tag' && args[i + 1]) tag = args[++i]
        else throw new Error(`Unknown or incomplete argument: ${args[i]}`)
    }
    const version = readVersion()
    if (tag !== undefined) validateTag(tag, version)
    if (sync && check) throw new Error('Use --sync or --check, not both.')
    if (sync || check) syncVersionResources(repository, check)
    process.stdout.write(version)
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
    try { main(process.argv.slice(2)) } catch (error) { console.error(error.message); process.exitCode = 1 }
}
