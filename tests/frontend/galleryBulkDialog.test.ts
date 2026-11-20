// @vitest-environment happy-dom
import { act, createElement as h } from 'react'
import { afterEach, expect, it, vi } from 'vitest'
import { GalleryBulkDialog } from '@renderer/features/gallery/GalleryBulkDialog'
import { cleanupReact, clickButton, mount } from './reactHarness'
afterEach(cleanupReact)
it('traps keyboard focus, restores the background and deduplicates entered tags',async()=>{
    const background=document.createElement('div');background.id='root';document.body.append(background)
    const apply=vi.fn(),close=vi.fn(), stop=await mount(h(GalleryBulkDialog,{count:2,remove:false,onApply:apply,onClose:close}))
    expect(background.inert).toBe(true)
    const input=document.querySelector('textarea')!, cancel=[...document.querySelectorAll('button')].find(item=>item.textContent==='취소')!
    expect(document.activeElement).toBe(input)
    await act(async()=>input.dispatchEvent(new KeyboardEvent('keydown',{key:'Tab',shiftKey:true,bubbles:true,cancelable:true})))
    expect(document.activeElement).toBe(cancel)
    await act(async()=>cancel.dispatchEvent(new KeyboardEvent('keydown',{key:'Tab',bubbles:true,cancelable:true})))
    expect(document.activeElement).toBe(input)
    await act(async()=>{Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype,'value')!.set!.call(input,'a, a\n b');input.dispatchEvent(new Event('input',{bubbles:true}))})
    await clickButton('적용');expect(apply).toHaveBeenCalledWith(['a','b'])
    await act(async()=>document.dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true})));expect(close).toHaveBeenCalledOnce()
    await stop();expect(background.inert).toBe(false);background.remove()
})
