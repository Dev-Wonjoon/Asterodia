import { test } from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs'
import path from 'node:path'
import crypto from 'node:crypto'
import { fileURLToPath } from 'node:url'
import { checkSnapshot } from '../../tools/generate-licenses.mjs'

const repository = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..')
const base = path.join(repository, '.test-data/licenses')
let parent = base
while (!fs.existsSync(parent)) parent = path.dirname(parent)
const realRoot = fs.realpathSync(repository), realParent = fs.realpathSync(parent)
if (realParent !== realRoot && !realParent.startsWith(realRoot + path.sep)) throw new Error('격리 경로가 저장소 밖을 가리킵니다.')
fs.mkdirSync(base, { recursive: true })
const run = fs.mkdtempSync(path.join(base, 'generator-'))
const hash = text => crypto.createHash('sha256').update(text).digest('hex')

test('rejects dependency drift, missing or modified text, unsafe paths and duplicate entries', () => {
    const folder = path.join(run, 'fixture')
    fs.mkdirSync(path.join(folder, 'internal/licenses/texts'), { recursive: true })
    fs.mkdirSync(path.join(folder, 'internal/licenses/upstream'), { recursive: true })
    fs.writeFileSync(path.join(folder, 'internal/licenses/upstream/provenance.json'), '[]')
    fs.writeFileSync(path.join(folder, 'go.mod'), 'original dependency\r\n')
    const text = 'Copyright Example\nOriginal permission terms'
    const textPath = path.join(folder, 'internal/licenses/texts/component.txt')
    fs.writeFileSync(textPath, text)
    const entry = { id: 'component', license: 'MIT', url: 'https://example.com', documents: [{ file: 'texts/component.txt', sha256: hash(text) }] }
    const snapshot = { inputs: { 'go.mod': hash('original dependency\n') }, entries: [entry] }
    assert.doesNotThrow(() => checkSnapshot(folder, snapshot))
    fs.writeFileSync(path.join(folder, 'go.mod'), 'updated dependency')
    assert.throws(() => checkSnapshot(folder, snapshot), /go.mod/)
    fs.writeFileSync(path.join(folder, 'go.mod'), 'original dependency\n')
    fs.writeFileSync(textPath, 'modified')
    assert.throws(() => checkSnapshot(folder, snapshot), /원문 불일치/)
    fs.writeFileSync(textPath, text)
    assert.throws(() => checkSnapshot(folder, { ...snapshot, entries: [entry, entry] }), /잘못된 라이선스 항목/)
    assert.throws(() => checkSnapshot(folder, { ...snapshot, entries: [{ ...entry, documents: [{ file: '../outside.txt' }] }] }), /잘못된 고지 경로/)
    assert.throws(() => checkSnapshot(folder, { ...snapshot, entries: [{ ...entry, documents: [] }] }), /잘못된 라이선스 항목/)
    fs.unlinkSync(textPath)
    assert.throws(() => checkSnapshot(folder, snapshot), /ENOENT/)
})
