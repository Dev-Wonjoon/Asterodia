import { expect, it, vi } from 'vitest'
import type { MediaApi } from '@contracts/media-api'
import { createGalleryStore } from '@renderer/features/gallery/galleryStore'
import { deferred } from './reactHarness'

const ok = (data: any) => ({ ok: true as const, data })
const fail = (code = 'JOB_ACTIVE') => ({ ok: false as const, error: { code, message: code, context: {} } })
const selection = [{ mediaId: 1, revision: 4 }, { mediaId: 2, revision: 4 }]
const detail = (id: number) => ({ id, revision: 4, title: '게시글 '+id, isFavorite: false, tags: [{ id: 7, name: '유지' }, { id: 8, name: '제거' }], files: { data: [{ filepath: 'C:/folder/file'+id+'.png' }], total: 1, hasNextPage: false } })
function setup() {
    const api = { get: vi.fn(async ({mediaId}) => ok(detail(mediaId))), edit: vi.fn(async ({mediaId}) => ok({...detail(mediaId),revision:5})), unregisterMedia: vi.fn(async ({mediaId})=>ok({mediaId,parentRemoved:true})), list: vi.fn(async()=>ok({data:selection.map(item=>detail(item.mediaId)),total:2,hasNextPage:false})) }
    const store = createGalleryStore(api as unknown as MediaApi)
    return { api, store }
}
it('serializes one batch, deduplicates IDs and blocks concurrent mutations', async()=>{
    const {api,store}=setup(), pending=deferred<any>(), progress=vi.fn()
    api.get.mockReturnValueOnce(pending.promise)
    const running=store.bulk([...selection,selection[0]],{kind:'favorite',value:true},progress)
    expect(store.getSnapshot().busy).toBe(true)
    expect(await store.bulk(selection,{kind:'unregister'},progress)).toBeNull()
    expect(await store.edit({mediaId:1,expectedRevision:4,isFavorite:false})).toBe(false)
    expect(api.edit).not.toHaveBeenCalled()
    pending.resolve(ok(detail(1)));expect(await running).toEqual({succeeded:[1,2],failed:[]})
    expect(api.edit.mock.calls.map(([value])=>value)).toEqual(selection.map(item=>({mediaId:item.mediaId,expectedRevision:4,isFavorite:true})))
    expect(api.list).toHaveBeenCalledTimes(1);expect(progress).toHaveBeenLastCalledWith(2,2);expect(store.getSnapshot().busy).toBe(false)
})
it('preserves unrelated tags when adding or removing names',async()=>{
    const {api,store}=setup()
    await store.bulk(selection,{kind:'tags',names:['新規'],remove:false},vi.fn())
    expect(api.edit).toHaveBeenCalledWith({mediaId:1,expectedRevision:4,tagChange:{addNames:['新規']}})
    await store.bulk(selection,{kind:'tags',names:['제거'],remove:true},vi.fn())
    expect(api.edit).toHaveBeenLastCalledWith({mediaId:2,expectedRevision:4,tagChange:{removeIds:[8]}})
})
it('keeps per-item conflicts and known failures while processing the remaining targets',async()=>{
    const {api,store}=setup()
    api.get.mockResolvedValueOnce(ok({...detail(1),revision:5}))
    expect(await store.bulk(selection,{kind:'favorite',value:false},vi.fn())).toEqual({succeeded:[2],failed:[{mediaId:1,message:expect.stringContaining('변경된 게시글')}]})
    expect(api.edit).toHaveBeenCalledTimes(1)
    api.unregisterMedia.mockResolvedValueOnce(fail() as any)
    expect(await store.bulk(selection,{kind:'unregister'},vi.fn())).toEqual({succeeded:[2],failed:[{mediaId:1,message:'JOB_ACTIVE'}]})
})
it('halts remaining writes after an unknown transport outcome and requires refresh',async()=>{
    const {api,store}=setup()
    api.edit.mockRejectedValueOnce(new Error('connection lost'))
    const result=await store.bulk(selection,{kind:'favorite',value:true},vi.fn())
    expect(result?.succeeded).toEqual([]);expect(result?.failed.map(item=>item.mediaId)).toEqual([1,2]);expect(api.get).toHaveBeenCalledTimes(1)
    expect(store.getSnapshot().needsRefresh).toBe(true);expect(store.getSnapshot().busy).toBe(false)
    expect(await store.bulk(selection,{kind:'unregister'},vi.fn())).toBeNull()
})
it('does not continue after disposal while waiting for a target read',async()=>{
    const {api,store}=setup(), pending=deferred<any>()
    api.get.mockReturnValueOnce(pending.promise)
    const running=store.bulk(selection,{kind:'unregister'},vi.fn());store.cancelReads();pending.resolve(ok(detail(1)))
    expect((await running)?.failed).toHaveLength(2);expect(api.unregisterMedia).not.toHaveBeenCalled();expect(store.getSnapshot().needsRefresh).toBe(true)
})
it('opens each Windows folder once despite case and slash differences, and reports missing files',async()=>{
    const {api,store}=setup(), open=vi.fn(async()=>{})
    api.get.mockResolvedValueOnce(ok({...detail(1),files:{data:[{filepath:'C:\\Folder\\one.png'}]}})).mockResolvedValueOnce(ok({...detail(2),files:{data:[{filepath:'c:/folder/two.png'}]}}))
    expect((await store.bulk(selection,{kind:'openFolders',open},vi.fn()))?.succeeded).toEqual([1,2]);expect(open).toHaveBeenCalledOnce();expect(api.list).not.toHaveBeenCalled()
    api.get.mockResolvedValueOnce(ok({...detail(1),files:{data:[]}}))
    expect((await store.bulk(selection.slice(0,1),{kind:'openFolders',open},vi.fn()))?.failed).toEqual([{mediaId:1,message:'등록된 파일 경로가 없습니다.'}])
})
it('reports read errors per target and requires refresh if the final reload fails',async()=>{
    const {api,store}=setup()
    api.get.mockResolvedValueOnce(fail('NOT_FOUND') as any);api.list.mockResolvedValueOnce(fail('NOT_READY') as any)
    const result=await store.bulk(selection,{kind:'favorite',value:true},vi.fn())
    expect(result?.failed).toEqual([{mediaId:1,message:'NOT_FOUND'}]);expect(result?.succeeded).toEqual([2]);expect(store.getSnapshot().needsRefresh).toBe(true)
})
it('ignores empty requests',async()=>{
    const {api,store}=setup();expect(await store.bulk([],{kind:'unregister'},vi.fn())).toBeNull();expect(api.get).not.toHaveBeenCalled()
})
it('stops a pending batch when the gallery filter changes',async()=>{
    const {api,store}=setup(),pending=deferred<any>()
    api.get.mockReturnValueOnce(pending.promise)
    const running=store.bulk(selection,{kind:'favorite',value:true},vi.fn())
    store.setFilter({keyword:'다른 필터'} as any);pending.resolve(ok(detail(1)))
    expect((await running)?.failed).toHaveLength(2);expect(api.edit).not.toHaveBeenCalled()
})
