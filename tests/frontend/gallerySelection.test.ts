// @vitest-environment happy-dom
import { act, createElement as h } from 'react'
import { afterEach, expect, it, vi } from 'vitest'
import { cleanupReact, clickButton, mount } from './reactHarness'
import { GalleryPage } from '@renderer/features/gallery/GalleryPage'
const mocks = vi.hoisted(() => ({ unregister: vi.fn(), confirm: vi.fn(async () => true), refresh: vi.fn(), getSnapshot: vi.fn(), get: vi.fn(), create: vi.fn(), run: vi.fn(), directories: vi.fn(), on: vi.fn(), state: null as any }))
const api = {fileMove:{get:mocks.get,create:mocks.create,run:mocks.run},media:{directories:mocks.directories},onMediaThumbnailUpdated:mocks.on}
vi.mock('@renderer/shared/hooks/useConfirm',()=>({useConfirm:()=>({confirm:mocks.confirm,isOpen:false})}))
vi.mock('@renderer/shared/api',()=>({getApi:()=>api}))
vi.mock('@renderer/features/gallery/useGalleryViewModel',()=>({useGalleryViewModel:()=>({store:{refresh:mocks.refresh,getSnapshot:mocks.getSnapshot,unregisterMedia:mocks.unregister},snapshot:mocks.state,openInFolder:vi.fn()})}))
vi.mock('@renderer/shared/hooks/useLeaveGuard',()=>({useLeaveGuard:vi.fn()}))
vi.mock('@renderer/shared/hooks/useInfiniteScroll',()=>({useInfiniteScroll:vi.fn()}))
vi.mock('@renderer/features/localimport/LocalImportPanel',()=>({LocalImportPanel:()=>null}))
afterEach(cleanupReact)
function setup(){
 mocks.confirm.mockReset().mockResolvedValue(true);mocks.unregister.mockReset().mockResolvedValue(true)
 mocks.state={busy:false,needsRefresh:false,detail:{mediaId:null},list:{status:'ready',page:{data:[1,2].map(id=>({id,title:'게시글 '+id,sourceKind:'download',revision:4,createdAt:1,isFavorite:false,fileCount:2})),total:2,hasNextPage:false}}}
 mocks.getSnapshot.mockImplementation(()=>mocks.state);mocks.on.mockReturnValue(()=>{});mocks.get.mockResolvedValue({ok:true,data:null});mocks.directories.mockResolvedValue({ok:true,data:{data:[{id:7,directoryPath:'C:/B'}],hasNextPage:false}});mocks.create.mockResolvedValue({ok:true,data:{jobId:'job'}});mocks.run.mockResolvedValue({ok:true,data:{jobId:'job'}})
}
it('connects gallery checkboxes to one multi-post move request',async()=>{
 setup()
 await mount(h(GalleryPage))
 const checks=[...document.querySelectorAll<HTMLInputElement>('input[type=checkbox]')];expect(checks).toHaveLength(2)
 await act(async()=>{checks[0].click();checks[1].click()});expect(document.body.textContent).toContain('게시글 2개 선택')
 await clickButton('파일 이동');const select=document.querySelector('select')!;await act(async()=>{select.value='7';select.dispatchEvent(new Event('change',{bubbles:true}))});await clickButton('이동 시작')
 expect(mocks.create).toHaveBeenCalledWith({selection:[{mediaId:1,revision:4},{mediaId:2,revision:4}],directoryId:7});expect(document.body.textContent).toContain('게시글 0개 선택')
})

it('requires approval before deleting a gallery card registration',async()=>{
 setup();mocks.confirm.mockResolvedValueOnce(false);await mount(h(GalleryPage));await clickButton('게시글 1 더보기');await clickButton('등록 해제');expect(mocks.unregister).not.toHaveBeenCalled()
 await clickButton('게시글 1 더보기');await clickButton('등록 해제');expect(mocks.unregister).toHaveBeenCalledExactlyOnceWith({mediaId:1,expectedRevision:4});expect(mocks.confirm).toHaveBeenLastCalledWith(expect.objectContaining({title:'묶음 등록 해제',danger:true}))
})
it('drops a gallery registration approval when its target changes or the view closes',async()=>{
 setup();let answer!:(accepted:boolean)=>void;mocks.confirm.mockImplementationOnce(()=>new Promise<boolean>(resolve=>answer=resolve))
 const stop=await mount(h(GalleryPage));await clickButton('게시글 1 더보기');await clickButton('등록 해제');mocks.state.list.page.data[0]={...mocks.state.list.page.data[0],revision:5};await act(async()=>answer(true));expect(mocks.unregister).not.toHaveBeenCalled()
 mocks.confirm.mockImplementationOnce(()=>new Promise<boolean>(resolve=>answer=resolve));await clickButton('게시글 2 더보기');await clickButton('등록 해제');await stop();await act(async()=>answer(true));expect(mocks.unregister).not.toHaveBeenCalled()
})
