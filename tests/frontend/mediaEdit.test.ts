// @vitest-environment happy-dom
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { mountMediaEditor, boundedCrop, keptFrames, FrameRanges } from '../../internal/webserver/web/editor.js'

const draft = () => ({ id:'a7b4c924-c2eb-4c50-8795-83272a0aad49',fileId:1,title:'합성 영상',options:{requestId:'session',fileId:1,start:0,end:1,fps:10,width:160,kind:'gif'},state:'ready',width:160,height:120,frameCount:10,progress:100,error:'',cancelling:false,createdAt:1 })
let instance: { destroy(): void } | undefined
const ok = (data: unknown) => ({ok:true,data})
const flush = async () => { for(let i=0;i<12;i++)await Promise.resolve() }
const button = (label: string) => [...document.querySelectorAll('button')].find(el=>el.textContent===label)!
const context = {clearRect:vi.fn(),drawImage:vi.fn(),beginPath:vi.fn(),rect:vi.fn(),fill:vi.fn(),strokeRect:vi.fn(),fillRect:vi.fn()}
beforeEach(()=>{
    localStorage.clear()
    vi.spyOn(HTMLCanvasElement.prototype,'getContext').mockReturnValue(context as unknown as CanvasRenderingContext2D)
    vi.stubGlobal('Image',class {onload?:()=>void;onerror?:()=>void;_src='';set src(value:string){this._src=value;queueMicrotask(()=>this.onload?.())}get src(){return this._src}})
    vi.stubGlobal('confirm',vi.fn(()=>true))
})
afterEach(()=>{instance?.destroy();instance=undefined;document.body.replaceChildren();vi.useRealTimers();vi.restoreAllMocks();vi.unstubAllGlobals()})
async function setup(session = draft(), file?: unknown) {
    const api = {list:vi.fn(async()=>ok({sessions:[session],jobs:[]})),prepare:vi.fn(),create:vi.fn(async()=>ok({id:'job'})),cancel:vi.fn(async()=>ok(null)),release:vi.fn(async()=>ok(null)),frameStatus:vi.fn(async()=>ok({offset:0,count:6,state:'ready',times:[0,.03,.14,.21,.46,.55],durations:[.03,.11,.07,.25,.09,.12]}))}
    const host=document.createElement('div');document.body.append(host);instance=mountMediaEditor(host,{api,file});await flush();button('열기').click();await flush();return api
}
it('keeps immutable output-frame IDs and bounds arbitrary crop coordinates',()=>{
    expect(keptFrames(6,[1,3])).toEqual([0,2,4,5])
    expect(boundedCrop({x:100,y:-5,width:80,height:999},160,120)).toEqual({x:80,y:0,width:80,height:120})
})
it('sends the selected frame deletions and crop, with undo and redo',async()=>{
    const api=await setup()
    const width=[...document.querySelectorAll('label')].find(el=>el.firstChild?.textContent==='너비')!.querySelector('input')!
    width.value='77';width.dispatchEvent(new Event('change'))
    for(const n of [2,5])document.querySelector<HTMLInputElement>(`input[aria-label="${n}번 프레임 선택"]`)!.click()
    button('선택 프레임 삭제').click();expect(document.body.textContent).toContain('10장 중 8장 남음')
    button('실행 취소').click();expect(document.body.textContent).toContain('10장 중 10장 남음')
    button('다시 실행').click();expect(document.body.textContent).toContain('10장 중 8장 남음')
    button('모든 프레임에 크롭 적용').click();button('GIF 저장').click();await flush()
    expect(api.create).toHaveBeenCalledOnce();expect(api.create.mock.calls[0][0]).toMatchObject({crop:{x:0,y:0,width:77,height:120},deleted:[1,4]})
})
it('preserves a frame and refuses deleting the entire animation',async()=>{
    await setup()
    const to=[...document.querySelectorAll('label')].find(el=>el.firstChild?.textContent==='선택 끝 번호')!.querySelector('input')!
    to.value='10';button('범위 선택').click();button('선택 프레임 삭제').click()
    expect(document.body.textContent).toContain('최소 한 프레임은 남겨야 합니다.')
    expect(document.body.textContent).toContain('10장 중 10장 남음')
})
it('virtualizes numbered frames without thumbnails and keeps the current list during polling',async()=>{
    vi.useFakeTimers();const session=draft();session.frameCount=900
    const api=await setup(session)
    expect(document.querySelectorAll('.sbe-frame')).toHaveLength(48)
    expect(document.querySelector('.sbe-frame img')).toBeNull();const first=document.querySelector('.sbe-frame button')
    await vi.advanceTimersByTimeAsync(2100);await flush()
    expect(api.list).toHaveBeenCalledTimes(2);expect(document.querySelector('.sbe-frame button')).toBe(first)
    button('다음 48장').click();expect(document.querySelector('input[aria-label="49번 프레임 선택"]')).not.toBeNull()
    expect(document.querySelectorAll('.sbe-frame')).toHaveLength(48)
})
it('uses the same request ID after an uncertain network result',async()=>{
    const api=await setup();api.create.mockRejectedValueOnce(new Error('연결 끊김'))
    button('모든 프레임에 크롭 적용').click();button('GIF 저장').click();await flush();button('모든 프레임에 크롭 적용').click();button('GIF 저장').click();await flush()
    expect(api.create).toHaveBeenCalledTimes(2);expect(api.create.mock.calls[0][0].requestId).toBe(api.create.mock.calls[1][0].requestId)
})
it('restores a local crop and deletion draft when reopening a session',async()=>{
    await setup();document.querySelector<HTMLInputElement>('input[aria-label="3번 프레임 선택"]')!.click();button('선택 프레임 삭제').click()
    instance!.destroy();document.body.replaceChildren();await setup()
    expect(document.body.textContent).toContain('10장 중 9장 남음')
})
it('does not overwrite a focused crop field when job status refreshes',async()=>{
    vi.useFakeTimers();const api=await setup()
    const width=[...document.querySelectorAll('label')].find(el=>el.firstChild?.textContent==='너비')!.querySelector('input')!
    width.focus();width.value='123';width.dispatchEvent(new Event('input'))
    await vi.advanceTimersByTimeAsync(2100);await flush();expect(width.value).toBe('123')
    width.dispatchEvent(new Event('change'));button('모든 프레임에 크롭 적용').click();button('GIF 저장').click();await flush()
    expect(api.create.mock.calls[0][0].crop.width).toBe(123)
})
it('draws a fresh crop inside the full image using mouse or touch pointer events',async()=>{
    const api=await setup();const canvas=document.querySelector<HTMLCanvasElement>('.sbe-stage')!
    vi.spyOn(canvas,'getBoundingClientRect').mockReturnValue({x:0,y:0,top:0,left:0,bottom:240,right:320,width:320,height:240,toJSON:()=>({})})
    canvas.setPointerCapture=vi.fn()
    canvas.dispatchEvent(new PointerEvent('pointerdown',{pointerId:1,button:0,clientX:40,clientY:40,pointerType:'touch'}))
    canvas.dispatchEvent(new PointerEvent('pointermove',{pointerId:1,clientX:200,clientY:160,pointerType:'touch'}))
    canvas.dispatchEvent(new PointerEvent('pointerup',{pointerId:1,clientX:200,clientY:160,pointerType:'touch'}))
    button('모든 프레임에 크롭 적용').click();button('GIF 저장').click();await flush()
    expect(api.create.mock.calls[0][0].crop).toEqual({x:20,y:20,width:80,height:60})
})
it('releases the save control after a missing response and keeps the request ID for retry',async()=>{
    vi.useFakeTimers();const api=await setup();api.create.mockImplementationOnce(()=>new Promise(()=>{}))
    button('모든 프레임에 크롭 적용').click();button('GIF 저장').click();await flush();expect(button('GIF 저장').disabled).toBe(true)
    await vi.advanceTimersByTimeAsync(15001);await flush()
    expect(button('GIF 저장').disabled).toBe(false);expect(document.body.textContent).toContain('요청 응답을 받지 못했습니다.')
    button('모든 프레임에 크롭 적용').click();button('GIF 저장').click();await flush()
    expect(api.create.mock.calls[0][0].requestId).toBe(api.create.mock.calls[1][0].requestId)
})

it('represents long deletions as ranges and jumps across them',()=>{
    const deleted=new FrameRanges([{first:1,last:999999999}]);deleted.addRange(1000000001,1000000002)
    expect(deleted.length).toBe(1000000001);expect(deleted.ranges).toHaveLength(2)
    expect(deleted.next(10,1000000004)).toBe(1000000000)
    deleted.delete(500000000);expect(deleted.ranges).toHaveLength(3)
    expect(deleted.next(20,1000000004)).toBe(500000000)
})

it('switches format and quality without changing the crop or frame deletion',async()=>{
    const api=await setup();document.querySelector<HTMLInputElement>('input[aria-label="3번 프레임 선택"]')!.click();button('선택 프레임 삭제').click()
    const selects=[...document.querySelectorAll('select')]
    const format=selects.find(el=>el.querySelector('option[value="webp"]'))!
    format.value='webp';format.dispatchEvent(new Event('change'))
    const preset=selects.find(el=>el.querySelector('option[value="custom"]'))!
    preset.value='custom';preset.dispatchEvent(new Event('change'))
    const quality=[...document.querySelectorAll('label')].find(el=>el.firstChild?.textContent==='화질(1–100)')!.querySelector('input')!
    quality.value='91';quality.dispatchEvent(new Event('change'))
    const lossless=[...document.querySelectorAll('label')].find(el=>el.firstChild?.textContent==='WebP 무손실')!.querySelector('input')!;lossless.click()
    button('화질 확인').click();await flush()
    expect(api.create.mock.calls[0][0]).toMatchObject({kind:'webp',deleted:[2],crop:{width:160,height:120},preview:true,quality:{preset:'custom',quality:91,lossless:true}})
    expect(document.body.textContent).toContain('10장 중 9장 남음')
})

it('seeks the source and main preview when a numbered frame is clicked',async()=>{
    await setup(draft(),{id:1,title:'합성 영상',duration:1,src:'about:blank'})
    document.querySelector<HTMLButtonElement>('button[aria-label="6번 프레임 보기"]')!.click();await flush()
    expect(document.querySelector('video')!.currentTime).toBe(.5)
    expect(document.body.textContent).toContain('6 / 10장 · 0.50초')
    expect(document.querySelector('button[aria-label="6번 프레임 보기"]')!.getAttribute('aria-current')).toBe('true')
    expect(document.querySelector('.sbe-frames img')).toBeNull()
})

it('keeps the selected interval when seeking outside it and sends its frame boundaries',async()=>{
    const api=await setup()
    const start=document.querySelector<HTMLButtonElement>('button[aria-label="남길 구간 시작 손잡이"]')!
    const end=document.querySelector<HTMLButtonElement>('button[aria-label="남길 구간 종료 손잡이"]')!
    start.dispatchEvent(new KeyboardEvent('keydown',{key:'ArrowRight',bubbles:true}))
    end.dispatchEvent(new KeyboardEvent('keydown',{key:'ArrowLeft',bubbles:true}))
    button('이전 48장').click()
    document.querySelector<HTMLButtonElement>('button[aria-label="1번 프레임 보기"]')!.click()
    button('GIF 저장').click();await flush()
    expect(api.create.mock.calls[0][0]).toMatchObject({rangeStart:1,rangeEnd:9})
    expect(document.body.textContent).toContain('10장 중 8장 남음')
})

it('applies a crop to every frame explicitly and blocks saving a pending crop',async()=>{
    const api=await setup()
    const width=[...document.querySelectorAll('label')].find(el=>el.firstChild?.textContent==='너비')!.querySelector('input')!
    width.value='120';width.dispatchEvent(new Event('change'))
    button('GIF 저장').click();await flush();expect(api.create).not.toHaveBeenCalled()
    button('모든 프레임에 크롭 적용').click()
    document.querySelector<HTMLButtonElement>('button[aria-label="8번 프레임 보기"]')!.click()
    button('GIF 저장').click();await flush()
    expect(api.create.mock.calls[0][0].crop).toMatchObject({width:120,height:120})
})

it('uses original GIF frame timestamps instead of a resampled FPS timeline',async()=>{
    const session={...draft(),options:{...draft().options,nativeGIF:true,fps:100,end:.67},frameCount:6}
    await setup(session)
    document.querySelector<HTMLButtonElement>('button[aria-label="3번 프레임 보기"]')!.click();await flush()
    expect(document.body.textContent).toContain('3 / 6장 · 0.14초')
})

it('keeps a huge frame list bounded and reaches its last frame by scrolling',async()=>{
    vi.useFakeTimers();const session={...draft(),frameCount:100000000};await setup(session)
    const nav=document.querySelector<HTMLElement>('.sbe-frames')!;nav.scrollTop=8000000-320;nav.dispatchEvent(new Event('scroll'))
    await vi.advanceTimersByTimeAsync(25);await flush()
    expect(document.querySelectorAll('.sbe-frame').length).toBeLessThanOrEqual(48)
    expect(document.querySelector('button[aria-label="100000000번 프레임 보기"]')).not.toBeNull()
})

it('keeps an in-flight request stable while the next crop is edited',async()=>{
    const api=await setup();let release!:()=>void
    ;(api as any).selection=vi.fn(()=>new Promise(resolve=>{release=()=>resolve(ok('selection'))}))
    button('GIF 저장').click();await flush()
    const width=[...document.querySelectorAll('label')].find(el=>el.firstChild?.textContent==='너비')!.querySelector('input')!
    width.value='100';width.dispatchEvent(new Event('change'));button('모든 프레임에 크롭 적용').click()
    release();await flush()
    expect(api.create).toHaveBeenCalledOnce();expect(api.create.mock.calls[0][0].crop.width).toBe(160)
    expect(document.body.textContent).toContain('100×120px 적용됨')
})
