// @vitest-environment happy-dom
import { act, createElement as h, useState } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ConfirmProvider } from '@renderer/app/providers/ConfirmProvider'
import { MediaDetailPane } from '@renderer/features/gallery/MediaDetailPane'
import { FileMovePanel } from '@renderer/features/gallery/FileMovePanel'
import type { GallerySnapshot, GalleryStore } from '@renderer/features/gallery/galleryStore'
import { cleanupReact, clickButton, mount } from './reactHarness'
const mocks=vi.hoisted(()=>({get:vi.fn(),create:vi.fn(),run:vi.fn(),directories:vi.fn()}))
const api={fileMove:mocks,media:{directories:mocks.directories}}
vi.mock('@renderer/shared/api',()=>({getApi:()=>api}))
vi.mock('@renderer/shared/hooks/useToast',()=>({useToast:()=>({showToast:vi.fn()})}))
afterEach(cleanupReact)
beforeEach(()=>{vi.resetAllMocks();Object.defineProperty(window,'confirm',{configurable:true,writable:true,value:vi.fn(()=>{throw Error('native dialog forbidden')})})})
async function answer(accepted:boolean){const buttons=[...document.querySelectorAll<HTMLButtonElement>('[role=alertdialog] button')];await act(async()=>{buttons[accepted?1:0].click()})}
async function changeInput(input:HTMLInputElement,value:string){await act(async()=>{Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value')!.set!.call(input,value);input.dispatchEvent(new Event('input',{bubbles:true}))})}
function fixture(){
 const data={id:1,revision:2,title:'저장된 제목',customTitle:'저장된 제목',sourceTitle:'원본',sourceKind:'legacy',coverMode:'auto',cover:null,fileCount:1,files:{data:[{id:7,title:'파일',fileKey:'one',filepath:'C:/synthetic/file.bin',kind:'unknown',storageKind:'unverified_legacy',thumbnailPath:null,previewPath:null}],total:1,hasNextPage:false},tags:[]}
 const state={busy:false,needsRefresh:false,detail:{mediaId:1,data,status:'ready',offset:0,error:null}} as unknown as GallerySnapshot
 const remove=vi.fn(async()=>true),removeFiles=vi.fn(async()=>true),edit=vi.fn(async()=>true)
 const store={getSnapshot:()=>state,unregisterMedia:remove,unregisterFiles:removeFiles,edit} as unknown as GalleryStore
 return{state,store,remove,removeFiles,edit,view:h(MediaDetailPane,{state,store,onDirtyChange:vi.fn()})}
}
describe('confirmed media actions',()=>{
 it('cancels deletion and applies the confirmed media ID and revision exactly once',async()=>{
  const f=fixture();await mount(h(ConfirmProvider,null,f.view));await clickButton('묶음 등록 해제');expect(f.remove).not.toHaveBeenCalled();await answer(false);expect(f.remove).not.toHaveBeenCalled();await clickButton('묶음 등록 해제');await answer(true);expect(f.remove).toHaveBeenCalledExactlyOnceWith({mediaId:1,expectedRevision:2});expect(window.confirm).not.toHaveBeenCalled()
 })
 it('uses one file dialog containing the draft warning',async()=>{
  const f=fixture();await mount(h(ConfirmProvider,null,f.view));await changeInput(document.querySelector('input')!,'초안');await clickButton('등록 해제');expect(document.querySelector('[role=alertdialog]')?.textContent).toContain('작성 중인 제목·태그');await answer(true);expect(f.removeFiles).toHaveBeenCalledExactlyOnceWith({mediaId:1,expectedRevision:2,fileIds:[7]});expect(document.querySelector('[role=alertdialog]')).toBeNull()
 })
 it('does not apply a stale approval after the target revision changes',async()=>{
  const f=fixture();await mount(h(ConfirmProvider,null,f.view));await clickButton('묶음 등록 해제');f.state.detail.data={...f.state.detail.data!,revision:3};await answer(true);expect(f.remove).not.toHaveBeenCalled()
 })
 it('does not apply a confirmation belonging to an unmounted detail',async()=>{
  const f=fixture();let hide!:()=>void
  function Host(){const[show,set]=useState(true);hide=()=>set(false);return h(ConfirmProvider,null,show&&f.view)}
  await mount(h(Host));await clickButton('묶음 등록 해제');await act(async()=>hide());await answer(true);expect(f.remove).not.toHaveBeenCalled()
 })
 it('keeps a declined title draft and resets it only after approval',async()=>{
  const f=fixture();await mount(h(ConfirmProvider,null,f.view));const input=document.querySelector('input')!;await changeInput(input,'새 초안');await clickButton('저장된 제목 불러오기');await answer(false);expect(input.value).toBe('새 초안');await clickButton('저장된 제목 불러오기');await answer(true);expect(input.value).toBe('저장된 제목');expect(f.edit).not.toHaveBeenCalled()
 })
 it('confirms reverting the original title before changing stored data',async()=>{
  const f=fixture();await mount(h(ConfirmProvider,null,f.view));await changeInput(document.querySelector('input')!,'초안');await clickButton('원본 제목으로 되돌리기');await answer(false);expect(f.edit).not.toHaveBeenCalled();await clickButton('원본 제목으로 되돌리기');await answer(true);expect(f.edit).toHaveBeenCalledExactlyOnceWith({mediaId:1,expectedRevision:2,customTitle:null})
 })
})
it('waits for the real shared dialog before creating any file move',async()=>{
 mocks.get.mockResolvedValue({ok:true,data:null});mocks.directories.mockResolvedValue({ok:true,data:{data:[{id:9,directoryPath:'C:/synthetic/B'}],hasNextPage:false}});mocks.create.mockResolvedValue({ok:true,data:{jobId:'move'}});mocks.run.mockResolvedValue({ok:true,data:{jobId:'move'}})
 await mount(h(ConfirmProvider,null,h(FileMovePanel,{selection:[{mediaId:1,revision:2}],disabled:false,onCreated:vi.fn(),onChanged:vi.fn()})))
 await clickButton('파일 이동');const select=document.querySelector('select')!;await act(async()=>{select.value='9';select.dispatchEvent(new Event('change',{bubbles:true}))});await clickButton('이동 시작');expect(mocks.create).not.toHaveBeenCalled();await answer(false);expect(mocks.create).not.toHaveBeenCalled();await clickButton('이동 시작');await answer(true);expect(mocks.create).toHaveBeenCalledExactlyOnceWith({selection:[{mediaId:1,revision:2}],directoryId:9});expect(mocks.run).toHaveBeenCalledOnce()
})
