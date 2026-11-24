// @vitest-environment happy-dom
import { act, createElement as h } from 'react'
import { afterEach, expect, it, vi } from 'vitest'
import { cleanupReact, clickButton, deferred, mount } from './reactHarness'
import { GalleryPage } from '@renderer/features/gallery/GalleryPage'
import { albumDragType } from '@renderer/features/gallery/useGallerySelection'
import { useInfiniteScroll } from '@renderer/shared/hooks/useInfiniteScroll'
import { useGalleryViewModel } from '@renderer/features/gallery/useGalleryViewModel'
const mocks = vi.hoisted(() => ({ bulk: vi.fn(), unregister: vi.fn(), confirm: vi.fn(async () => true), refresh: vi.fn(), getSnapshot: vi.fn(), get: vi.fn(), create: vi.fn(), run: vi.fn(), directories: vi.fn(), on: vi.fn(), mediaGet: vi.fn(), mountEditor: vi.fn(), destroyEditor: vi.fn(), state: null as any }))
const api = {fileMove:{get:mocks.get,create:mocks.create,run:mocks.run},media:{directories:mocks.directories,albums:vi.fn(),saveAlbum:vi.fn(),mergeAlbum:vi.fn(),get:mocks.mediaGet},onMediaThumbnailUpdated:mocks.on,showItemInFolder:vi.fn()}
vi.mock('../../internal/webserver/web/editor.js',()=>({mountMediaEditor:mocks.mountEditor}))
vi.mock('@renderer/shared/hooks/useConfirm',()=>({useConfirm:()=>({confirm:mocks.confirm,isOpen:false})}))
vi.mock('@renderer/shared/api',()=>({getApi:()=>api}))
const store = {refresh:mocks.refresh,getSnapshot:mocks.getSnapshot,unregisterMedia:mocks.unregister,bulk:mocks.bulk,closeDetail:vi.fn(),toggleAlbum:vi.fn(),loadMore:vi.fn(),openDetail:vi.fn(),edit:vi.fn()}
const downloads = { activeJobs: [] as any[], cancellingIds: [], openDetail: vi.fn(), closeDetail: vi.fn(), cancelDownload: vi.fn() }
vi.mock('@renderer/features/gallery/useGalleryViewModel',()=>({useGalleryViewModel:vi.fn(()=>({store,snapshot:mocks.state,openInFolder:vi.fn(),downloads}))}))
vi.mock('@renderer/shared/hooks/useLeaveGuard',()=>({useLeaveGuard:vi.fn()}))
vi.mock('@renderer/shared/hooks/useInfiniteScroll',()=>({useInfiniteScroll:vi.fn()}))
vi.mock('@renderer/features/download/components/DownloadThumbnail',()=>({DownloadThumbnail:()=>h('div',null,'작업 썸네일')}))
vi.mock('@renderer/features/download/components/DownloadDetailPane',()=>({DownloadDetailPane:()=>h('div',null,'선택한 다운로드 작업')}))
vi.mock('@renderer/features/localimport/LocalImportPanel',()=>({LocalImportPanel:()=>null}))
vi.mock('@renderer/features/gallery/MediaViewer',()=>({MediaViewer:({mediaId,fileId,onClose}:any)=>h('div',{role:'dialog','aria-label':`감상 ${mediaId} ${fileId ?? ''}`},h('button',{onClick:onClose},'감상 닫기'))}))
afterEach(cleanupReact)

it('navigates cards without changing selection and selects a range with Shift', async () => {
 setup(); await mount(h(GalleryPage))
 const cards = [...document.querySelectorAll<HTMLButtonElement>('[data-card-focus]')]
 const press = async (value: string, shiftKey = false) => { await act(async () => { document.activeElement!.dispatchEvent(new KeyboardEvent('keydown', { key: value, shiftKey, bubbles: true, cancelable: true })) }) }
 cards[0].focus(); expect(cards[0].tabIndex).toBe(0); expect(cards[1].tabIndex).toBe(-1)
 await press('ArrowRight'); expect(document.activeElement).toBe(cards[1]); expect(document.querySelector('[aria-label="선택 항목 작업"]')).toBeNull()
 await press('Home'); await press('ArrowRight', true)
 expect(document.querySelector('[aria-label="선택 항목 작업"]')?.textContent).toContain('2개 선택')
 await press('Escape'); expect(document.querySelector('[aria-label="선택 항목 작업"]')).toBeNull()
 await press(' '); expect(document.querySelector('[aria-label="선택 항목 작업"]')?.textContent).toContain('1개 선택')
})
it('opens card actions with the keyboard and protects typing and underlying selection under a menu', async () => {
 setup(); await mount(h(GalleryPage))
 const card = document.querySelector<HTMLButtonElement>('[data-card-focus]')!; card.focus()
 await act(async () => { card.dispatchEvent(new KeyboardEvent('keydown', { key: ' ', bubbles: true, cancelable: true })) })
 await act(async () => { card.dispatchEvent(new KeyboardEvent('keydown', { key: 'F10', shiftKey: true, bubbles: true, cancelable: true })) })
 const menu = document.querySelector<HTMLElement>('[role="menu"]')!; expect(menu).not.toBeNull()
 await act(async () => { menu.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true })) })
 expect(document.querySelector('[role="menu"]')).toBeNull(); expect(document.querySelector('[aria-label="선택 항목 작업"]')).not.toBeNull()
 expect(mocks.unregister).not.toHaveBeenCalled()
})
function setup(){
 store.edit.mockReset().mockResolvedValue(true)
 mocks.refresh.mockReset()
 vi.mocked(useGalleryViewModel).mockClear()
 mocks.mediaGet.mockReset().mockResolvedValue({ok:false,error:{code:'NOT_FOUND',message:'찾지 못했습니다.'}});mocks.destroyEditor.mockReset();api.showItemInFolder.mockReset()
 mocks.mountEditor.mockReset().mockImplementation((host:HTMLElement,options:any)=>{
  const dialog=document.createElement('dialog');dialog.open=true;dialog.setAttribute('aria-label','편집 테스트 창')
  const close=document.createElement('button');close.textContent='테스트 편집기 닫기';close.onclick=options.onClose;dialog.append(close);host.append(dialog)
  return {destroy:()=>{mocks.destroyEditor();dialog.remove()}}
 })
 api.media.albums.mockReset().mockResolvedValue({ok:true,data:{data:[{id:9,title:'여행',revision:3,fileCount:0,isUserAlbum:true}],hasNextPage:false}})
 api.media.saveAlbum.mockReset().mockResolvedValue({ok:true,data:{id:9,revision:4}})
 downloads.activeJobs=[]; downloads.openDetail.mockClear(); downloads.closeDetail.mockClear(); downloads.cancelDownload.mockClear()
 mocks.confirm.mockReset().mockResolvedValue(true);mocks.unregister.mockReset().mockResolvedValue(true)
 mocks.bulk.mockReset().mockResolvedValue({succeeded:[1],failed:[{mediaId:2,message:'시험 오류'}]})
 mocks.state={busy:false,needsRefresh:false,detail:{mediaId:null},list:{status:'ready',page:{data:[1,2].map(id=>({id,title:'게시글 '+id,sourceKind:'download',revision:4,createdAt:1,isFavorite:false,fileCount:2})),total:2,hasNextPage:false}}}
 mocks.getSnapshot.mockImplementation(()=>mocks.state);mocks.on.mockReturnValue(()=>{});mocks.get.mockResolvedValue({ok:true,data:null});mocks.directories.mockResolvedValue({ok:true,data:{data:[{id:7,directoryPath:'C:/B'}],hasNextPage:false}});mocks.create.mockResolvedValue({ok:true,data:{jobId:'job'}});mocks.run.mockResolvedValue({ok:true,data:{jobId:'job'}})
}
function singleVideo(){
 const file={id:11,mediaId:1,title:'선택한 동영상',fileKey:'video',filepath:'C:/fixture/video.mp4',duration:2.5,kind:'video',detectedFormat:'mp4'}
 const media=mocks.state.list.page.data[0]
 Object.assign(media,{fileCount:1,cover:{fileId:file.id,path:null,kind:'video'}})
 const result={ok:true,data:{...media,files:{data:[file],total:1,hasNextPage:false}}}
 mocks.mediaGet.mockResolvedValue(result)
 return {file,result}
}
it('pins groups and individual files through the direct card action',async()=>{
 setup();const {file}=singleVideo()
 Object.assign(mocks.state.list.page.data[1],{isPinned:true,galleryFile:{...file,id:12,title:'고정한 파일'}})
 await mount(h(GalleryPage))
 await clickButton('게시글 1 고정')
 expect(store.edit).toHaveBeenLastCalledWith({mediaId:1,expectedRevision:4,isPinned:true})
 await clickButton('고정한 파일 고정 해제')
 expect(store.edit).toHaveBeenLastCalledWith({mediaId:2,expectedRevision:4,isPinned:false,pinFileId:12})
 expect(document.querySelector('[role=menu]')).toBeNull()
})
it('opens the animation editor directly from the card film icon',async()=>{
 setup();singleVideo();await mount(h(GalleryPage))
 await clickButton('게시글 1 GIF, WebP로 만들기')
 expect(document.querySelector('[aria-label="편집 테스트 창"]')).not.toBeNull()
 expect(mocks.mountEditor.mock.lastCall![1].file.id).toBe(11)
})
it('places pinned cards ahead of active downloads in the same scrolling grid',async()=>{
 setup();Object.assign(mocks.state.list.page.data[0],{isPinned:true,galleryPinned:true})
 downloads.activeJobs=[{id:'queued',title:'새 영상',state:'pending',phase:'queued',downloadedBytes:0,revision:1}]
 await mount(h(GalleryPage))
 expect([...document.querySelectorAll('article')].map(item=>item.textContent)).toEqual([expect.stringContaining('게시글 1'),expect.stringContaining('새 영상'),expect.stringContaining('게시글 2')])
})
it('switches gallery views, clears selection and exposes hidden files through the view menu',async()=>{
 setup();await mount(h(GalleryPage))
 expect(vi.mocked(useGalleryViewModel).mock.lastCall).toEqual([false,undefined,'all',false])
 await clickButton('메뉴');await clickButton('현재 목록 전체 선택')
 expect(document.querySelector('[aria-label="선택 항목 작업"]')).not.toBeNull()
 await clickButton('전체 보기');expect(document.querySelector('[role=menuitemradio][aria-checked=true]')?.textContent).toBe('전체')
 await clickButton('앨범만')
 expect(vi.mocked(useGalleryViewModel).mock.lastCall).toEqual([false,undefined,'albums',false])
 expect(document.querySelector('[aria-label="선택 항목 작업"]')).toBeNull()
 await clickButton('앨범만 보기')
 expect(document.querySelector<HTMLButtonElement>('[role=menuitemcheckbox]')!.disabled).toBe(true)
 await clickButton('파일만');await clickButton('파일만 보기');await clickButton('숨긴 파일 표시')
 expect(vi.mocked(useGalleryViewModel).mock.lastCall).toEqual([false,undefined,'files',true])
 await clickButton('파일만 보기');expect(document.querySelector('[role=menuitemcheckbox]')?.getAttribute('aria-checked')).toBe('true')
 await clickButton('전체')
 expect(vi.mocked(useGalleryViewModel).mock.lastCall).toEqual([false,undefined,'all',true])
})
it('keeps favorites and search constraints while changing the gallery view',async()=>{
 setup();await mount(h(GalleryPage,{favoritesOnly:true,filter:{keyword:'여행'}}))
 await clickButton('전체 보기');await clickButton('파일만')
 expect(vi.mocked(useGalleryViewModel).mock.lastCall).toEqual([true,{keyword:'여행'},'files',false])
})
it('disables view changes while a library mutation is running',async()=>{
 setup();mocks.state.busy=true;await mount(h(GalleryPage))
 expect(document.querySelector<HTMLButtonElement>('button[aria-label="전체 보기"]')!.disabled).toBe(true)
})
it('does not offer collapse for file cards forced by visibility or file-only mode',async()=>{
 setup();Object.assign(mocks.state.list.page.data[0],{galleryFile:{id:18,title:'남은 파일',kind:'image'},galleryCanCollapse:false})
 await mount(h(GalleryPage));expect(document.querySelector('[aria-label="게시글 1 파일 접기"]')).toBeNull()
 await clickButton('남은 파일 더보기')
 expect([...document.querySelectorAll('[role=menuitem]')].map(item=>item.textContent)).not.toContain('파일 접기')
})
it('opens the renamed editor from the main menu and keeps it mounted after the menu closes',async()=>{
 setup();await mount(h(GalleryPage))
 expect(document.body.textContent).not.toContain('GIF, WebP로 만들기')
 expect(document.body.textContent).not.toContain('미디어 편집 작업')
 await clickButton('메뉴');await clickButton('GIF, WebP로 만들기')
 expect(document.querySelector('[role=menu]')).toBeNull()
 expect(document.querySelector('[aria-label="편집 테스트 창"]')).not.toBeNull()
 expect(mocks.mountEditor).toHaveBeenCalledOnce()
 expect(mocks.mountEditor.mock.lastCall![1].file).toBeUndefined()
 expect(mocks.destroyEditor).not.toHaveBeenCalled()
 await clickButton('테스트 편집기 닫기')
 expect(mocks.destroyEditor).toHaveBeenCalledOnce()
 expect(document.querySelector('[aria-label="편집 테스트 창"]')).toBeNull()
})
it('loads a single video from its card and opens a blank editor separately from the main menu',async()=>{
 setup();const {file}=singleVideo();await mount(h(GalleryPage))
 await clickButton('게시글 1 더보기');await clickButton('GIF, WebP로 만들기')
 expect(mocks.mediaGet).toHaveBeenCalledExactlyOnceWith({mediaId:1,fileLimit:1,fileOffset:0})
 expect(document.querySelector('[role=menu]')).toBeNull()
 expect(document.querySelector('[aria-label="편집 테스트 창"]')).not.toBeNull()
 expect(mocks.mountEditor.mock.lastCall![1].file).toEqual(expect.objectContaining({id:file.id,title:file.title,duration:file.duration,kind:'video',src:'/media?path='+encodeURIComponent(file.filepath)}))
 expect(mocks.destroyEditor).not.toHaveBeenCalled()
 await clickButton('테스트 편집기 닫기');await clickButton('메뉴');await clickButton('GIF, WebP로 만들기')
 expect(mocks.mountEditor).toHaveBeenCalledTimes(2)
 expect(mocks.mountEditor.mock.lastCall![1].file).toBeUndefined()
})
it('passes an expanded video directly to the editor without fetching the first file in its group',async()=>{
 setup();const {file}=singleVideo()
 Object.assign(mocks.state.list.page.data[0],{fileCount:20,galleryFileOffset:7,galleryFile:{...file,id:18,title:'여덟 번째 동영상'}})
 await mount(h(GalleryPage));await clickButton('여덟 번째 동영상 더보기');await clickButton('GIF, WebP로 만들기')
 expect(mocks.mediaGet).not.toHaveBeenCalled()
 expect(mocks.mountEditor.mock.lastCall![1].file).toEqual(expect.objectContaining({id:18,title:'여덟 번째 동영상'}))
 expect(document.querySelector('[aria-label="편집 테스트 창"]')).not.toBeNull()
})
it.each(['image','audio','group'])('does not offer a video conversion for a %s card',async kind=>{
 setup();const {file}=singleVideo()
 Object.assign(mocks.state.list.page.data[0],kind==='group'?{fileCount:2}:{galleryFile:{...file,kind}})
 await mount(h(GalleryPage));await clickButton(kind==='group'?'게시글 1 더보기':file.title+' 더보기')
 expect(document.querySelector('[role=menu]')?.textContent).not.toContain('GIF, WebP로 만들기')
 expect(mocks.mountEditor).not.toHaveBeenCalled()
})
it('shows a video loading failure and allows another attempt',async()=>{
 setup();singleVideo();mocks.mediaGet.mockResolvedValueOnce({ok:false,error:{message:'파일을 불러올 수 없습니다.'}})
 await mount(h(GalleryPage));await clickButton('게시글 1 더보기');await clickButton('GIF, WebP로 만들기')
 expect(document.querySelector('[role=alert]')?.textContent).toBe('파일을 불러올 수 없습니다.')
 expect(mocks.mountEditor).not.toHaveBeenCalled()
 await clickButton('게시글 1 더보기');await clickButton('GIF, WebP로 만들기')
 expect(document.querySelector('[role=alert]')).toBeNull()
 expect(mocks.mountEditor).toHaveBeenCalledOnce()
})
it.each(['revision','file'])('rejects a changed %s instead of editing a different video',async change=>{
 setup();const {result}=singleVideo()
 if(change==='revision')result.data.revision++
 else result.data.files.data[0].id++
 await mount(h(GalleryPage));await clickButton('게시글 1 더보기');await clickButton('GIF, WebP로 만들기')
 expect(document.querySelector('[role=alert]')?.textContent).toContain('미디어가 변경되었습니다.')
 expect(mocks.mountEditor).not.toHaveBeenCalled()
})
it.each(['changed','closed'])('ignores a pending video response after its target is %s',async change=>{
 setup();const {result}=singleVideo(),pending=deferred<typeof result>()
 mocks.mediaGet.mockReturnValueOnce(pending.promise)
 const stop=await mount(h(GalleryPage));await clickButton('게시글 1 더보기');await clickButton('GIF, WebP로 만들기')
 expect(document.querySelector('[role=status]')?.textContent).toBe('동영상 불러오는 중…')
 await clickButton('메뉴')
 expect([...document.querySelectorAll<HTMLButtonElement>('[role=menuitem]')].find(item=>item.textContent==='GIF, WebP로 만들기')?.disabled).toBe(true)
 if(change==='closed')await stop()
 else mocks.state.list.page.data[0]={...mocks.state.list.page.data[0],revision:5}
 await act(async()=>pending.resolve(result))
 expect(mocks.mountEditor).not.toHaveBeenCalled()
 expect(document.body.textContent).not.toContain('동영상 불러오는 중…')
})
it('hides the completed gallery footer while preserving loading and retry feedback',async()=>{
 setup();const stop=await mount(h(GalleryPage))
 expect(document.body.textContent).not.toContain('개 항목')
 expect(document.body.textContent).not.toContain('모든 항목을 불러왔습니다.')
 expect([...document.querySelectorAll('button')].some(item=>item.textContent==='더 불러오기')).toBe(false)
 await stop();mocks.state.list.loadingMore=true;mocks.state.list.page.hasNextPage=true
 const stopLoading=await mount(h(GalleryPage))
 expect(document.querySelector('[role=status]')?.textContent).toBe('불러오는 중…')
 expect([...document.querySelectorAll('button')].find(item=>item.textContent==='더 불러오기')?.disabled).toBe(true)
 await stopLoading();mocks.state.list.loadingMore=false;mocks.state.list.moreError={message:'추가 목록 오류'};store.loadMore.mockClear()
 await mount(h(GalleryPage))
 expect(document.querySelector('[role=alert]')?.textContent).toBe('추가 목록 오류')
 await clickButton('다시 시도');expect(store.loadMore).toHaveBeenCalledOnce()
 expect(vi.mocked(useInfiniteScroll).mock.lastCall![0].enabled).toBe(false)
})
it.each([1, 3])('adds a download with %i files from its card without opening details', async count => {
 setup();mocks.state.list.page.data[0].fileCount=count;store.openDetail.mockClear()
 await mount(h(GalleryPage))
 await clickButton(`게시글 1 ${count === 1 ? '앨범에 추가' : '전체 추가'}`)
 const select=document.querySelector<HTMLSelectElement>('select[aria-label="추가할 앨범"]')!
 await act(async()=>{select.value='9';select.dispatchEvent(new Event('change',{bubbles:true}))})
 await clickButton('선택한 앨범에 추가')
 expect(api.media.saveAlbum).toHaveBeenCalledExactlyOnceWith({albumId:9,expectedRevision:3,addFileIds:[],addMediaIds:[1]})
 expect(store.openDetail).not.toHaveBeenCalled()
})
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
 await act(async()=>{checks[0].click();checks[1].click()});expect(document.querySelector('[aria-label="전체 2 / 선택 2"]')?.textContent).toBe('2개 선택')
 await clickButton('한 폴더에 모으기');const select=document.querySelector('select[aria-label="이동할 폴더"]')!;await act(async()=>{select.value='7';select.dispatchEvent(new Event('change',{bubbles:true}))});await clickButton('복사 시작')
 expect(mocks.create).toHaveBeenCalledWith({selection:[{mediaId:1,revision:4},{mediaId:2,revision:4}],directoryId:7,operation:'copy'});expect(document.querySelector('header [role=status]')).toBeNull()
})

it('selects the loaded list, keeps failures selected and clears selection',async()=>{
 setup();await mount(h(GalleryPage));await clickButton('메뉴');await clickButton('현재 목록 전체 선택')
 await clickButton('선택 작업');expect(document.body.textContent).not.toContain('제목 편집');await clickButton('즐겨찾기 추가')
 expect(mocks.bulk).toHaveBeenCalledWith([{mediaId:1,revision:4},{mediaId:2,revision:4}],{kind:'favorite',value:true},expect.any(Function))
 expect(document.body.textContent).toContain('성공 1개 · 실패 1개');expect(document.querySelector('[aria-label="전체 2 / 선택 1"]')?.textContent).toBe('1개 선택')
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
 expect(document.activeElement?.textContent).toBe('GIF, WebP로 만들기')
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

it('places selection actions beside the gallery title while keeping the main controls stable',async()=>{
 setup();await mount(h(GalleryPage))
 const header=document.querySelector('header')!
 const toolbar=header.querySelector('[data-gallery-toolbar]')!
 const initialControls=[...toolbar.querySelectorAll('button')].map(item=>item.textContent)
 expect(initialControls).toEqual(['전체 보기','새로고침','메뉴'])
 expect(header.querySelector('[role=status]')).toBeNull();expect(header.querySelector('.invisible')).toBeNull()
 expect(header.textContent).not.toContain('묶음');expect(header.textContent).not.toContain('게시글')
 await act(async()=>document.querySelector<HTMLInputElement>('input[type=checkbox]')!.click())
 const bar=document.querySelector('[aria-label="선택 항목 작업"]')!
 expect(bar.closest('header')).toBe(header)
 expect(header.querySelector('h1')?.textContent).toBe('갤러리')
 expect(document.querySelectorAll('[aria-label="선택 항목 작업"]')).toHaveLength(1)
 expect(bar.querySelector('[role=status]')?.textContent).toBe('1개 선택')
 expect([...document.querySelectorAll('button')].some(item=>item.getAttribute('aria-label')==='게시글 1 더보기')).toBe(true)
 expect([...toolbar.querySelectorAll('button')].map(item=>item.textContent)).toEqual(initialControls)
 expect([...bar.querySelectorAll('button')].map(item=>item.textContent)).toEqual(['앨범에 추가','한 폴더에 모으기','병합','선택 작업','선택 해제'])
 await clickButton('메뉴')
 const clear=[...toolbar.querySelectorAll<HTMLButtonElement>('[role=menuitem]')].find(item=>item.textContent==='선택 해제')!
 await act(async()=>clear.click())
 expect(header.querySelector('[role=status]')).toBeNull()
 expect(document.querySelector('[aria-label="선택 항목 작업"]')).toBeNull()
 expect([...toolbar.querySelectorAll('button')].map(item=>item.textContent)).toEqual(initialControls)
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
 await clickButton('앨범 파일 접기');expect(store.toggleAlbum).toHaveBeenCalledExactlyOnceWith(1)
 await clickButton('더 불러오기');expect(store.loadMore).toHaveBeenCalledOnce()
 const options=vi.mocked(useInfiniteScroll).mock.lastCall![0]
 expect(options.enabled).toBe(true)
 await options.onLoadMore();expect(store.loadMore).toHaveBeenCalledTimes(2)
 expect(document.querySelector('[aria-label="갤러리 페이지"]')).toBeNull()
 expect(document.body.textContent).not.toContain('개 항목')
})

it('opens playback from the thumbnail and details from the title or named menu',async()=>{
 setup();store.openDetail.mockClear();await mount(h(GalleryPage))
 await clickButton('게시글 1 선택 · 두 번 클릭하여 감상')
 expect(document.querySelector('[aria-label="감상 1 "]')).not.toBeNull()
 expect(store.openDetail).not.toHaveBeenCalled()
 await clickButton('감상 닫기');await clickButton('게시글 1 상세보기')
 expect(store.openDetail).toHaveBeenCalledExactlyOnceWith(1,undefined)
 store.openDetail.mockClear();await clickButton('게시글 2 더보기')
 expect([...document.querySelectorAll('[role=menuitem]')].map(item=>item.textContent)).toEqual(expect.arrayContaining(['감상하기','상세 정보','앨범에 추가','파일 펼치기','즐겨찾기 추가','파일 위치 열기','제목 편집','태그 편집']))
 await clickButton('상세 정보');expect(store.openDetail).toHaveBeenCalledExactlyOnceWith(2,undefined)
})

it('distinguishes album controls from download file controls and keeps album editing in the menu',async()=>{
 setup();mocks.state.list.page.data[0].isUserAlbum=true;await mount(h(GalleryPage))
 expect(document.querySelector('[aria-label="게시글 1 앨범 펼치기"]')).not.toBeNull()
 expect(document.querySelector('[aria-label="게시글 2 파일 펼치기"]')).not.toBeNull()
 expect(document.body.textContent).toContain('사용자 앨범');expect(document.body.textContent).toContain('다운로드 묶음')
 expect([...document.querySelectorAll('button')].some(button=>button.getAttribute('aria-label')==='앨범 편집')).toBe(false)
 await clickButton('게시글 1 더보기');expect([...document.querySelectorAll('[role=menuitem]')].map(item=>item.textContent)).toContain('앨범 편집')
})

it('adds a mixed selection of groups and expanded files through the same selection bar',async()=>{
 setup();mocks.state.list.page.data[1]={...mocks.state.list.page.data[1],galleryFileOffset:0,galleryFile:{id:22,title:'개별 영상',kind:'video'}}
 await mount(h(GalleryPage));const checks=[...document.querySelectorAll<HTMLInputElement>('input[type=checkbox]')]
 await act(async()=>{checks[0].click();checks[1].click()})
 const bar=document.querySelector('[aria-label="선택 항목 작업"]')!
 expect(bar.textContent).toContain('2개 선택');expect(bar.textContent).toContain('묶음 1개 · 개별 파일 1개')
 await act(async()=>bar.querySelector<HTMLButtonElement>('button')!.click())
 const select=document.querySelector<HTMLSelectElement>('[aria-label="추가할 앨범"]')!
 await act(async()=>{select.value='9';select.dispatchEvent(new Event('change',{bubbles:true}))});await clickButton('선택한 앨범에 추가')
 expect(api.media.saveAlbum).toHaveBeenCalledExactlyOnceWith({albumId:9,expectedRevision:3,addFileIds:[22],addMediaIds:[1]})
 expect(document.querySelector('[aria-label="선택 항목 작업"]')).toBeNull()
 expect(checks.every(check=>!check.checked)).toBe(true)
})

it('moves a mixed selection with per-file scope and clears every kind of selection together',async()=>{
 setup();mocks.state.list.page.data[1]={...mocks.state.list.page.data[1],galleryFileOffset:0,galleryFile:{id:22,title:'개별 영상',kind:'video'}}
 await mount(h(GalleryPage));const checks=[...document.querySelectorAll<HTMLInputElement>('input[type=checkbox]')]
 await act(async()=>{checks[0].click();checks[1].click()});await clickButton('한 폴더에 모으기')
 const select=document.querySelector<HTMLSelectElement>('[aria-label="이동할 폴더"]')!
 await act(async()=>{select.value='7';select.dispatchEvent(new Event('change',{bubbles:true}))});await clickButton('복사 시작')
 expect(mocks.create).toHaveBeenCalledWith({selection:[{mediaId:1,revision:4},{mediaId:2,revision:4,fileIds:[22]}],directoryId:7,operation:'copy'})
 expect(document.querySelector('[aria-label="선택 항목 작업"]')).toBeNull()
})


it('connects Explorer mouse selection and double-click playback to gallery cards', async () => {
 setup(); await mount(h(GalleryPage))
 const first = document.querySelector<HTMLButtonElement>('article[data-album-id="1"] [data-card-select]')!
 const second = document.querySelector<HTMLButtonElement>('article[data-album-id="2"] [data-card-select]')!
 await act(async () => first.dispatchEvent(new MouseEvent('click', { bubbles:true, detail:1 })))
 expect(document.querySelector('[aria-label="감상 1 "]')).toBeNull()
 expect(document.querySelector('[aria-label="전체 2 / 선택 1"]')).not.toBeNull()
 await act(async () => second.dispatchEvent(new MouseEvent('click', { bubbles:true, detail:1, shiftKey:true })))
 expect(document.querySelector('[aria-label="전체 2 / 선택 2"]')).not.toBeNull()
 const checkbox = document.querySelector<HTMLInputElement>('input[type="checkbox"]')!
 checkbox.focus(); await act(async () => checkbox.dispatchEvent(new KeyboardEvent('keydown', { key:'Escape', bubbles:true, cancelable:true })))
 expect(document.querySelector('[aria-label="선택 항목 작업"]')).toBeNull()
 await act(async () => second.dispatchEvent(new MouseEvent('click', { bubbles:true, detail:1 })))
 await act(async () => second.dispatchEvent(new MouseEvent('dblclick', { bubbles:true, detail:2 })))
 expect(document.querySelector('[aria-label="감상 2 "]')).not.toBeNull()
})

it('freezes selected groups in the merge dialog and creates one combined job', async () => {
 setup(); mocks.create.mockClear(); mocks.run.mockClear(); await mount(h(GalleryPage)); await clickButton('메뉴'); await clickButton('현재 목록 전체 선택'); await clickButton('병합')
 expect(document.querySelector('dialog[aria-labelledby="folder-merge-title"]')).not.toBeNull()
 await act(async () => document.querySelector<HTMLInputElement>('input[value="combined"]')!.click())
 await act(async () => {
  const input = document.querySelector<HTMLInputElement>('input[aria-label="새 앨범 이름"]')!
  Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value')!.set!.call(input,'합친 묶음');input.dispatchEvent(new Event('input',{bubbles:true}))
  const select = document.querySelector<HTMLSelectElement>('select[aria-label="병합 대상 폴더"]')!
  select.value='7';select.dispatchEvent(new Event('change',{bubbles:true}))
 })
 await clickButton('병합 시작')
 expect(mocks.create).toHaveBeenCalledExactlyOnceWith({selection:[{mediaId:1,revision:4},{mediaId:2,revision:4}],directoryId:7,operation:'copy',mergeTitle:'합친 묶음'})
 expect(mocks.run).toHaveBeenCalledExactlyOnceWith({jobId:'job'})
 expect(document.querySelector('[aria-label="선택 항목 작업"]')).toBeNull()
 expect(document.querySelector('dialog')).toBeNull()
})

function dragTransfer(types = [albumDragType]) {
 return { types, effectAllowed: 'none', dropEffect: 'none', setData: vi.fn(), setDragImage: vi.fn() }
}
async function dragEvent(type: string, target: Element, transfer: ReturnType<typeof dragTransfer>) {
 const event = new Event(type, { bubbles: true, cancelable: true })
 Object.defineProperty(event, 'dataTransfer', { value: transfer })
 await act(async () => {
  target.dispatchEvent(event)
  // A successful native HTML drag suppresses the pointer stream immediately.
  if (type === 'dragstart' && !event.defaultPrevented) target.dispatchEvent(new PointerEvent('pointercancel', { bubbles: true, pointerId: 1, pointerType: 'mouse' }))
 })
 return event
}
function appendDropAlbum() {
 mocks.state.list.page.data.push({ id: 9, title: '여행', sourceKind: 'local', revision: 3, createdAt: 1, fileCount: 0, isUserAlbum: true })
 mocks.state.list.page.total = mocks.state.list.page.data.length
}
function expandedRows() {
 mocks.state.list.page.data = [11, 22, 33].map((id, index) => ({ id: index + 1, title: `묶음 ${index + 1}`, revision: 4, createdAt: 1, fileCount: 2,
  galleryFile: { id, title: `사진 ${id}`, kind: 'image' }, galleryFileOffset: 0 }))
 appendDropAlbum()
}
const sourceCard = (id: number) => document.querySelector<HTMLElement>(`article[data-album-id="${id}"]`)!
const dropAlbum = (id = 9) => document.querySelector<HTMLElement>(`[data-album-drop-id="${id}"]`)!

it('accepts an album drop after the pointercancel emitted by native dragstart', async () => {
 setup(); expandedRows(); await mount(h(GalleryPage)); const transfer = dragTransfer()
 await dragEvent('dragstart', sourceCard(1), transfer)
 const over = await dragEvent('dragover', dropAlbum(), transfer)
 expect(over.defaultPrevented).toBe(true)
 expect(transfer.dropEffect).toBe('copy')
 await dragEvent('drop', dropAlbum(), transfer)
 expect(api.media.saveAlbum).toHaveBeenCalledExactlyOnceWith({ albumId: 9, expectedRevision: 3, addFileIds: [11], addMediaIds: [] })
})

it('accepts the first drop before React commits drag state and keeps the toolbar layout stable', async () => {
 setup(); expandedRows()
 Object.assign(mocks.state.list.page.data[0].galleryFile, {thumbnailPath:'C:/fixture/thumb.jpg'})
 await mount(h(GalleryPage)); const transfer = dragTransfer()
 expect(sourceCard(1).querySelector('img')?.getAttribute('draggable')).toBe('false')
 const over = new Event('dragover', {bubbles:true,cancelable:true})
 await act(async () => {
  for (const [type, target] of [['dragstart',sourceCard(1).querySelector('img')!],['dragover',dropAlbum()],['drop',dropAlbum()]] as const) {
   const event = type === 'dragover' ? over : new Event(type,{bubbles:true,cancelable:true})
   Object.defineProperty(event,'dataTransfer',{value:transfer});target.dispatchEvent(event)
  }
 })
 expect(over.defaultPrevented).toBe(true)
 expect(api.media.saveAlbum).toHaveBeenCalledExactlyOnceWith({albumId:9,expectedRevision:3,addFileIds:[11],addMediaIds:[]})
 expect(mocks.refresh).toHaveBeenCalledOnce()
})

it('preserves a native drag through focus changes without inserting a selection toolbar', async () => {
 setup(); expandedRows(); await mount(h(GalleryPage)); const transfer = dragTransfer()
 await dragEvent('dragstart',sourceCard(1),transfer)
 expect(document.querySelector('[aria-label="선택 항목 작업"]')).toBeNull()
 expect(sourceCard(1).dataset.selected).toBe('true')
 await act(async () => window.dispatchEvent(new Event('blur')))
 await dragEvent('dragover',dropAlbum(),transfer)
 expect(dropAlbum().dataset.dropActive).toBe('true')
 await dragEvent('drop',dropAlbum(),transfer)
 expect(api.media.saveAlbum).toHaveBeenCalledOnce()
})

it('retries an additive drop once using the current album revision', async () => {
 setup(); expandedRows()
 api.media.saveAlbum.mockResolvedValueOnce({ok:false,error:{code:'REVISION_CONFLICT',message:'앨범이 변경되었습니다.'}})
 mocks.mediaGet.mockResolvedValueOnce({ok:true,data:{id:9,revision:8,isUserAlbum:true}})
 await mount(h(GalleryPage))
 await act(async () => { sourceCard(1).querySelector<HTMLInputElement>('input')!.click();sourceCard(2).querySelector<HTMLInputElement>('input')!.click() })
 const transfer=dragTransfer();await dragEvent('dragstart',sourceCard(1),transfer);await dragEvent('drop',dropAlbum(),transfer)
 expect(mocks.mediaGet).toHaveBeenCalledExactlyOnceWith({mediaId:9,fileLimit:1,fileOffset:0})
 expect(api.media.saveAlbum).toHaveBeenCalledTimes(2)
 expect(api.media.saveAlbum).toHaveBeenLastCalledWith({albumId:9,expectedRevision:8,addFileIds:[11,22],addMediaIds:[]})
 expect(document.querySelector('[role="alert"]')).toBeNull()
 expect(mocks.refresh).toHaveBeenCalledOnce()
})

it('waits for the new revision before accepting the next drop', async () => {
 setup(); expandedRows(); const refresh=deferred<boolean>();mocks.refresh.mockReturnValueOnce(refresh.promise)
 await mount(h(GalleryPage));const transfer=dragTransfer()
 await dragEvent('dragstart',sourceCard(1),transfer);await dragEvent('drop',dropAlbum(),transfer)
 expect(sourceCard(2).getAttribute('draggable')).toBe('false')
 await dragEvent('dragstart',sourceCard(2),transfer);await dragEvent('drop',dropAlbum(),transfer)
 expect(api.media.saveAlbum).toHaveBeenCalledOnce()
 await act(async () => { mocks.state.list.page.data.find((item:any)=>item.id===9).revision=8;refresh.resolve(true) })
 await dragEvent('dragstart',sourceCard(2),transfer);await dragEvent('drop',dropAlbum(),transfer)
 expect(api.media.saveAlbum).toHaveBeenCalledTimes(2)
 expect(api.media.saveAlbum).toHaveBeenLastCalledWith({albumId:9,expectedRevision:8,addFileIds:[22],addMediaIds:[]})
})

it('shows file actions in an expanded album and removes only that membership', async () => {
 setup(); expandedRows()
 Object.assign(mocks.state.list.page.data[0],{isUserAlbum:true,title:'사진 앨범',galleryFile:{id:11,mediaId:77,title:'사진 11',kind:'image',filepath:'C:/fixture/second.jpg'}})
 await mount(h(GalleryPage));await clickButton('사진 11 더보기')
 const labels=[...document.querySelectorAll('[role="menuitem"]')].map(item=>item.textContent)
 expect(labels).toEqual(expect.arrayContaining(['감상하기','상세 정보','앨범에 추가','파일 위치 열기','앨범에서 제외']))
 for(const label of ['앨범 편집','앨범 접기','등록 해제','제목 편집','즐겨찾기 추가']) expect(labels).not.toContain(label)
 await clickButton('파일 위치 열기')
 expect(api.showItemInFolder).toHaveBeenCalledExactlyOnceWith('C:/fixture/second.jpg')
 expect(mocks.mediaGet).not.toHaveBeenCalled()
 await clickButton('사진 11 더보기');await clickButton('앨범에서 제외')
 expect(api.media.saveAlbum).toHaveBeenCalledExactlyOnceWith({albumId:1,expectedRevision:4,removeFileIds:[11]})
 expect(mocks.unregister).not.toHaveBeenCalled()
 expect(mocks.refresh).toHaveBeenCalledOnce()
 expect(document.body.textContent).toContain('‘사진 앨범’ 앨범에서 제외했습니다.')
})

it('keeps album actions on the album card and does not remove a file from a normal group', async () => {
 setup();expandedRows();await mount(h(GalleryPage))
 await clickButton('여행 더보기')
 expect(document.querySelector('[role="menu"]')?.textContent).toContain('앨범 편집')
 expect(document.querySelector('[role="menu"]')?.textContent).not.toContain('앨범에서 제외')
 await clickButton('여행 더보기');await clickButton('사진 11 더보기')
 expect(document.querySelector('[role="menu"]')?.textContent).not.toContain('앨범에서 제외')
})

it('preserves an album file when removing it has a revision conflict', async () => {
 setup();expandedRows();mocks.state.list.page.data[0].isUserAlbum=true
 api.media.saveAlbum.mockResolvedValueOnce({ok:false,error:{code:'REVISION_CONFLICT',message:'앨범이 변경되었습니다.'}})
 await mount(h(GalleryPage));await clickButton('사진 11 더보기');await clickButton('앨범에서 제외')
 expect(api.media.saveAlbum).toHaveBeenCalledOnce()
 expect(mocks.mediaGet).not.toHaveBeenCalled()
 expect(mocks.refresh).not.toHaveBeenCalled()
 expect(sourceCard(1)).not.toBeNull()
 expect(document.querySelector('[role="alert"]')?.textContent).toContain('새로고침')
 expect(mocks.unregister).not.toHaveBeenCalled()
})

it('drags one expanded file without starting a marquee or including unrelated selections', async () => {
 setup(); expandedRows(); await mount(h(GalleryPage))
 expect(document.querySelector('aside')).toBeNull()
 expect(document.body.textContent).not.toContain('앨범에 놓기')
 expect(api.media.albums).not.toHaveBeenCalled()
 expect(dropAlbum()).toBe(sourceCard(9))
 await act(async () => sourceCard(1).querySelector<HTMLInputElement>('input')!.click())
 const source = sourceCard(2), transfer = dragTransfer()
 await act(async () => source.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true, pointerType: 'mouse', button: 0, pointerId: 1, clientX: 10, clientY: 10 })))
 await act(async () => window.dispatchEvent(new PointerEvent('pointermove', { bubbles: true, pointerId: 1, clientX: 150, clientY: 80 })))
 expect(document.querySelector('.pointer-events-none.fixed')).toBeNull()
 const started = await dragEvent('dragstart', source.querySelector('[data-card-select]')!, transfer)
 expect(started.defaultPrevented).toBe(false); expect(transfer.effectAllowed).toBe('copy')
 expect(transfer.setData).toHaveBeenCalledWith(albumDragType, JSON.stringify({ fileIds: [22], mediaIds: [] }))
 const over = await dragEvent('dragover', dropAlbum().querySelector('h2 button')!, transfer)
 expect(over.defaultPrevented).toBe(true); expect(transfer.dropEffect).toBe('copy')
 expect(dropAlbum().dataset.dropActive).toBe('true')
 await dragEvent('drop', dropAlbum(), transfer)
 expect(api.media.saveAlbum).toHaveBeenCalledExactlyOnceWith({ albumId: 9, expectedRevision: 3, addFileIds: [22], addMediaIds: [] })
 expect(mocks.refresh).toHaveBeenCalledOnce()
 expect(document.querySelector('[aria-label="선택 항목 작업"]')).toBeNull()
 expect(document.body.textContent).toContain('‘여행’ 앨범에 추가했습니다.')
})

it('drags all selected files from a selected card and retains them on cancellation', async () => {
 setup(); expandedRows(); await mount(h(GalleryPage))
 const checks = [...document.querySelectorAll<HTMLInputElement>('article:not([data-album-drop-id]) input')]
 await act(async () => { checks[0].click(); checks[1].click() })
 const transfer = dragTransfer()
 await dragEvent('dragstart', sourceCard(1), transfer)
 expect(transfer.setData).toHaveBeenCalledWith(albumDragType, JSON.stringify({ fileIds: [11, 22], mediaIds: [] }))
 expect(document.querySelector('[aria-label="선택 항목 작업"]')?.textContent).toContain('2개 선택')
 await dragEvent('dragend', sourceCard(1), transfer)
 expect(api.media.saveAlbum).not.toHaveBeenCalled()
 expect(checks.map(check => check.checked)).toEqual([true, true, false])
 await dragEvent('dragstart', sourceCard(2), transfer)
 await dragEvent('drop', dropAlbum(), transfer)
 expect(api.media.saveAlbum).toHaveBeenCalledExactlyOnceWith({ albumId: 9, expectedRevision: 3, addFileIds: [11, 22], addMediaIds: [] })
})

it('supports mixed group and individual-file drags', async () => {
 setup(); mocks.state.list.page.data[1].galleryFile = { id: 22, title: '파일', kind: 'image' }
 appendDropAlbum()
 await mount(h(GalleryPage))
 await act(async () => { for (const check of document.querySelectorAll<HTMLInputElement>('article:not([data-album-drop-id]) input')) check.click() })
 const transfer = dragTransfer()
 await dragEvent('dragstart', sourceCard(1), transfer); await dragEvent('drop', dropAlbum(), transfer)
 expect(api.media.saveAlbum).toHaveBeenCalledExactlyOnceWith({ albumId: 9, expectedRevision: 3, addFileIds: [22], addMediaIds: [1] })
})

it('rejects self-album drops, external file drops, and drags from action controls', async () => {
 setup(); mocks.state.list.page.data[0].id = 9; mocks.state.list.page.data[0].isUserAlbum = true
 await mount(h(GalleryPage))
 const transfer = dragTransfer()
 const actionDrag = await dragEvent('dragstart', sourceCard(9).querySelector('[data-action-boundary] button')!, transfer)
 expect(actionDrag.defaultPrevented).toBe(true); expect(transfer.setData).not.toHaveBeenCalled()
 await dragEvent('dragstart', sourceCard(9), transfer)
 expect((await dragEvent('dragover', dropAlbum(), transfer)).defaultPrevented).toBe(false)
 expect(transfer.dropEffect).toBe('none')
 await dragEvent('drop', dropAlbum(), transfer)
 await dragEvent('dragend', sourceCard(9), transfer)
 await dragEvent('drop', dropAlbum(), dragTransfer(['Files']))
 expect(api.media.saveAlbum).not.toHaveBeenCalled()
})

it('preserves selected files on a revision conflict and retries after refreshing target albums', async () => {
 setup(); expandedRows()
 api.media.saveAlbum.mockResolvedValueOnce({ ok: false, error: { code: 'REVISION_CONFLICT', message: '앨범이 변경되었습니다.' } })
 await mount(h(GalleryPage))
 await act(async () => { sourceCard(1).querySelector<HTMLInputElement>('input')!.click(); sourceCard(2).querySelector<HTMLInputElement>('input')!.click() })
 const transfer = dragTransfer()
 await dragEvent('dragstart', sourceCard(1), transfer); await dragEvent('drop', dropAlbum(), transfer)
 expect(document.querySelector('[role="alert"]')?.textContent).toContain('새로고침')
 expect([...document.querySelectorAll<HTMLInputElement>('article:not([data-album-drop-id]) input')].map(check => check.checked)).toEqual([true, true, false])
 expect(mocks.refresh).not.toHaveBeenCalled()
 mocks.state.list.page.data.find((item: any) => item.id === 9).revision = 8
 await clickButton('새로고침')
 await act(async () => { sourceCard(1).querySelector<HTMLInputElement>('input')!.click(); sourceCard(2).querySelector<HTMLInputElement>('input')!.click() })
 await dragEvent('dragstart', sourceCard(2), transfer); await dragEvent('drop', dropAlbum(), transfer)
 expect(api.media.saveAlbum).toHaveBeenLastCalledWith({ albumId: 9, expectedRevision: 8, addFileIds: [11, 22], addMediaIds: [] })
 expect(document.querySelector('[aria-label="선택 항목 작업"]')).toBeNull()
})

it('blocks duplicate drops and other commands while a save is pending', async () => {
 setup(); expandedRows(); const pending = deferred<any>(); api.media.saveAlbum.mockReturnValueOnce(pending.promise)
 await mount(h(GalleryPage)); const transfer = dragTransfer()
 await dragEvent('dragstart', sourceCard(1), transfer); await dragEvent('drop', dropAlbum(), transfer)
 expect(api.media.saveAlbum).toHaveBeenCalledOnce()
 expect(sourceCard(1).getAttribute('draggable')).toBe('false')
 await dragEvent('drop', dropAlbum(), transfer)
 expect(api.media.saveAlbum).toHaveBeenCalledOnce()
 expect(document.querySelector<HTMLButtonElement>('[aria-label="새로고침"]')?.disabled).toBe(true)
 await act(async () => pending.resolve({ ok: true, data: { id: 9, title: '여행', revision: 4, fileCount: 1 } }))
 expect(mocks.refresh).toHaveBeenCalledOnce()
})

it('cancels a native album drag with Escape without clearing the selection', async () => {
 setup(); expandedRows(); await mount(h(GalleryPage)); const transfer = dragTransfer()
 await dragEvent('dragstart', sourceCard(1), transfer)
 await act(async () => window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true })))
 await dragEvent('drop', dropAlbum(), transfer)
 expect(api.media.saveAlbum).not.toHaveBeenCalled()
 expect(sourceCard(1).querySelector<HTMLInputElement>('input')?.checked).toBe(true)
})

it('defers thumbnail refreshes until the active album drag ends', async () => {
 setup(); expandedRows(); await mount(h(GalleryPage)); const transfer = dragTransfer()
 vi.useFakeTimers()
 try {
  await dragEvent('dragstart', sourceCard(1), transfer)
  await act(async () => { mocks.on.mock.lastCall![0](); await vi.advanceTimersByTimeAsync(100) })
  expect(mocks.refresh).not.toHaveBeenCalled()
  expect(document.querySelector('[aria-label="선택 항목 작업"]')).toBeNull()
  await dragEvent('dragend', sourceCard(1), transfer)
  expect(mocks.refresh).toHaveBeenCalledOnce()
 } finally { vi.useRealTimers() }
})

it('rejects drops onto normal group cards, expanded files, and empty gallery space', async () => {
 setup(); expandedRows(); mocks.state.list.page.data[2].galleryFile = undefined
 await mount(h(GalleryPage)); const transfer = dragTransfer()
 await dragEvent('dragstart', sourceCard(1), transfer)
 for (const target of [sourceCard(2), sourceCard(3), document.querySelector<HTMLElement>('.motion-gallery-grid')!]) {
  expect((await dragEvent('dragover', target, transfer)).defaultPrevented).toBe(false)
  expect(transfer.dropEffect).toBe('none')
  await dragEvent('drop', target, transfer)
 }
 expect(api.media.saveAlbum).not.toHaveBeenCalled()
 expect(sourceCard(1).querySelector<HTMLInputElement>('input')?.checked).toBe(true)
})

it('highlights only the destination album card and clears it when the drag leaves or ends', async () => {
 setup(); expandedRows(); await mount(h(GalleryPage)); const transfer = dragTransfer()
 await dragEvent('dragstart', sourceCard(1), transfer)
 await dragEvent('dragover', dropAlbum(), transfer)
 expect(document.querySelectorAll('[data-drop-active]')).toHaveLength(1)
 expect(dropAlbum().querySelector('[aria-hidden="true"].pointer-events-none')?.textContent).toBe('앨범에 추가')
 const betweenChildren = new Event('dragleave', { bubbles: true })
 Object.defineProperty(betweenChildren, 'relatedTarget', { value: dropAlbum().querySelector('h2 button') })
 await act(async () => dropAlbum().dispatchEvent(betweenChildren))
 expect(dropAlbum().dataset.dropActive).toBe('true')
 await dragEvent('dragleave', dropAlbum(), transfer)
 expect(dropAlbum().hasAttribute('data-drop-active')).toBe(false)
 await dragEvent('dragover', dropAlbum(), transfer)
 await dragEvent('dragend', sourceCard(1), transfer)
 expect(dropAlbum().hasAttribute('data-drop-active')).toBe(false)
})

it('does not report a completed album drop after the gallery has unmounted', async () => {
 setup(); expandedRows(); const pending = deferred<any>(); api.media.saveAlbum.mockReturnValueOnce(pending.promise)
 const stop = await mount(h(GalleryPage)); const transfer = dragTransfer()
 await dragEvent('dragstart', sourceCard(1), transfer); await dragEvent('drop', dropAlbum(), transfer)
 await stop()
 await act(async () => pending.resolve({ ok: true, data: { id: 9, revision: 4 } }))
 expect(mocks.refresh).not.toHaveBeenCalled()
})
