import { readFileSync, writeFileSync } from 'node:fs'

const config = readFileSync(new URL('../build/config.yml', import.meta.url), 'utf8')
const version = config.match(/^  version: "([^"]+)"/m)?.[1]
if (!version || !/^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/.test(version)) {
    throw new Error('build/config.yml must contain a stable major.minor.patch version.')
}
if (process.argv.includes('--sync-windows')) {
    const infoURL = new URL('../build/windows/info.json', import.meta.url)
    const info = JSON.parse(readFileSync(infoURL, 'utf8'))
    info.fixed = { ...info.fixed, file_version: `${version}.0`, product_version: `${version}.0` }
    for (const locale of Object.values(info.info)) {
        locale.FileVersion = version
        locale.ProductVersion = version
    }
    writeFileSync(infoURL, JSON.stringify(info, null, '\t') + '\n')
    const manifestURL = new URL('../build/windows/wails.exe.manifest', import.meta.url)
    const manifest = readFileSync(manifestURL, 'utf8').replace(/(<assemblyIdentity type="win32" name="com\.storagebucket\.app" version=")[^"]+/, (_, prefix) => `${prefix}${version}.0`)
    writeFileSync(manifestURL, manifest)
    const nsisURL = new URL('../build/windows/nsis/wails_tools.nsh', import.meta.url)
    writeFileSync(nsisURL, readFileSync(nsisURL, 'utf8').replace(/(!define INFO_PRODUCTVERSION ")[^"]+/, (_, prefix) => `${prefix}${version}`))
    process.stdout.write(`Windows version resources: ${version}\n`)
} else process.stdout.write(version)
