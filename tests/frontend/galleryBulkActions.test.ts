// @vitest-environment happy-dom
import { act, createElement as h, useState } from 'react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { useGalleryBulkActions, type BulkCommand } from '@renderer/features/gallery/useGalleryBulkActions'
import type { GalleryStore } from '@renderer/features/gallery/galleryStore'
import type { MoveSelection } from '@contracts/file-move-api'
import { cleanupReact, deferred, mount } from './reactHarness'

const mocks = vi.hoisted(()=>({confirm:vi.fn(),open:vi.fn(),get:vi.fn()}))
vi.mock('@renderer/shared/hooks/useConfirm',()=>({useConfirm:()=>({confirm:mocks.confirm})}))
vi.mock('@renderer/shared/api',()=>({getApi:()=>({showItemInFolder:mocks.open,media:{get:mocks.get}})}))
afterEach(cleanupReact)
beforeEach(()=>vi.resetAllMocks())
let view!: ReturnType<typeof useGalleryBulkActions>
let changeSelection!: () => void
function setup(initial: MoveSelection[] = [{mediaId:1,revision:4},{mediaId:2,revision:4}]) {
    const bulk = vi.fn(async (_targets:any,_action:any,progress:any)=>{progress(1,2);return {succeeded:[1],failed:[{mediaId:2,message:'실패'}]}})
    const rows = initial.map(target => ({ id: target.mediaId, revision: target.revision, fileCount: target.fileIds?.length ?? 1, managedFileCount: 0, files: { data: (target.fileIds ?? []).map(id => ({ id, storageKind: 'external_reference' })), hasNextPage: false } }))
    const snapshot = {busy:false,needsRefresh:false,list:{page:{data:rows}}}
    mocks.get.mockResolvedValue({ok:true,data:{id:1,revision:4,fileCount:1,managedFileCount:0,files:{data:[{id:11,storageKind:'external_reference'}],hasNextPage:false}}})
    const store = {bulk,getSnapshot:()=>snapshot} as unknown as GalleryStore
    const success = vi.fn()
    function Harness(){
        const [selection,setSelection]=useState(initial)
        changeSelection=()=>setSelection([])
        view=useGalleryBulkActions(store,selection,success)
        return h('span',null,view.busy?'진행 중':'대기')
    }
    return {bulk,snapshot,store,success,Harness}
}
it('reports progress and leaves the caller to clear only successful targets',async()=>{
    const {Harness,bulk,success}=setup();await mount(h(Harness))
    await act(async()=>{await view.run('addTags',['a'])})
    expect(bulk).toHaveBeenCalledWith(expect.any(Array),{kind:'tags',names:['a'],remove:false},expect.any(Function))
    expect(success).toHaveBeenCalledWith([1], 'addTags');expect(view.result?.failed).toHaveLength(1);expect(view.busy).toBe(false);expect(view.progress).toBeNull()
})
it('asks once for registration removal, blocks duplicate activation, and respects cancellation',async()=>{
    const {Harness,bulk}=setup(),answer=deferred<boolean>();mocks.confirm.mockReturnValueOnce(answer.promise)
    await mount(h(Harness));let run!:Promise<void>
    await act(async()=>{run=view.run('unregister');await view.run('unregister')})
    expect(mocks.confirm).toHaveBeenCalledTimes(1);expect(bulk).not.toHaveBeenCalled();expect(view.busy).toBe(true)
    await act(async()=>{answer.resolve(false);await run});expect(bulk).not.toHaveBeenCalled();expect(view.busy).toBe(false)
    mocks.confirm.mockResolvedValueOnce(true);await act(async()=>{await view.run('unregister')});expect(bulk).toHaveBeenCalledTimes(1)
})
it('ignores stale approval when selection changes or the view unmounts',async()=>{
    const {Harness,bulk}=setup(),answer=deferred<boolean>();mocks.confirm.mockReturnValueOnce(answer.promise)
    await mount(h(Harness));let run!:Promise<void>
    await act(async()=>{run=view.run('openFolders');changeSelection()});await act(async()=>{answer.resolve(true);await run});expect(bulk).not.toHaveBeenCalled()
    await cleanupReact();const second=deferred<boolean>();mocks.confirm.mockReturnValueOnce(second.promise);const stop=await mount(h(Harness))
    await act(async()=>{run=view.run('unregister')});await stop();await act(async()=>{second.resolve(true);await run});expect(bulk).not.toHaveBeenCalled()
})
it('connects folder opening, handles a rejected request and blocks writes needing refresh',async()=>{
    const {Harness,bulk,snapshot}=setup();await mount(h(Harness));mocks.confirm.mockResolvedValue(true)
    await act(async()=>{await view.run('openFolders')})
    const action=bulk.mock.calls[0][1];await action.open('C:/one.png');expect(mocks.open).toHaveBeenCalledWith('C:/one.png')
    bulk.mockRejectedValueOnce(new Error('lost'));await act(async()=>{await view.run('unfavorite')})
    expect(view.result?.failed).toHaveLength(2);expect(view.busy).toBe(false)
    snapshot.needsRefresh=true;await act(async()=>{await view.run('favorite')});expect(bulk).toHaveBeenCalledTimes(2)
})
it('covers tag removal and an operation rejected by the store lock',async()=>{
    const {Harness,bulk}=setup();await mount(h(Harness));bulk.mockResolvedValueOnce(null as any)
    await act(async()=>{await view.run('removeTags',['one'])});expect(view.result).toBeNull();expect(view.progress).toBeNull()
    expect(bulk).toHaveBeenLastCalledWith(expect.any(Array),{kind:'tags',names:['one'],remove:true},expect.any(Function))
})
it('confirms all selected cards once while keeping other bulk actions scoped to collections', async () => {
    const targets = [{ mediaId: 1, revision: 4 }, { mediaId: 2, revision: 4, fileIds: [21, 22] }]
    const { Harness, bulk, success } = setup(targets)
    mocks.confirm.mockResolvedValue(true)
    await mount(h(Harness))
    await act(async () => { await view.run('unregister') })
    expect(mocks.confirm).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ danger: true, message: expect.stringContaining('3개') }))
    expect(bulk).toHaveBeenLastCalledWith(targets, { kind: 'unregister' }, expect.any(Function))
    expect(success).toHaveBeenLastCalledWith([1], 'unregister')
    await act(async () => { await view.run('favorite') })
    expect(bulk).toHaveBeenLastCalledWith([targets[0]], { kind: 'favorite', value: true }, expect.any(Function))
})
it('confirms a focused file without selection', async () => {
    const { Harness, bulk } = setup([]), answer = deferred<boolean>()
    mocks.confirm.mockReturnValueOnce(answer.promise)
    await mount(h(Harness))
    const fallback = [{ mediaId: 1, revision: 4, fileIds: [11] }]
    let pending!: Promise<void>
    await act(async () => { pending = view.run('unregister', [], fallback) })
    expect(mocks.confirm).toHaveBeenCalledOnce(); expect(bulk).not.toHaveBeenCalled()
    await act(async () => { answer.resolve(true); await pending })
    expect(bulk).toHaveBeenCalledExactlyOnceWith(fallback, { kind: 'unregister' }, expect.any(Function))
})
it('rejects approval when the gallery page refreshes while the popup is open', async () => {
    const { Harness, bulk, snapshot } = setup(), answer = deferred<boolean>()
    mocks.confirm.mockReturnValueOnce(answer.promise)
    await mount(h(Harness))
    let pending!: Promise<void>
    await act(async () => { pending = view.run('unregister') })
    snapshot.list.page = {data:[]}
    await act(async () => { answer.resolve(true); await pending })
    expect(bulk).not.toHaveBeenCalled(); expect(view.busy).toBe(false)
})
