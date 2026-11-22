// @vitest-environment happy-dom
import { afterEach, expect, it, vi } from 'vitest'
import { mountRangeControl } from '../../internal/webserver/web/editor.js'
afterEach(()=>{document.body.replaceChildren();vi.restoreAllMocks()})
it('keeps nearby handles at the same height and lets either boundary be dragged from overlap',()=>{
    let values=[50,51]
    const changes=vi.fn((a:number,b:number)=>{values=[a,b]})
    const range=mountRangeControl(document.body,{label:'구간',count:()=>1000,values:()=>values,change:changes,format:(n:number)=>String(n)})
    const track=document.querySelector<HTMLElement>('.sbe-range-track')!
    vi.spyOn(track,'getBoundingClientRect').mockReturnValue({left:0,width:1000} as DOMRect)
    track.setPointerCapture=vi.fn()
    const end=document.querySelector<HTMLElement>('.sbe-range-end')!
    function drag(dx:number) {
        end.dispatchEvent(new PointerEvent('pointerdown',{bubbles:true,pointerId:1,button:0,clientX:51}))
        track.dispatchEvent(new PointerEvent('pointermove',{bubbles:true,pointerId:1,clientX:51+dx}))
        track.dispatchEvent(new PointerEvent('pointerup',{bubbles:true,pointerId:1,clientX:51+dx}))
    }
    range.update();expect(track.classList.contains('is-tight')).toBe(false)
    drag(-20);expect(values).toEqual([30,51])
    values=[50,51];range.update();drag(20);expect(values).toEqual([50,71])
    expect(changes.mock.lastCall?.[3]).toBe(true)
    expect(document.querySelector<HTMLElement>('.sbe-range-start')!.style.top).toBe('')
    expect(end.style.top).toBe('')
})
