import fs from 'node:fs'
import path from 'node:path'
import crypto from 'node:crypto'
import { fileURLToPath } from 'node:url'
import { spawnSync } from 'node:child_process'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const destination = path.join(root, 'internal/licenses')
const hash = bytes => crypto.createHash('sha256').update(bytes).digest('hex')
const read = filename => fs.readFileSync(filename)
const normalizeText = bytes => bytes.toString('utf8').replaceAll('\r\n', '\n')
const inputHash = (file, bytes) => hash(/\.(ttf|jpg|png)$/i.test(file) ? bytes : normalizeText(bytes))
const run = (command, args, env = {}) => {
    const result = spawnSync(command, args, { cwd: root, encoding: 'utf8', maxBuffer: 32 * 1024 * 1024, env: { ...process.env, GOPROXY: 'off', GOSUMDB: 'off', ...env } })
    if (result.status !== 0) throw new Error(`${command} ${args.join(' ')}: ${result.stderr}`)
    return result.stdout.trim()
}
const inputFiles = ['go.mod', 'go.sum', 'frontend/package.json', 'frontend/package-lock.json', 'frontend/public/Inter-Medium.ttf', 'frontend/public/bg-desktop.jpg', 'frontend/public/bg-mobile.jpg', 'internal/engine/manager.go', 'internal/engine/integrity.go', 'internal/engine/imagemagick_runtime.go', 'tools/generate-licenses.mjs', 'internal/licenses/upstream/provenance.json', 'internal/licenses/upstream/engines.json']
const inputs = () => Object.fromEntries(inputFiles.map(file => [file, inputHash(file, read(path.join(root, file)))]))
const noticeNames = /^(?:(?:licen[cs]e|copying|notice|copyright|authors|patents)([._-]|$)|third[_-]?party.*(?:licen[cs]e|notice))/i
const entryID = name => name.replace(/[^a-zA-Z0-9._-]/g, '_')
const moduleLicenses = {
    'github.com/andybalholm/brotli': 'MIT',
    'github.com/bodgit/plumbing': 'BSD-3-Clause', 'github.com/bodgit/sevenzip': 'BSD-3-Clause', 'github.com/bodgit/windows': 'BSD-3-Clause',
    'github.com/hashicorp/golang-lru/v2': 'MPL-2.0',
    'github.com/klauspost/compress': 'BSD-3-Clause; additional source notices', 'github.com/pierrec/lz4/v4': 'BSD-3-Clause',
    'github.com/spf13/afero': 'Apache-2.0', 'github.com/stangelandcl/ppmd': 'MIT', 'github.com/ulikunitz/xz': 'BSD-3-Clause',
    'go4.org': 'Apache-2.0; additional source notices',
    'github.com/nlnwa/whatwg-url': 'Apache-2.0', 'github.com/wailsapp/wails/v3': 'MIT; additional MIT/ISC notices',
    'github.com/adrg/xdg': 'MIT', 'github.com/go-ole/go-ole': 'MIT', 'github.com/dustin/go-humanize': 'MIT',
    'github.com/mattn/go-isatty': 'MIT', 'github.com/ncruces/go-strftime': 'MIT',
    'github.com/bits-and-blooms/bitset': 'BSD-3-Clause', 'github.com/google/uuid': 'BSD-3-Clause',
    'github.com/remyoudompheng/bigfft': 'BSD-3-Clause', 'golang.org/x/net': 'BSD-3-Clause',
    'golang.org/x/sys': 'BSD-3-Clause', 'golang.org/x/mod': 'BSD-3-Clause', 'golang.org/x/text': 'BSD-3-Clause; additional source notices',
    'golang.org/x/image': 'BSD-3-Clause; additional source notices',
    'modernc.org/libc': 'BSD-3-Clause; see third-party notices', 'modernc.org/mathutil': 'BSD-3-Clause; see additional notices',
    'modernc.org/memory': 'BSD-3-Clause; see additional notices', 'modernc.org/sqlite': 'BSD-3-Clause; see third-party notices',
}

function modules() {
    const records = new Map()
    const lines = run('go', ['list', '-deps', '-tags', 'production', '-f', '{{if .Module}}{{.Module.Path}}|{{.Module.Version}}|{{.Module.Dir}}{{end}}', '.'], { GOOS: 'windows', GOARCH: 'amd64', CGO_ENABLED: '0' }).split(/\r?\n/)
    for (const line of lines) {
        const [name, version, directory] = line.split('|')
        if (version) records.set(name, { name, version, directory })
    }
    return [...records.values()].sort((a, b) => a.name.localeCompare(b.name, 'en'))
}

function filesIn(directory) {
    return fs.readdirSync(directory, { withFileTypes: true }).filter(item => item.isFile() && noticeNames.test(item.name)).map(item => item.name).sort()
}
function noticeFilesIn(directory) {
    const result = []
    function visit(folder) {
        for (const item of fs.readdirSync(folder, { withFileTypes: true })) {
            if (['.git', 'node_modules', 'testdata', 'examples', 'tests', 'test', 'cmd'].includes(item.name)) continue
            const file = path.join(folder, item.name)
            if (item.isDirectory()) visit(file)
            else if (noticeNames.test(item.name) && !/\.(pdf|png|jpg|go|js|ts)$/i.test(item.name)) result.push(path.relative(directory, file).replaceAll('\\', '/'))
        }
    }
    visit(directory)
    return result.sort()
}

function sourceNotices(directory) {
    const result = new Map()
    function visit(folder) {
        for (const item of fs.readdirSync(folder, { withFileTypes: true })) {
            if (['.git', 'node_modules', 'testdata', 'examples', 'tests', 'test', 'cmd'].includes(item.name)) continue
            const file = path.join(folder, item.name)
            if (item.isDirectory()) visit(file)
            else if (/\.(go|c|h|s|S)$/.test(item.name)) {
                const handle = fs.openSync(file, 'r'), bytes = Buffer.alloc(65536)
                const length = fs.readSync(handle, bytes, 0, bytes.length, 0); fs.closeSync(handle)
                const source = bytes.subarray(0, length).toString('utf8')
                for (const match of source.matchAll(/(?:^[ \t]*\/\/[^\n]*(?:\n|$))+|\/\*[\s\S]*?\*\//gm)) {
                    if (!/copyright|permission to (?:use|copy)|redistribution and use|public domain|SPDX-License-Identifier/i.test(match[0])) continue
                    const notice = match[0].trimEnd()
                    if (!result.has(notice)) result.set(notice, [])
                    result.get(notice).push(path.relative(directory, file).replaceAll('\\', '/'))
                }
            }
        }
    }
    visit(directory)
    return [...result].map(([notice, files]) => `Source: ${files.join(', ')}\n${notice}`).join('\n\n')
}

export function checkSnapshot(base, snapshot) {
    for (const [file, expected] of Object.entries(snapshot.inputs)) {
        if (inputHash(file, read(path.join(base, file))) !== expected) throw new Error(`라이선스 목록 갱신 필요: ${file}`)
    }
    const seen = new Set()
    for (const item of snapshot.entries) {
        if (seen.has(item.id) || !item.documents.length || !item.license || !item.url.startsWith('https://')) throw new Error(`잘못된 라이선스 항목: ${item.id}`)
        seen.add(item.id)
        for (const document of item.documents) {
            if (!/^texts\/[a-zA-Z0-9._-]+\.txt$/.test(document.file)) throw new Error(`잘못된 고지 경로: ${document.file}`)
            const bytes = read(path.join(base, 'internal/licenses', document.file))
            if (!bytes.length || hash(bytes) !== document.sha256) throw new Error(`라이선스 원문 불일치: ${document.file}`)
        }
    }
    const provenance = JSON.parse(read(path.join(base, 'internal/licenses/upstream/provenance.json')))
    for (const item of provenance) {
        if (!/^[a-zA-Z0-9._-]+$/.test(item.file) || hash(read(path.join(base, 'internal/licenses/upstream', item.file))) !== item.sha256) throw new Error(`외부 고지 원문 불일치: ${item.file}`)
    }
}

function summary(snapshot) {
    return '# Third-Party Software Notices\n\n' +
        'StorageBucket uses the components listed below. Scope identifies bundled components, separately installed external tools, and build-only packages. Original license, copyright, NOTICE and third-party texts supplied by these components are retained in `internal/licenses/texts/`, embedded in the application, and available offline in Settings → Open-source licenses and the mobile `/licenses` page.\n\n' +
        'The StorageBucket project license is in [LICENSE](LICENSE). External tools are downloaded from upstream on the user’s machine and executed separately. Their binary distributions and included dependencies have their own terms. Redistributing those binaries requires satisfying the terms for the exact binaries and their corresponding source, not just retaining this summary.\n\n' +
        '| Component | Version | Scope | License | Original notices |\n| --- | --- | --- | --- | --- |\n' +
        snapshot.entries.map(item => `| [${item.name}](${item.url}) | ${item.version} | ${item.scope} | ${item.license.replaceAll('|', '\\|')} | ${item.documents.map((d, i) => `[${i + 1}](internal/licenses/${d.file})`).join(', ')} |`).join('\n') +
        '\n\nGenerated by `node tools/generate-licenses.mjs`. Verify before shipping with `node tools/generate-licenses.mjs --check`. Build-only packages are included for attribution and audit; they are not all linked into the release executable.\n'
}

async function main() {
    if (process.argv.includes('--check')) {
        const snapshot = JSON.parse(read(path.join(destination, 'catalog.json')))
        checkSnapshot(root, snapshot)
        const actual = modules().map(item => `${item.name}@${item.version}`)
        if (JSON.stringify(actual) !== JSON.stringify(snapshot.goModules)) throw new Error('Windows production Go 의존성이 고지 목록과 다릅니다. 다시 생성하세요.')
        if (run('go', ['env', 'GOVERSION']) !== snapshot.entries[0].version) throw new Error('Go SDK 버전이 달라졌습니다. 고지를 다시 생성하세요.')
        if (normalizeText(read(path.join(root, 'THIRD_PARTY_LICENSES.md'))) !== summary(snapshot)) throw new Error('THIRD_PARTY_LICENSES.md를 다시 생성하세요.')
        console.log(`라이선스 고지 확인: ${snapshot.entries.length}개 항목`)
        return
    }
    fs.mkdirSync(path.join(destination, 'texts'), { recursive: true })
    const entries = []
    function document(id, title, filename, url) {
        const bytes = read(filename)
        if (!bytes.length) throw new Error(`빈 라이선스 원문: ${filename}`)
        const file = `texts/${entryID(id)}.txt`
        fs.writeFileSync(path.join(destination, file), bytes)
        return { title, file, url, sha256: hash(bytes) }
    }
    function record(name, version, license, scope, url, sourceURL, notice, docs) {
        if (!docs.length) throw new Error(`라이선스 원문 누락: ${name}`)
        const id = entryID(name + '@' + version)
        if (entries.some(item => item.id === id)) return
        sourceURL = sourceURL.replace(/^git\+/, '').replace(/^git:\/\//, 'https://')
        if (!sourceURL.startsWith('https://')) sourceURL = url
        entries.push({ id, name, version, license, scope, url, sourceURL, notice, documents: docs })
    }
    const production = modules()
    const environment = JSON.parse(run('go', ['env', '-json', 'GOROOT', 'GOVERSION', 'GOMODCACHE']))
    const goDocs = [document('go-runtime-LICENSE', 'LICENSE', path.join(environment.GOROOT, 'LICENSE'), 'https://go.dev/LICENSE')]
    for (const name of noticeFilesIn(path.join(environment.GOROOT, 'src'))) goDocs.push(document('go-stdlib-' + name, 'src/' + name, path.join(environment.GOROOT, 'src', name), 'https://go.dev/src/' + name))
    const standardNotices = sourceNotices(path.join(environment.GOROOT, 'src'))
    const standardFile = 'texts/go-stdlib-source-notices.txt'
    fs.writeFileSync(path.join(destination, standardFile), standardNotices + '\n')
    goDocs.push({ title: 'Original license and copyright comment blocks retained from Go source', file: standardFile, url: 'https://go.dev/src/', sha256: hash(read(path.join(destination, standardFile))) })
    record('Go standard library and runtime', environment.GOVERSION, 'BSD-3-Clause; additional source notices', 'bundled', 'https://go.dev/', 'https://go.dev/dl/', 'Go 런타임·표준 라이브러리와 소스 트리에 동봉된 추가 고지.', goDocs)
    for (const mod of production) {
        const docs = noticeFilesIn(mod.directory).map(name => document(`${mod.name}-${name}`, name, path.join(mod.directory, name), `https://pkg.go.dev/${mod.name}@${mod.version}`))
        const copyrights = sourceNotices(mod.directory)
        if (copyrights) {
            const file = `texts/${entryID(mod.name)}-source-copyrights.txt`
            fs.writeFileSync(path.join(destination, file), copyrights + '\n')
            docs.push({ title: 'Original license and copyright comment blocks retained from source', file, url: `https://pkg.go.dev/${mod.name}@${mod.version}`, sha256: hash(read(path.join(destination, file))) })
        }
        if (!moduleLicenses[mod.name]) throw new Error(`새 Go 모듈의 라이선스를 검토하세요: ${mod.name}`)
        record(mod.name, mod.version, moduleLicenses[mod.name], 'bundled', `https://pkg.go.dev/${mod.name}@${mod.version}`, `https://pkg.go.dev/${mod.name}@${mod.version}`, 'Windows production EXE에 포함되는 Go 모듈. 모듈에 동봉된 추가 고지와 저작권도 함께 보존합니다.', docs)
    }
    const wails = production.find(item => item.name === 'github.com/wailsapp/wails/v3')
    const lock = JSON.parse(read(path.join(root, 'frontend/package-lock.json')))
    for (const [key, info] of Object.entries(lock.packages).sort(([a], [b]) => a.localeCompare(b, 'en'))) {
        if (!key) continue
        if (!fs.existsSync(path.join(root, 'frontend', key, 'package.json'))) {
            if (!info.dev && !info.optional) throw new Error(`설치되지 않은 runtime 의존성: ${key}`)
            continue
        }
        const base = path.join(root, 'frontend', key)
        const pkg = JSON.parse(read(path.join(base, 'package.json')))
        if (pkg.version !== info.version) throw new Error(`lockfile과 설치 버전이 다릅니다: ${key}`)
        const scope = ['@wailsio/runtime', 'lucide-react', 'react', 'react-dom', 'scheduler', 'tailwindcss'].includes(pkg.name) ? 'bundled' : 'build'
        let docs = filesIn(base).map(name => document(`npm-${key}-${name}`, name, path.join(base, name), `https://www.npmjs.com/package/${pkg.name}/v/${pkg.version}`))
        if (!docs.length && pkg.name === '@wailsio/runtime') docs = [document('npm-wails-runtime-LICENSE', 'Wails LICENSE (runtime source repository)', path.join(wails.directory, 'LICENSE'), 'https://github.com/wailsapp/wails/blob/v3.0.0/v3/LICENSE')]
        if (!docs.length && pkg.name.startsWith('@rolldown/binding-')) {
            const parent = path.join(root, 'frontend/node_modules/rolldown')
            const parentPackage = JSON.parse(read(path.join(parent, 'package.json')))
            if (parentPackage.version !== pkg.version) throw new Error('rolldown native binding version mismatch')
            docs = filesIn(parent).map(name => document(`npm-${key}-${name}`, 'rolldown/' + name, path.join(parent, name), `https://www.npmjs.com/package/rolldown/v/${pkg.version}`))
        }
        if (!docs.length && pkg.name === 'stackback') docs = [document('stackback-package-metadata', 'Original package metadata (MIT declaration and author; upstream package supplies no separate license file)', path.join(base, 'package.json'), `https://www.npmjs.com/package/stackback/v/${pkg.version}`)]
        if (!docs.length) throw new Error(`npm 라이선스 원문을 확인해야 합니다: ${pkg.name}@${pkg.version}`)
        const license = pkg.name === 'lucide-react' ? 'ISC AND MIT (Feather portions)' : typeof pkg.license === 'string' ? pkg.license : info.license
        const notice = pkg.name === 'stackback' ? '빌드·테스트 전용입니다. 원본 패키지는 별도 LICENSE 파일 없이 package.json에 MIT와 작성자를 명시하므로 해당 원본 선언을 보존합니다.' : scope === 'bundled' ? '프런트엔드 번들 또는 생성된 스타일에 사용합니다.' : '빌드·타입 검사·테스트용 의존성. 이 분류가 모두 배포 EXE에 포함된다는 의미는 아닙니다.'
        record('npm: ' + pkg.name, pkg.version, license, scope, `https://www.npmjs.com/package/${pkg.name}/v/${pkg.version}`, typeof pkg.repository === 'string' ? pkg.repository : pkg.repository?.url || pkg.homepage || `https://www.npmjs.com/package/${pkg.name}`, notice, docs)
    }
    const fontLicense = path.join(wails.directory, 'internal/templates/_common/frontend/Inter Font License.txt')
    for (const [asset, original] of [['Inter-Medium.ttf', 'react/frontend/public/Inter-Medium.ttf'], ['bg-desktop.jpg', '_common/frontend/public/bg-desktop.jpg'], ['bg-mobile.jpg', '_common/frontend/public/bg-mobile.jpg']]) {
        if (hash(read(path.join(root, 'frontend/public', asset))) !== hash(read(path.join(wails.directory, 'internal/templates', original)))) throw new Error(`Wails 템플릿 리소스 출처를 다시 검토하세요: ${asset}`)
    }
    record('Inter font', 'Bundled Inter-Medium.ttf', 'OFL-1.1', 'bundled', 'https://rsms.me/inter/', 'https://github.com/rsms/inter', 'Wails 템플릿에서 가져온 글꼴 파일의 고지입니다.', [document('inter-OFL', 'Inter Font License.txt', fontLicense, 'https://github.com/rsms/inter/blob/master/LICENSE.txt')])
    const upstream = JSON.parse(read(path.join(destination, 'upstream/provenance.json')))
    for (const source of upstream) if (hash(read(path.join(destination, 'upstream', source.file))) !== source.sha256) throw new Error(`외부 원문 해시 불일치: ${source.file}`)
    const external = JSON.parse(read(path.join(destination, 'upstream/engines.json')))
    for (const item of external) {
        const docs = item.files.map(name => {
            const source = upstream.find(entry => entry.file === name)
            if (!source) throw new Error(`원문 출처 누락: ${name}`)
            return document('external-' + name, name, path.join(destination, 'upstream', name), source.url)
        })
        record(item.name, item.version, item.license, 'external', item.url, item.sourceURL, item.notice, docs)
    }
    const snapshot = { schema: 1, target: 'windows/amd64 production', inputs: inputs(), goModules: production.map(item => `${item.name}@${item.version}`), entries }
    fs.writeFileSync(path.join(destination, 'catalog.json'), JSON.stringify(snapshot, null, 2) + '\n')
    fs.writeFileSync(path.join(root, 'THIRD_PARTY_LICENSES.md'), summary(snapshot))
    console.log(`라이선스 고지 생성: ${entries.length}개 항목`)
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) main().catch(error => { console.error(error.message); process.exitCode = 1 })
