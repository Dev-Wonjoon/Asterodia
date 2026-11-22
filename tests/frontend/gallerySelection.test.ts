// @vitest-environment happy-dom
import { act, createElement as h } from 'react'
import { afterEach, expect, it, vi } from 'vitest'
import { cleanupReact, clickButton, mount } from './reactHarness'
import { GalleryPage } from '@renderer/features/gallery/GalleryPage'
import { useInfiniteScroll } from '@renderer/shared/hooks/useInfiniteScroll'
const mocks = vi.hoisted(() => ({ bulk: vi.fn(), unregister: vi.fn(), confirm: vi.fn(async () => true), refresh: vi.fn(), getSnapshot: vi.fn(), get: vi.fn(), create: vi.fn(), run: vi.fn(), directories: vi.fn(), on: vi.fn(), state: null as any }))
const api = {fileMove:{get:mocks.get,create:mocks.create,run:mocks.run},media:{directories:mocks.directories},onMediaThumbnailUpdated:mocks.on}
vi.mock('@renderer/shared/hooks/useConfirm',()=>({useConfirm:()=>({confirm:mocks.confirm,isOpen:false})}))
vi.mock('@renderer/shared/api',()=>({getApi:()=>api}))
const store = {refresh:mocks.refresh,getSnapshot:mocks.getSnapshot,unregisterMedia:mocks.unregister,bulk:mocks.bulk,closeDetail:vi.fn(),toggleAlbum:vi.fn(),loadMore:vi.fn(),openDetail:vi.fn()}
const downloads = { activeJobs: [] as any[], cancellingIds: [], openDetail: vi.fn(), closeDetail: vi.fn(), cancelDownload: vi.fn() }
vi.mock('@renderer/features/gallery/useGalleryViewModel',()=>({useGalleryViewModel:()=>({store,snapshot:mocks.state,openInFolder:vi.fn(),downloads})}))
vi.mock('@renderer/shared/hooks/useLeaveGuard',()=>({useLeaveGuard:vi.fn()}))
vi.mock('@renderer/shared/hooks/useInfiniteScroll',()=>({useInfiniteScroll:vi.fn()}))
vi.mock('@renderer/features/download/components/DownloadThumbnail',()=>({DownloadThumbnail:()=>h('div',null,'작업 썸네일')}))
vi.mock('@renderer/features/download/components/DownloadDetailPane',()=>({DownloadDetailPane:()=>h('div',null,'선택한 다운로드 작업')}))
vi.mock('@renderer/features/localimport/LocalImportPanel',()=>({LocalImportPanel:()=>null}))
afterEach(cleanupReact)
function setup(){
 downloads.activeJobs=[]; downloads.openDetail.mockClear(); downloads.closeDetail.mockClear(); downloads.cancelDownload.mockClear()
 mocks.confirm.mockReset().mockResolvedValue(true);mocks.unregister.mockReset().mockResolvedValue(true)
 mocks.bulk.mockReset().mockResolvedValue({succeeded:[1],failed:[{mediaId:2,message:'시험 오류'}]})
 mocks.state={busy:false,needsRefresh:false,detail:{mediaId:null},list:{status:'ready',page:{data:[1,2].map(id=>({id,title:'게시글 '+id,sourceKind:'download',revision:4,createdAt:1,isFavorite:false,fileCount:2})),total:2,hasNextPage:false}}}
 mocks.getSnapshot.mockImplementation(()=>mocks.state);mocks.on.mockReturnValue(()=>{});mocks.get.mockResolvedValue({ok:true,data:null});mocks.directories.mockResolvedValue({ok:true,data:{data:[{id:7,directoryPath:'C:/B'}],hasNextPage:false}});mocks.create.mockResolvedValue({ok:true,data:{jobId:'job'}});mocks.run.mockResolvedValue({ok:true,data:{jobId:'job'}})
}
it('shows active downloads before registered media and connects cancellation and detail', async () => {
 setup(); downloads.activeJobs=[{id:'queued',title:'새 영상',url:'https://example.invalid/a',state:'pending',phase:null,progress:0,cleanupRequired:false}]
 await mount(h(GalleryPage))
 expect([...document.querySelectorAll('article')].map(item=>item.textContent)).toEqual([expect.stringContaining('새 영상'),expect.stringContaining('게시글 1'),expect.stringContaining('게시글 2')])
 await clickButton('다운로드 취소');expect(downloads.cancelDownload).toHaveBeenCalledExactlyOnceWith('queued')
 await clickButton('새 영상 다운로드 상세보기');expect(downloads.openDetail).toHaveBeenCalledExactlyOnceWith('queued')
 expect(document.body.textContent).toContain('선택한 다운로드 작업')
})
it('does not mix queued jobs into favorites or search results', async () => {
 setup(); downloads.activeJobs=[{id:'queued',title:'새 영상'}]
 const stop=await mount(h(GalleryPage,{favoritesOnly:true}));expect(document.body.textContent).not.toContain('새 영상');await stop()
 await mount(h(GalleryPage,{filter:{keyword:'검색'}}));expect(document.body.textContent).not.toContain('새 영상')
})
it('connects gallery checkboxes to one multi-post move request',async()=>{
 setup()
 await mount(h(GalleryPage))
 expect(document.querySelectorAll('input[type=checkbox]')).toHaveLength(2)
 const checks=[...document.querySelectorAll<HTMLInputElement>('input[type=checkbox]')];expect(checks).toHaveLength(2)
 await act(async()=>{checks[0].click();checks[1].click()});expect(document.querySelector('[aria-label="전체 2 / 선택 2"]')?.textContent).toBe('2 / 2')
 await clickButton('선택 작업');await clickButton('한 폴더에 모으기');const select=document.querySelector('select[aria-label="이동할 폴더"]')!;await act(async()=>{select.value='7';select.dispatchEvent(new Event('change',{bubbles:true}))});await clickButton('복사 시작')
 expect(mocks.create).toHaveBeenCalledWith({selection:[{mediaId:1,revision:4},{mediaId:2,revision:4}],directoryId:7,operation:'copy'});expect(document.querySelector('header [role=status]')).toBeNull()
})

it('selects the loaded list, keeps failures selected and clears selection',async()=>{
 setup();await mount(h(GalleryPage));await clickButton('메뉴');await clickButton('현재 목록 전체 선택')
 await clickButton('선택 작업');expect(document.body.textContent).not.toContain('제목 편집');await clickButton('즐겨찾기 추가')
 expect(mocks.bulk).toHaveBeenCalledWith([{mediaId:1,revision:4},{mediaId:2,revision:4}],{kind:'favorite',value:true},expect.any(Function))
 expect(document.body.textContent).toContain('성공 1개 · 실패 1개');expect(document.querySelector('[aria-label="전체 2 / 선택 1"]')?.textContent).toBe('2 / 1')
 expect([...document.querySelectorAll<HTMLInputElement>('input[type=checkbox]')].map(item=>item.checked)).toEqual([false,true])
 await clickButton('메뉴');await clickButton('선택 해제');expect([...document.querySelectorAll('button')].find(item=>item.textContent==='선택 작업')).toBeUndefined()
 expect(document.querySelectorAll('input[type=checkbox]')).toHaveLength(2)
})

it('adds and removes named tags through one dialog without a title bulk action',async()=>{
 setup();await mount(h(GalleryPage));await clickButton('메뉴');await clickButton('현재 목록 전체 선택')
 await clickButton('선택 작업');await clickButton('태그 추가')
 const input=document.querySelector('textarea')!;await act(async()=>{const setter=Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype,'value')!.set!;setter.call(input,'새 태그, 새 태그\n다른 태그');input.dispatchEvent(new Event('input',{bubbles:true}))});await clickButton('적용')
 expect(mocks.bulk).toHaveBeenLastCalledWith(expect.any(Array),{kind:'tags',names:['새 태그','다른 태그'],remove:false},expect.any(Function))
 await clickButton('선택 작업');await clickButton('태그 제거');await clickButton('취소');expect(document.querySelector('[role=dialog]')).toBeNull()
})

it('supports menu keyboard navigation and escape',async()=>{
 setup();await mount(h(GalleryPage));await clickButton('메뉴')
 expect(document.activeElement?.textContent).toBe('폴더 가져오기')
 await act(async()=>document.activeElement?.dispatchEvent(new KeyboardEvent('keydown',{key:'End',bubbles:true})))
 expect(document.activeElement?.textContent).toBe('현재 목록 전체 선택')
 await act(async()=>document.activeElement?.dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true})))
 expect(document.querySelector('[role=menu]')).toBeNull();expect(document.activeElement?.textContent).toBe('메뉴')
})

it('keeps refresh available after a list error while blocking selection',async()=>{
 setup();mocks.refresh.mockClear();mocks.state.list.status='error';mocks.state.list.error={message:'목록 오류'}
 await mount(h(GalleryPage));await clickButton('새로고침');expect(mocks.refresh).toHaveBeenCalledOnce()
 await clickButton('메뉴');expect([...document.querySelectorAll('button')].find(item=>item.textContent==='현재 목록 전체 선택')?.disabled).toBe(true)
})

it('shows selection actions and counts on the left only while items are selected',async()=>{
 setup();await mount(h(GalleryPage))
 const header=document.querySelector('header')!
 const initialControls=[...header.querySelectorAll('button')].map(item=>item.textContent)
 expect(initialControls).toEqual(['미디어 편집 작업','새로고침','메뉴'])
 expect(header.querySelector('[role=status]')).toBeNull();expect(header.querySelector('.invisible')).toBeNull()
 expect(header.textContent).not.toContain('묶음');expect(header.textContent).not.toContain('게시글')
 await act(async()=>document.querySelector<HTMLInputElement>('input[type=checkbox]')!.click())
 expect(header.querySelector('[role=status]')?.textContent).toBe('2 / 1')
 expect([...document.querySelectorAll('button')].some(item=>item.getAttribute('aria-label')==='게시글 1 더보기')).toBe(true)
 expect([...header.querySelectorAll('button')].map(item=>item.textContent)).toEqual(['미디어 편집 작업','선택 작업','새로고침','메뉴'])
 expect(header.textContent!.indexOf('선택 작업')).toBeLessThan(header.textContent!.indexOf('2 / 1'))
 expect(header.textContent!.indexOf('2 / 1')).toBeLessThan(header.textContent!.indexOf('새로고침'))
 await clickButton('메뉴');await clickButton('선택 해제')
 expect(header.querySelector('[role=status]')).toBeNull()
 expect([...header.querySelectorAll('button')].map(item=>item.textContent)).toEqual(initialControls)
})

it('offers direct selection without selection-mode menu entries or checkbox text',async()=>{
 setup();await mount(h(GalleryPage))
 const checks=[...document.querySelectorAll<HTMLInputElement>('input[type=checkbox]')]
 expect(checks).toHaveLength(2);expect(checks.every(item=>item.closest('label')?.textContent==='')).toBe(true)
 await clickButton('메뉴')
 const items=[...document.querySelectorAll('[role=menuitem]')].map(item=>item.textContent)
 expect(items).toContain('현재 목록 전체 선택');expect(items).toContain('선택 해제')
 expect(items).not.toContain('선택');expect(items).not.toContain('선택 모드 종료')
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

it('opens an expanded file at its own offset and retains infinite scrolling with a manual retry',async()=>{
 setup();store.openDetail.mockClear();store.toggleAlbum.mockClear();store.loadMore.mockClear()
 mocks.state.list.page={data:[{id:1,title:'앨범',sourceKind:'local',revision:4,createdAt:1,fileCount:100,galleryFileOffset:51,galleryFile:{id:52,title:'선택 영상',kind:'video',thumbnailPath:null,previewPath:null}}],total:101,offset:48,hasNextPage:true}
 await mount(h(GalleryPage))
 expect(document.querySelectorAll('article')).toHaveLength(1)
 expect(document.querySelector('input[type=checkbox]')?.getAttribute('aria-label')).toContain('앨범 추가 선택')
 await clickButton('선택 영상 상세보기');expect(store.openDetail).toHaveBeenCalledExactlyOnceWith(1,51)
 await clickButton('앨범 앨범 닫기');expect(store.toggleAlbum).toHaveBeenCalledExactlyOnceWith(1)
 await clickButton('더 불러오기');expect(store.loadMore).toHaveBeenCalledOnce()
 const options=vi.mocked(useInfiniteScroll).mock.lastCall![0]
 expect(options.enabled).toBe(true)
 await options.onLoadMore();expect(store.loadMore).toHaveBeenCalledTimes(2)
 expect(document.querySelector('[aria-label="갤러리 페이지"]')).toBeNull()
 expect(document.body.textContent).toContain('1 / 101개 항목')
})
