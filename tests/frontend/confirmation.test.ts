// @vitest-environment happy-dom
import { act, createElement as h, StrictMode, useRef, useState } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ConfirmProvider } from '@renderer/app/providers/ConfirmProvider'
import { LeaveGuardProvider } from '@renderer/app/providers/LeaveGuardProvider'
import { useConfirm } from '@renderer/shared/hooks/useConfirm'
import { useLeaveGuard } from '@renderer/shared/hooks/useLeaveGuard'
import type { Confirm } from '@renderer/shared/hooks/confirmContext'
import type { LeaveGuardRef } from '@renderer/shared/hooks/leaveGuardContext'
import { ListDetailLayout } from '@renderer/shared/components/layout/ListDetailLayout'
import { cleanupReact, clickButton, mount } from './reactHarness'

const dialog = () => document.querySelector<HTMLElement>('[role=alertdialog]')!
const button = (name: string) => [...document.querySelectorAll('button')].find(b => b.textContent === name)!
function Trigger({ done, chain = false }: { done: (accepted: boolean) => void; chain?: boolean }) {
 const { confirm } = useConfirm()
 return h('button', { onClick: async () => { let answer = await confirm({title:'등록 해제',message:'선택한 자료의 등록을 해제할까요?',danger:true}); if(answer && chain) answer = await confirm('편집 중인 내용을 버릴까요?');done(answer) } }, '작업')
}
async function open() {button('작업').focus();await clickButton('작업')}
async function key(key: string, shiftKey = false) { await act(async()=>{ document.activeElement!.dispatchEvent(new KeyboardEvent('keydown',{key,shiftKey,bubbles:true,cancelable:true})) }) }
beforeEach(()=>{ Object.defineProperty(window,'confirm',{configurable:true,writable:true,value:vi.fn(()=>{throw Error('native dialog forbidden')})}) })
afterEach(async()=>{await cleanupReact();vi.restoreAllMocks()})

describe('application confirmation dialogs',()=>{
 it('uses app text, disables the background, focuses cancel and restores focus on approval',async()=>{
  const done=vi.fn();await mount(h(ConfirmProvider,null,h(Trigger,{done})));await open()
  expect(dialog().textContent).toContain('선택한 자료');expect(dialog().getAttribute('aria-modal')).toBe('true')
  expect(document.querySelector('[data-confirm-background]')?.hasAttribute('inert')).toBe(true);expect(document.activeElement).toBe(button('취소'));expect(done).not.toHaveBeenCalled()
  expect(window.confirm).not.toHaveBeenCalled();expect(document.body.textContent).not.toContain('wails.localhost')
  await clickButton('확인');expect(done).toHaveBeenCalledExactlyOnceWith(true);expect(dialog()).toBeNull();expect(document.activeElement).toBe(button('작업'));expect(document.querySelector('[data-confirm-background]')?.hasAttribute('inert')).toBe(false)
 })
 it('cancels with Escape and isolates it from document-level close handlers',async()=>{
  const done=vi.fn(),outer=vi.fn();await mount(h(ConfirmProvider,null,h(Trigger,{done})));await open();document.addEventListener('keydown',outer)
  try {await key('Escape');expect(done).toHaveBeenCalledExactlyOnceWith(false);expect(outer).not.toHaveBeenCalled();expect(document.activeElement).toBe(button('작업'))} finally{document.removeEventListener('keydown',outer)}
 })
 it('cancels explicitly, traps forward/backward Tab and keeps overlay clicks inert',async()=>{
  const done=vi.fn();await mount(h(ConfirmProvider,null,h(Trigger,{done})));await open();await key('Tab');expect(document.activeElement).toBe(button('확인'));await key('Tab',true);expect(document.activeElement).toBe(button('취소'))
  const overlay=[...document.body.children].find(el=>el.classList.contains('z-[99998]'))!;await act(async()=>{(overlay as HTMLElement).click()});expect(dialog()).not.toBeNull();await clickButton('취소');expect(done).toHaveBeenCalledExactlyOnceWith(false)
 })
 it('rejects a second simultaneous request without replacing the first',async()=>{
  const done=vi.fn();await mount(h(ConfirmProvider,null,h(Trigger,{done})));await open();await clickButton('작업');expect(done).toHaveBeenCalledExactlyOnceWith(false);expect(dialog()).not.toBeNull();await clickButton('확인');expect(done.mock.calls.map(c=>c[0])).toEqual([false,true])
 })
 it('resolves canceled work when the provider disappears and rejects late calls',async()=>{
  let confirm!:Confirm
  function Capture(){confirm=useConfirm().confirm;return h(Trigger,{done})}
  const done=vi.fn();const stop=await mount(h(StrictMode,null,h(ConfirmProvider,null,h(Capture))));await open();await stop();expect(done).toHaveBeenCalledExactlyOnceWith(false);expect(await confirm('late')).toBe(false)
 })
 it('supports sequential requests and missing/restored focus origins',async()=>{
  const done=vi.fn();await mount(h(ConfirmProvider,null,h(Trigger,{done,chain:true})));await open();await clickButton('확인');expect(dialog().textContent).toContain('편집 중인');await clickButton('취소');expect(done).toHaveBeenCalledExactlyOnceWith(false);expect(document.activeElement).toBe(button('작업'))
 })
 it('fails closed without a provider and supports custom labels',async()=>{
  const done=vi.fn();await mount(h(Trigger,{done}));await open();expect(done).toHaveBeenCalledExactlyOnceWith(false);expect(dialog()).toBeNull();await cleanupReact()
  function Custom(){const {confirm}=useConfirm();return h('button',{onClick:async()=>done(await confirm({message:'질문',confirmLabel:'진행',cancelLabel:'유지'}))},'작업')}
  await mount(h(ConfirmProvider,null,h(Custom)));await open();expect(dialog().textContent).toContain('확인');await clickButton('진행');expect(done).toHaveBeenLastCalledWith(true)
 })
})


it('handles missing or removed focus origins safely', async()=>{
 let ask!:Confirm
 function Capture(){ask=useConfirm().confirm;return null}
 await mount(h(ConfirmProvider,null,h(Capture)))
 const origin=document.createElement('button');document.body.append(origin);origin.focus()
 let pending!:Promise<boolean>;await act(async()=>{pending=ask('내용')});origin.remove();await clickButton('취소');expect(await pending).toBe(false)
 const active=Object.getOwnPropertyDescriptor(document,'activeElement')
 Object.defineProperty(document,'activeElement',{configurable:true,get:()=>null})
 try {await act(async()=>{pending=ask('다시')})} finally {if(active)Object.defineProperty(document,'activeElement',active);else delete (document as any).activeElement}
 await clickButton('확인');expect(await pending).toBe(true)
})

describe('async unsaved-data guard',()=>{
 let set!: (dirty:boolean,busy?:boolean,show?:boolean)=>void
 const done=vi.fn()
 function Guarded({dirty,busy}:{dirty:boolean;busy:boolean}){useLeaveGuard(dirty,busy);return null}
 function Harness(){const ref=useRef<LeaveGuardRef['current']>(null);const [state,update]=useState({dirty:true,busy:false,show:true});set=(dirty,busy=false,show=true)=>update({dirty,busy,show});return h(LeaveGuardProvider,{guard:ref,children:h('div',null,state.show&&h(Guarded,state),h('button',{onClick:async()=>done(await ref.current?.())},'작업'))})}
 beforeEach(()=>done.mockReset())
 it('guards drafts and allows clean navigation without a dialog',async()=>{
  await mount(h(ConfirmProvider,null,h(Harness)));await open();await clickButton('취소');expect(done).toHaveBeenLastCalledWith(false);await open();await clickButton('버리고 계속');expect(done).toHaveBeenLastCalledWith(true)
  await act(async()=>set(false));await clickButton('작업');expect(done).toHaveBeenLastCalledWith(true);expect(dialog()).toBeNull()
 })
 it('blocks busy work and stale approvals after state changes or guard unmount',async()=>{
  await mount(h(ConfirmProvider,null,h(Harness)));await act(async()=>set(true,true));await clickButton('작업');expect(done).toHaveBeenLastCalledWith(false);expect(dialog()).toBeNull()
  await act(async()=>set(true));await open();await act(async()=>set(false));await clickButton('버리고 계속');expect(done).toHaveBeenLastCalledWith(false)
  await act(async()=>set(true));await open();await act(async()=>set(true,false,false));await clickButton('버리고 계속');expect(done).toHaveBeenLastCalledWith(false)
 })
})

describe('detail outside clicks',()=>{
 const action=vi.fn(),close=vi.fn()
 function Draft({dirty}:{dirty:boolean}){useLeaveGuard(dirty,false);return h('input',{'aria-label':'초안'})}
 function Harness({dirty=true}:{dirty?:boolean}){const [open,setOpen]=useState(true);const {confirm}=useConfirm();const guard=useRef<LeaveGuardRef['current']>(null);const onClose=()=>{close();if(!dirty){setOpen(false);return true};return confirm('초안을 버릴까요?').then(yes=>{if(yes)setOpen(false);return yes})};return h(LeaveGuardProvider,{guard,children:h(ListDetailLayout,{detail:open?h(Draft,{dirty}):undefined,detailLabel:'상세',detailKey:open?1:null,onClose,children:h('button',{onClick:async()=>{if(!guard.current||await guard.current())action()}},'화면 이동')})})}
 beforeEach(()=>{action.mockReset();close.mockReset()})
 it('replays the original action once after async acceptance and never on cancel',async()=>{
  await mount(h(ConfirmProvider,null,h(Harness)));await clickButton('화면 이동');expect(action).not.toHaveBeenCalled();await clickButton('취소');expect(action).not.toHaveBeenCalled();expect(document.querySelector('input')).not.toBeNull()
  await clickButton('화면 이동');await clickButton('확인');expect(action).toHaveBeenCalledOnce();expect(document.querySelector('input')).toBeNull();expect(close).toHaveBeenCalledTimes(2)
 })
 it('closes clean details synchronously and ignores modal keys/outside clicks',async()=>{
  await mount(h(ConfirmProvider,null,h(Harness,{dirty:false})));await clickButton('화면 이동');expect(action).toHaveBeenCalledOnce();expect(dialog()).toBeNull();await cleanupReact()
  await mount(h(ConfirmProvider,null,h(Harness)));await clickButton('화면 이동');await key('Escape');expect(close).toHaveBeenCalledTimes(2);expect(action).toHaveBeenCalledTimes(1)
 })
})

it('keeps a newer leave guard when the older owner unmounts',async()=>{
 let remove!:()=>void
 const ref:LeaveGuardRef={current:null}
 function Guard(){useLeaveGuard(false,false);return null}
 function Pair(){const[show,set]=useState(true);remove=()=>set(false);return h(LeaveGuardProvider,{guard:ref,children:h('div',null,show&&h(Guard,{key:'old'}),h(Guard,{key:'new'}))})}
 await mount(h(ConfirmProvider,null,h(Pair)));const latest=ref.current;await act(async()=>remove());expect(ref.current).toBe(latest);expect(await ref.current?.()).toBe(true)
 await cleanupReact();await mount(h(Guard));expect(document.querySelector('[role=alertdialog]')).toBeNull()
})

it('cancels on Escape even when the key targets the body after an overlay click',async()=>{
 const done=vi.fn();await mount(h(ConfirmProvider,null,h(Trigger,{done})));await open()
 await act(async()=>{document.body.dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true,cancelable:true}))})
 expect(done).toHaveBeenCalledExactlyOnceWith(false);expect(dialog()).toBeNull()
})
