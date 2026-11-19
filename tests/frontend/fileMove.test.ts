// @vitest-environment happy-dom
import { act, createElement as h } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { MoveStatus } from '@contracts/file-move-api'
import { FileMovePanel, moveLabel, moveRunning } from '@renderer/features/gallery/FileMovePanel'
import { cleanupReact, clickButton, deferred, mount } from './reactHarness'
const mocks = vi.hoisted(() => ({ confirm: vi.fn(), get: vi.fn(), create: vi.fn(), run: vi.fn(), pause: vi.fn(), retry: vi.fn(), directories: vi.fn() }))
const api = { fileMove: mocks, media: { directories: mocks.directories } }
vi.mock('@renderer/shared/hooks/useConfirm', () => ({ useConfirm: () => ({ confirm: mocks.confirm, isOpen: false }) }))
vi.mock('@renderer/shared/api', () => ({ getApi: () => api }))
const ok = (data: unknown) => ({ ok: true, data })
const fail = { ok: false, error: { code: 'FAILED', message: '시험 오류' } }
const selection = [{ mediaId: 1, revision: 2 }, { mediaId: 2, revision: 5 }]
const state = (changes: Partial<MoveStatus> = {}): MoveStatus => ({ jobId: 'move-1', directory: 'C:/B', state: 'paused', total: 2, completed: 0, failed: 0, cleanupPending: 0, totalBytes: 6000, processedBytes: 0, current: '', items: [], offset: 0, hasMore: false, ...changes })
const folder = { id: 7, directoryPath: 'C:/B', keyVersion: 1, importMediaId: null, fileCount: 1, mediaCount: 1 }
async function choose() {
 await clickButton('파일 이동')
 const select = document.querySelector('select')!
 await act(async () => { select.value = '7'; select.dispatchEvent(new Event('change', { bubbles: true })) })
}
beforeEach(() => { vi.resetAllMocks(); mocks.get.mockResolvedValue(ok(null)); mocks.directories.mockResolvedValue(ok({ data: [folder], hasNextPage: false })); mocks.create.mockResolvedValue(ok({ jobId: 'created' })); for (const name of ['run', 'pause', 'retry'] as const) mocks[name].mockResolvedValue(ok({ jobId: 'move-1' })); mocks.confirm.mockResolvedValue(true) })
afterEach(async () => { await cleanupReact(); vi.useRealTimers(); vi.restoreAllMocks() })
const props = () => ({ selection, disabled: false, onCreated: vi.fn(), onChanged: vi.fn() })
describe('explicit file moves', () => {
 it('creates one job for several media and uses the selected directory', async () => {
  const p = props(); await mount(h(FileMovePanel, p));await choose();await clickButton('이동 시작')
  expect(mocks.create).toHaveBeenCalledWith({ selection, directoryId: 7 });expect(mocks.run).toHaveBeenCalledWith({ jobId: 'created' });expect(p.onCreated).toHaveBeenCalledOnce()
 })
 it('supports canceling the move confirmation and clearing selection', async () => {
  mocks.confirm.mockResolvedValue(false);const p = props();await mount(h(FileMovePanel, p));await choose();await clickButton('이동 시작');expect(mocks.create).not.toHaveBeenCalled();await clickButton('선택 해제');expect(p.onCreated).toHaveBeenCalledOnce();await clickButton('닫기')
 })
 it('displays create, run and transport errors and rechecks status', async () => {
  await mount(h(FileMovePanel, props()));await choose();mocks.create.mockResolvedValueOnce(fail);await clickButton('이동 시작');expect(document.body.textContent).toContain('시험 오류')
  mocks.run.mockResolvedValueOnce(fail);await clickButton('이동 시작');expect(document.body.textContent).toContain('시험 오류')
  await choose();mocks.create.mockRejectedValueOnce(new Error('offline'));await clickButton('이동 시작');expect(document.body.textContent).toContain('요청 결과를 확인하지 못했습니다')
 })
 it('polls running work, pauses, resumes and refreshes media after progress', async () => {
  vi.useFakeTimers();const p=props();mocks.get.mockResolvedValue(ok(state({ state:'running', current:'C:/A.mp4',processedBytes:3000 })));await mount(h(FileMovePanel,p));expect(document.querySelector('[role=progressbar]')?.getAttribute('aria-valuenow')).toBe('50');await clickButton('중단');expect(mocks.pause).toHaveBeenCalledWith({jobId:'move-1'})
  mocks.get.mockResolvedValue(ok(state({completed:1,items:[{id:'1',mediaId:1,fileId:1,source:'C:/A.mp4',target:'C:/B/A_12345678.mp4',state:'completed',error:''}]})))
  await act(async()=>{await vi.advanceTimersByTimeAsync(600)});expect(p.onChanged).toHaveBeenCalled();await clickButton('이어하기');expect(mocks.run).toHaveBeenCalled()
  mocks.get.mockResolvedValue(ok(state({state:'completed_with_errors',failed:1,cleanupPending:1})));await clickButton('상태 새로고침');await clickButton('실패·정리 재시도');expect(mocks.retry).toHaveBeenCalled()
 })
 it('keeps completed results compact and pages file results', async()=>{
  mocks.get.mockResolvedValue(ok(state({state:'completed',completed:2,totalBytes:0,hasMore:true,items:[{id:'x',mediaId:1,fileId:1,source:'C:/A',target:'C:/B',state:'failed',error:'source changed'}]})))
  await mount(h(FileMovePanel,props()));expect(document.body.textContent).toContain('source changed');await clickButton('다음 결과');expect(mocks.get).toHaveBeenLastCalledWith({offset:100});await clickButton('이전 결과');await clickButton('이동 결과 닫기');expect(document.querySelector('[role=progressbar]')).toBeNull();await clickButton('최근 이동 결과 보기');expect(document.querySelector('[role=progressbar]')).not.toBeNull()
 })
 it('handles no folders, folder pagination, search and folder read errors', async()=>{
  mocks.directories.mockResolvedValueOnce(ok({data:[],hasNextPage:false}));await mount(h(FileMovePanel,props()));await clickButton('파일 이동');expect(document.body.textContent).toContain('등록된 폴더가 없습니다')
  mocks.directories.mockResolvedValueOnce(ok({data:[folder],hasNextPage:true}));await clickButton('검색');await clickButton('폴더 더 불러오기');expect(mocks.directories).toHaveBeenLastCalledWith({offset:1,limit:100,keyword:''})
  mocks.directories.mockResolvedValueOnce(fail);await clickButton('검색');expect(document.body.textContent).toContain('시험 오류');mocks.directories.mockRejectedValueOnce(new Error('offline'));await clickButton('검색');expect(document.body.textContent).toContain('대상 폴더를 불러오지 못했습니다')
 })
 it('reports status and action errors and ignores late reads after unmount', async()=>{
  mocks.get.mockResolvedValueOnce(fail);const unmount=await mount(h(FileMovePanel,props()));expect(document.body.textContent).toContain('시험 오류');await unmount()
  mocks.get.mockResolvedValue(ok(state()));await mount(h(FileMovePanel,props()));mocks.run.mockResolvedValueOnce(fail);await clickButton('이어하기');expect(document.body.textContent).toContain('시험 오류');mocks.run.mockRejectedValueOnce(new Error('offline'));await clickButton('이어하기');expect(document.body.textContent).toContain('요청 결과를 확인하지 못했습니다');await cleanupReact()
  mocks.get.mockRejectedValueOnce(new Error('offline'));await mount(h(FileMovePanel,props()));expect(document.body.textContent).toContain('이동 상태를 확인하지 못했습니다');await cleanupReact()
  const pending=deferred<unknown>();mocks.get.mockReturnValueOnce(pending.promise);const stop=await mount(h(FileMovePanel,props()));await stop();await act(async()=>{pending.resolve(ok(state()))});expect(document.body.textContent).toBe('')
 })
 it('prevents duplicate starts while a create request is pending', async()=>{
  const pending=deferred<unknown>();mocks.create.mockReturnValueOnce(pending.promise);await mount(h(FileMovePanel,props()));await choose();await clickButton('이동 시작');expect((document.querySelector('select')?.parentElement?.querySelector('button') as HTMLButtonElement).disabled).toBe(true);await act(async()=>pending.resolve(ok({jobId:'created'})))
 })
 it('shows all state names and prevents an empty selection', async()=>{
  for(const value of ['pending','running','pausing','paused','interrupted','completed','completed_with_errors','failed','copying','verified','committed','cleanup_pending','excluded','other']) expect(moveLabel(value)).toBeTruthy()
  expect(moveRunning('running')).toBe(true);expect(moveRunning('paused')).toBe(false)
  await mount(h(FileMovePanel,{...props(),selection:[]}));expect([...document.querySelectorAll('button')].find(b=>b.textContent==='파일 이동')?.disabled).toBe(true)
 })
 it('searches by keyword and keeps pausing disabled', async()=>{
  mocks.get.mockResolvedValue(ok(state({state:'pausing',total:0,totalBytes:0})));await mount(h(FileMovePanel,props()));expect([...document.querySelectorAll('button')].find(b=>b.textContent==='중단')?.disabled).toBe(true);await cleanupReact()
  mocks.get.mockResolvedValue(ok(null));await mount(h(FileMovePanel,props()));await clickButton('파일 이동')
  const input=document.querySelector('input')!;await act(async()=>{const setter=Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value')!.set!;setter.call(input,'B');input.dispatchEvent(new Event('input',{bubbles:true}))});await clickButton('검색');expect(mocks.directories).toHaveBeenCalled()
 })
 it('ignores folder and create responses after the page closes', async()=>{
  const dirs=deferred<unknown>();mocks.directories.mockReturnValueOnce(dirs.promise);const stop=await mount(h(FileMovePanel,props()));await clickButton('파일 이동');await stop();await act(async()=>dirs.resolve(ok({data:[folder],hasNextPage:false})));expect(document.body.textContent).toBe('')
  const create=deferred<unknown>();mocks.create.mockReturnValueOnce(create.promise);const stop2=await mount(h(FileMovePanel,props()));await choose();await clickButton('이동 시작');await stop2();await act(async()=>create.resolve(ok({jobId:'late'})));expect(mocks.run).not.toHaveBeenCalled()
 })

})
