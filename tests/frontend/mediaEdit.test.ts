// @vitest-environment happy-dom
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { mountMediaEditor, boundedCrop, keptFrames, FrameRanges, formatEditTime } from '../../internal/webserver/web/editor.js'

const draft = () => ({ id:'a7b4c924-c2eb-4c50-8795-83272a0aad49',fileId:1,title:'합성 영상',options:{requestId:'session',fileId:1,start:0,end:1,fps:10,width:160,kind:'gif'},state:'ready',width:160,height:120,frameCount:10,progress:100,error:'',cancelling:false,createdAt:1 })
let instance: { destroy(): void } | undefined
const ok = (data: unknown) => ({ok:true,data})
const flush = async () => { for(let i=0;i<12;i++)await Promise.resolve() }
function pendingResult<T>() { let resolve!: (value:T)=>void; const promise=new Promise<T>(done=>{resolve=done}); return {promise,resolve}; }
const button = (label: string) => [...document.querySelectorAll('button')].find(el=>el.textContent===label)!
const context = {clearRect:vi.fn(),drawImage:vi.fn(),beginPath:vi.fn(),rect:vi.fn(),fill:vi.fn(),strokeRect:vi.fn(),fillRect:vi.fn()}
function editorLayoutHarness() {
    const frames = new Map<number, FrameRequestCallback>()
    const observers: { callback: () => void; observe: ReturnType<typeof vi.fn>; disconnect: ReturnType<typeof vi.fn> }[] = []
    let next = 0
    vi.spyOn(window,'requestAnimationFrame').mockImplementation(callback => { frames.set(++next,callback); return next })
    vi.spyOn(window,'cancelAnimationFrame').mockImplementation(id => { frames.delete(id) })
    vi.stubGlobal('ResizeObserver',class {
        observe = vi.fn()
        disconnect = vi.fn()
        constructor(public callback: () => void) { observers.push(this) }
    })
    return { frames, observers, flush: async () => {
        const pending = [...frames.values()]; frames.clear()
        for (const callback of pending) callback(0)
        await flush()
    } }
}
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

it('shows an overview for a 1000-frame video without preparing all precise frames',async()=>{
    const first=pendingResult<any>(),last=pendingResult<any>()
    const api={list:vi.fn(async()=>ok({sessions:[],jobs:[]})),prepare:vi.fn(),frames:vi.fn(),
        overview:vi.fn(({index}:any)=>index===0?first.promise:last.promise)}
    const host=document.createElement('div');document.body.append(host)
    instance=mountMediaEditor(host,{api,file:{id:1,title:'1000 프레임',kind:'video',duration:40,src:'/video'}})
    await flush()
    expect(document.querySelectorAll('.sbe-overview-frame')).toHaveLength(12)
    expect(document.querySelector('.sbe-overview')?.hasAttribute('hidden')).toBe(false)
    expect(api.overview).toHaveBeenCalledExactlyOnceWith({fileId:1,index:0})
    expect(api.prepare).not.toHaveBeenCalled();expect(api.frames).not.toHaveBeenCalled()
    first.resolve(ok({index:0,count:12,time:0,duration:40,frameCount:1000,image:'data:image/jpeg;base64,/9j/'}));await flush()
    expect(document.querySelector<HTMLImageElement>('.sbe-overview-frame img')!.hidden).toBe(false)
    expect(document.querySelectorAll('.sbe-overview-frame img:not([hidden])')).toHaveLength(1)
    expect(api.overview).toHaveBeenLastCalledWith({fileId:1,index:11})
    expect(api.prepare).not.toHaveBeenCalled()
    instance.destroy();instance=undefined
    last.resolve(ok({index:11,count:12,time:36.67,duration:40,frameCount:1000,image:'data:image/jpeg;base64,/9j/'}));await flush()
    expect(api.overview).toHaveBeenCalledTimes(2)
})

it('selects a small overview interval and prepares only that interval on demand',async()=>{
    let prepared:any=null
    const remaining=pendingResult<any>()
    const api={list:vi.fn(async()=>ok({sessions:prepared?[prepared]:[],jobs:[]})),
        overview:vi.fn(async({index}:any)=>index===0?ok({index,count:12,time:0,duration:1200,frameCount:36000,image:'data:image/jpeg;base64,/9j/'}):remaining.promise),
        prepare:vi.fn(async(req:any)=>{prepared={...draft(),options:{...req,quickVideo:false},frameCount:1000};return ok(prepared)})}
    const host=document.createElement('div');document.body.append(host)
    instance=mountMediaEditor(host,{api,file:{id:1,title:'긴 영상',kind:'video',duration:1200,src:'/video'}})
    const source=document.querySelector<HTMLVideoElement>('.sbe-source')!
    Object.defineProperties(source,{duration:{value:1200},videoWidth:{value:160},videoHeight:{value:120}})
    source.dispatchEvent(new Event('loadedmetadata'));await flush()
    document.querySelectorAll<HTMLButtonElement>('.sbe-overview-frame')[6].click();await flush()
    expect(source.currentTime).toBe(600)
    expect(api.prepare).not.toHaveBeenCalled()
    button('프레임 정밀 편집').click();await flush()
    const request=api.prepare.mock.calls[0][0]
    expect(request).toMatchObject({fileId:1,start:600,fullSource:false,originalFrames:true})
    expect(request.end-request.start).toBeCloseTo(1000/30,2)
    expect(document.querySelector<HTMLElement>('.sbe-overview')!.hidden).toBe(true)
    remaining.resolve(ok({index:11,count:12,time:1100,duration:1200,frameCount:36000,image:'data:image/jpeg;base64,/9j/'}));await flush()
    expect(api.overview).toHaveBeenCalledTimes(2)
})
it('separates available RAM and disk from estimates and keeps unavailable readings distinct from zero',async()=>{
    vi.useFakeTimers()
    const api=await setup()
    const resources=vi.fn(async()=>ok({memoryKnown:true,availableRam:10.81*1024**3,totalRam:31.10*1024**3,estimateKnown:true,lowRam:106*1024**2,highRam:286*1024**2,basis:'sampled',diskKnown:true,diskAvailable:133.31*1024**3,diskEstimateKnown:true,diskEstimate:26*1024**2,note:'미리보기 실측을 반영했습니다.',pressure:false,live:{known:false}}))
    Object.assign(api,{resources})
    await vi.advanceTimersByTimeAsync(450);await flush()
    const box=document.querySelector<HTMLElement>('[aria-label="처리 PC 자원"]')!
    const memory=box.querySelector<HTMLElement>('[aria-label="메모리 · RAM"]')!,disk=box.querySelector<HTMLElement>('[aria-label="저장 공간"]')!
    expect(memory.querySelector('strong')?.textContent).toBe('10.81')
    expect(memory.textContent).toContain('전체 31.10 GiB')
    expect(memory.querySelector('dd')?.textContent).toBe('106–286 MiB')
    expect(disk.querySelector('strong')?.textContent).toBe('133.31')
    expect(disk.querySelector('dd')?.textContent).toBe('26 MiB')
    expect(box.querySelector('.sbe-resource-badge')?.textContent).toBe('미리보기 실측 반영')
    expect(box.querySelector('.sbe-resource-warning')).toBeNull()
    resources.mockResolvedValue(ok({memoryKnown:false,availableRam:0,totalRam:0,estimateKnown:false,lowRam:0,highRam:0,basis:'calculated',diskKnown:false,diskAvailable:0,diskEstimateKnown:false,diskEstimate:0,note:'정보 확인 중',pressure:false,live:{known:false}}))
    await vi.advanceTimersByTimeAsync(2100);await flush()
    expect(box.querySelectorAll('.sbe-resource-value.is-unknown')).toHaveLength(2)
    expect(box.textContent).not.toContain('0 B')
    expect(box.textContent).toContain('추정 불가');expect(box.textContent).toContain('화질 확인 후 추정')
    expect(box.querySelector('.sbe-resource-badge')).toBeNull()
})
it('emphasizes RAM pressure and preserves live encoder readings without blocking save',async()=>{
    vi.useFakeTimers()
    const api=await setup()
    Object.assign(api,{resources:vi.fn(async()=>ok({memoryKnown:true,availableRam:100*1024**2,totalRam:8*1024**3,estimateKnown:true,lowRam:106*1024**2,highRam:286*1024**2,basis:'calculated',diskKnown:true,diskAvailable:20*1024**3,diskEstimateKnown:false,pressure:true,live:{known:true,currentBytes:48*1024**2,observedPeakBytes:64*1024**2}}))})
    await vi.advanceTimersByTimeAsync(450);await flush()
    const box=document.querySelector<HTMLElement>('[aria-label="처리 PC 자원"]')!
    expect(box.querySelector('.sbe-resource-memory')?.classList.contains('is-warning')).toBe(true)
    expect(box.querySelector('.sbe-resource-warning')?.textContent).toContain('메모리 여유 부족 예상')
    expect(box.querySelector('.sbe-resource-warning')?.textContent).toContain('그대로 실행할 수도 있습니다.')
    expect(box.querySelector('.sbe-resource-live')?.textContent).toContain('48 MiB')
    expect(box.querySelector('.sbe-resource-live')?.textContent).toContain('관측 최대 64 MiB')
    expect(box.querySelector('.sbe-resource-badge')?.textContent).toBe('계산 추정')
    expect(button('GIF 저장').disabled).toBe(false)
})
it('keeps immutable output-frame IDs and bounds arbitrary crop coordinates',()=>{
    expect(keptFrames(6,[1,3])).toEqual([0,2,4,5])
    expect(boundedCrop({x:100,y:-5,width:80,height:999},160,120)).toEqual({x:80,y:0,width:80,height:120})
})
it('shows automatic retention for both drafts and jobs and still allows manual deletion',async()=>{
    const api=await setup()
    const count=document.querySelector<HTMLElement>('[aria-label="편집 초안 보관 개수"]')!
    const jobs=document.querySelector<HTMLElement>('[aria-label="작업 내역 보관 개수"]')!
    const hint=[...document.querySelectorAll<HTMLElement>('.sbe-history p')].find(el=>el.textContent?.startsWith('편집 초안과 작업 내역은 각각'))!
    expect(count.textContent).toBe('편집 초안 1개 · 최근 20개 자동 보관')
    expect(jobs.textContent).toBe('작업 내역 0개 · 최근 20개 자동 보관')
    expect(hint.hidden).toBe(false)
    const sessions=Array.from({length:20},(_,n)=>({...draft(),id:`draft-${n}`}))
    api.list.mockResolvedValue(ok({sessions,jobs:[]}))
    button('목록 새로고침').click();await flush()
    expect(count.textContent).toBe('편집 초안 20개 · 최근 20개 자동 보관')
    expect(hint.textContent).toContain('오래된 항목부터 자동 정리');expect(hint.textContent).toContain('원본 영상과 저장된 결과는 유지됩니다.')
    expect(api.release).not.toHaveBeenCalled()
    expect(document.querySelectorAll('.sbe-history .sbe-record')).toHaveLength(20)
    api.list.mockResolvedValue(ok({sessions:sessions.slice(1),jobs:[]}))
    button('편집 초안 삭제').click();await flush()
    expect(api.release).toHaveBeenCalledExactlyOnceWith({id:'draft-0'})
    expect(count.textContent).toBe('편집 초안 19개 · 최근 20개 자동 보관');expect(hint.hidden).toBe(false)
})
it('clears an automatically removed draft and its saved editing state and disables retry without a ready draft',async()=>{
    const api=await setup()
    const key='storagebucket-edit-v1:'+draft().id
    localStorage.setItem(key,'{}')
    localStorage.setItem('other-setting','keep')
    api.list.mockResolvedValue(ok({sessions:[],jobs:[{id:'old-job',sessionId:draft().id,state:'failed',options:{kind:'gif',sessionId:draft().id},error:'failed',progress:0}]}))
    button('목록 새로고침').click();await flush()
    expect(localStorage.getItem(key)).toBeNull()
    expect(localStorage.getItem('other-setting')).toBe('keep')
    expect(document.body.textContent).toContain('오래된 편집 초안이 정리되었습니다.')
    expect(button('GIF 저장').disabled).toBe(true)
    expect(button('같은 설정으로 다시 실행').disabled).toBe(true)
    expect(api.release).not.toHaveBeenCalled()
})
it('formats editing times as hours, minutes, seconds, and hundredths without wrapping at a day',()=>{
    for(const [seconds,text] of [[0,'00:00:00.00'],[134.37,'00:02:14.37'],[217,'00:03:37.00'],[59.999,'00:01:00.00'],[3599.999,'01:00:00.00'],[90061.25,'25:01:01.25']] as const)
        expect(formatEditTime(seconds)).toBe(text)
})
it('shows the original video immediately and prepares only a selected interval for precise editing',async()=>{
    const session={...draft(),state:'pending',frameCount:0,options:{...draft().options,fullSource:true,nativeVideo:true,start:0,end:0,fps:1000}}
    const api={list:vi.fn(async()=>ok({sessions:[],jobs:[]})),prepare:vi.fn(async()=>ok(session))}
    const host=document.createElement('div');document.body.append(host)
    instance=mountMediaEditor(host,{api,file:{id:1,title:'긴 영상',duration:120,kind:'video',src:'/media?path=video.mp4'}});await flush()
    const video=document.querySelector<HTMLVideoElement>('.sbe-source')!
    expect(video.hidden).toBe(false);expect(video.controls).toBe(true)
    expect(document.querySelector<HTMLElement>('.sbe-source-surface')!.hidden).toBe(false)
    const start=[...document.querySelectorAll('label')].find(el=>el.firstChild?.textContent==='시작 시간(초)')!
    expect(start.hidden).toBe(true)
    expect(api.prepare).not.toHaveBeenCalled()
    button('프레임 정밀 편집').click();await flush()
    expect(api.prepare).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({fileId:1,originalFrames:true,fullSource:false,start:0,end:120,kind:'gif'}))
    expect(video.hidden).toBe(false)
    expect(document.body.textContent).toContain('선택 구간')
})
it('gives a source-selection instruction when the editor is opened without a file',async()=>{
    const host=document.createElement('div');document.body.append(host)
    instance=mountMediaEditor(host,{api:{list:async()=>ok({sessions:[],jobs:[]})}});await flush()
    const placeholder=document.querySelector<HTMLElement>('.sbe-crop-placeholder')!
    expect(placeholder.hidden).toBe(false)
    expect(placeholder.textContent).toContain('영상 카드의 ⋯ 메뉴')
    expect(document.querySelector<HTMLElement>('.sbe-source-surface')!.hidden).toBe(false)
})
it('does not show a different original video when a historical session is preparing',async()=>{
    await setup({...draft(),fileId:2,state:'preparing'},{id:1,kind:'video',src:'/media?path=video.mp4'})
    expect(document.querySelector<HTMLVideoElement>('video')!.hidden).toBe(true)
    expect(document.querySelector<HTMLElement>('.sbe-crop-placeholder')!.hidden).toBe(false)
})
it('uses original video timestamps for frame seeking and saving a kept range',async()=>{
    const session={...draft(),options:{...draft().options,fullSource:true,nativeVideo:true,fps:1000,end:.67},frameCount:6}
    const api=await setup(session,{id:1,kind:'video',src:'/media?path=video.mp4'})
    button('다음 프레임').click();await flush()
    expect(document.querySelector<HTMLVideoElement>('video')!.currentTime).toBe(.03)
    const range=document.querySelector<HTMLElement>('[aria-label="남길 구간"]')!
    expect(range).not.toBeNull()
    button('GIF 저장').click();await flush()
    expect(api.create).toHaveBeenCalledWith(expect.objectContaining({rangeStart:0,rangeEnd:6}))
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

it('opens a session at the top crop surface and moves focus away from the history button without scrolling',async()=>{
    const reveal=vi.spyOn(HTMLElement.prototype,'scrollIntoView').mockImplementation(()=>{
        document.querySelector<HTMLElement>('.sbe-main-pane')!.scrollTop=600
    })
    await setup()
    const pane=document.querySelector<HTMLElement>('.sbe-main-pane')!,stage=document.querySelector<HTMLElement>('.sbe-stage')!
    expect(pane.scrollTop).toBe(0);expect(document.activeElement).toBe(stage)
    const focus=vi.spyOn(stage,'focus')
    pane.scrollTop=700
    button('열기').focus();button('열기').click();await flush()
    expect(pane.scrollTop).toBe(0)
    expect(focus).toHaveBeenCalledWith({preventScroll:true})
    expect(document.activeElement).toBe(stage)
    expect(document.querySelector('.sbe-frame.is-current')?.getAttribute('data-frame')).toBe('0')
    expect(reveal).not.toHaveBeenCalled()
})

it('keeps the reading position when a session finishes preparing or its status refreshes',async()=>{
    vi.useFakeTimers()
    const pending={...draft(),state:'preparing'},api=await setup(pending)
    const pane=document.querySelector<HTMLElement>('.sbe-main-pane')!
    pane.scrollTop=320
    api.list.mockResolvedValue(ok({sessions:[{...pending,state:'ready'}],jobs:[]}))
    await vi.advanceTimersByTimeAsync(2100);await flush()
    expect(pane.scrollTop).toBe(320)
    pane.scrollTop=480
    await vi.advanceTimersByTimeAsync(2100);await flush()
    expect(pane.scrollTop).toBe(480)
    button('열기').click();await flush()
    expect(pane.scrollTop).toBe(0)
    expect(document.activeElement).toBe(document.querySelector('.sbe-stage'))
})

it('fits previews to a resized workspace without refetching frames or rebuilding the frame list',async()=>{
    const layout=editorLayoutHarness()
    const requests=vi.spyOn(Image.prototype,'src','set')
    await setup({...draft(),width:1600,height:1200})
    const stage=document.querySelector<HTMLCanvasElement>('.sbe-stage')!
    const preview=document.querySelector<HTMLCanvasElement>('[aria-label="크롭·삭제 적용 미리보기"]')!
    const body=document.querySelector<HTMLElement>('.sbe-body')!
    const firstFrame=document.querySelector('.sbe-frame')
    let width=1000,height=800
    vi.spyOn(stage.parentElement!,'clientWidth','get').mockImplementation(()=>width)
    vi.spyOn(preview.parentElement!,'clientWidth','get').mockImplementation(()=>width)
    vi.spyOn(body,'clientHeight','get').mockImplementation(()=>height)
    const fetched=requests.mock.calls.length
    for(let i=0;i<4;i++)window.dispatchEvent(new Event('resize'))
    layout.observers[0].callback()
    expect(layout.frames.size).toBe(1)
    await layout.flush()
    expect(parseFloat(stage.style.width)).toBe(640)
    expect(parseFloat(stage.style.height)).toBe(480)
    expect(preview.width).toBe(640);expect(preview.height).toBe(480)
    width=2200;height=1200
    window.dispatchEvent(new Event('resize'));await layout.flush()
    expect(parseFloat(stage.style.width)).toBe(960)
    expect(parseFloat(preview.style.height)).toBe(720)
    expect(preview.width).toBe(960)
    width=300;height=400
    layout.observers[0].callback();await layout.flush()
    expect(parseFloat(stage.style.width)).toBe(300)
    expect(parseFloat(stage.style.height)).toBe(225)
    expect(preview.width).toBe(300);expect(preview.height).toBe(225)
    expect(requests).toHaveBeenCalledTimes(fetched)
    expect(document.querySelector('.sbe-frame')).toBe(firstFrame)
    expect(layout.observers[0].observe.mock.calls.map(call=>call[0])).toEqual([body,document.querySelector('.sbe-main-pane'),document.querySelector('.sbe-frames')])
})

it('preserves crop drafts, deletions, selected frames, focus and scrolling across window changes',async()=>{
    const layout=editorLayoutHarness(),api=await setup()
    const width=[...document.querySelectorAll('label')].find(el=>el.firstChild?.textContent==='너비')!.querySelector('input')!
    width.value='120';width.dispatchEvent(new Event('change'));button('모든 프레임에 크롭 적용').click()
    document.querySelector<HTMLInputElement>('[aria-label="3번 프레임 선택"]')!.click();button('선택 프레임 삭제').click()
    document.querySelector<HTMLInputElement>('[aria-label="4번 프레임 선택"]')!.click();await flush()
    const pane=document.querySelector<HTMLElement>('.sbe-main-pane')!,nav=document.querySelector<HTMLElement>('.sbe-frames')!
    pane.scrollTop=250;nav.scrollTop=44
    width.focus();width.value='119'
    window.dispatchEvent(new Event('resize'));await layout.flush()
    expect(width.value).toBe('119');expect(document.activeElement).toBe(width)
    expect(pane.scrollTop).toBe(250);expect(nav.scrollTop).toBe(44)
    expect(document.querySelector<HTMLInputElement>('[aria-label="4번 프레임 선택"]')!.checked).toBe(true)
    expect(document.body.textContent).toContain('10장 중 9장 남음')
    expect(document.body.textContent).toContain('4 / 10장')
    expect(api.list).toHaveBeenCalledOnce()
    button('GIF 저장').click();await flush()
    expect(api.create.mock.calls[0][0]).toMatchObject({crop:{x:0,y:0,width:120,height:120},deleted:[2]})
})

it('disconnects resize observation and cancels a pending layout when the editor closes',async()=>{
    const layout=editorLayoutHarness(),removed=vi.spyOn(window,'removeEventListener')
    await setup()
    window.dispatchEvent(new Event('resize'))
    const stale=[...layout.frames.values()][0]
    const draws=context.drawImage.mock.calls.length
    instance!.destroy()
    expect(layout.frames.size).toBe(0)
    expect(layout.observers[0].disconnect).toHaveBeenCalledOnce()
    expect(removed).toHaveBeenCalledWith('resize',expect.any(Function))
    stale(0);layout.observers[0].callback();window.dispatchEvent(new Event('resize'));await layout.flush()
    expect(context.drawImage).toHaveBeenCalledTimes(draws)
    expect(document.querySelector('.sb-editor')).toBeNull()
})

it('resizes through the window listener when ResizeObserver is unavailable',async()=>{
    const layout=editorLayoutHarness()
    vi.stubGlobal('ResizeObserver',undefined)
    await setup()
    const stage=document.querySelector<HTMLCanvasElement>('.sbe-stage')!
    vi.spyOn(stage.parentElement!,'clientWidth','get').mockReturnValue(240)
    window.dispatchEvent(new Event('resize'));await layout.flush()
    expect(stage.style.width).toBe('240px');expect(stage.style.height).toBe('180px')
    expect(layout.observers).toHaveLength(0)
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
    button('새 영역 그리기').click()
    vi.spyOn(canvas,'getBoundingClientRect').mockReturnValue({x:0,y:0,top:0,left:0,bottom:240,right:320,width:320,height:240,toJSON:()=>({})})
    canvas.setPointerCapture=vi.fn()
    canvas.dispatchEvent(new PointerEvent('pointerdown',{pointerId:1,button:0,clientX:40,clientY:40,pointerType:'touch'}))
    canvas.dispatchEvent(new PointerEvent('pointermove',{pointerId:1,clientX:200,clientY:160,pointerType:'touch'}))
    canvas.dispatchEvent(new PointerEvent('pointerup',{pointerId:1,clientX:200,clientY:160,pointerType:'touch'}))
    button('모든 프레임에 크롭 적용').click();button('GIF 저장').click();await flush()
    expect(api.create.mock.calls[0][0].crop).toEqual({x:20,y:20,width:80,height:60})
})

function cropStage() {
    const canvas=document.querySelector<HTMLCanvasElement>('.sbe-stage')!
    vi.spyOn(canvas,'getBoundingClientRect').mockReturnValue({x:0,y:0,top:0,left:0,bottom:240,right:320,width:320,height:240,toJSON:()=>({})})
    canvas.setPointerCapture=vi.fn()
    return canvas
}
function dragCrop(canvas: HTMLCanvasElement, from: [number,number], to: [number,number], pointerType='mouse') {
    canvas.dispatchEvent(new PointerEvent('pointerdown',{pointerId:1,button:0,clientX:from[0],clientY:from[1],pointerType}))
    canvas.dispatchEvent(new PointerEvent('pointermove',{pointerId:1,clientX:to[0],clientY:to[1],pointerType}))
    canvas.dispatchEvent(new PointerEvent('pointerup',{pointerId:1,clientX:to[0],clientY:to[1],pointerType}))
}
it('shows a 4x crop magnifier only during a left-button crop interaction and hides it immediately on release',async()=>{
    const requests=vi.spyOn(Image.prototype,'src','set')
    await setup();const canvas=cropStage(),magnifier=document.querySelector<HTMLElement>('.sbe-magnifier')!
    const fetched=requests.mock.calls.length
    expect(magnifier.hidden).toBe(true);expect(magnifier.getAttribute('aria-hidden')).toBe('true')
    canvas.dispatchEvent(new PointerEvent('pointermove',{pointerId:1,clientX:320,clientY:120,pointerType:'mouse'}))
    expect(magnifier.hidden).toBe(true)
    canvas.dispatchEvent(new PointerEvent('pointerdown',{pointerId:1,button:2,clientX:320,clientY:120,pointerType:'mouse'}))
    expect(magnifier.hidden).toBe(true);expect(canvas.setPointerCapture).not.toHaveBeenCalled()
    canvas.dispatchEvent(new PointerEvent('pointerdown',{pointerId:1,button:0,buttons:1,clientX:320,clientY:120,pointerType:'mouse'}))
    expect(magnifier.hidden).toBe(false)
    const sample=context.drawImage.mock.lastCall!
    expect((sample[7] / sample[3]) / (320 / 160)).toBeCloseTo(4)
    canvas.dispatchEvent(new PointerEvent('pointermove',{pointerId:1,buttons:1,clientX:280,clientY:120,pointerType:'mouse'}))
    canvas.dispatchEvent(new PointerEvent('pointerup',{pointerId:2,clientX:280,clientY:120,pointerType:'mouse'}))
    expect(magnifier.hidden).toBe(false)
    canvas.dispatchEvent(new PointerEvent('pointerup',{pointerId:1,clientX:280,clientY:120,pointerType:'mouse'}))
    expect(magnifier.hidden).toBe(true)
    await flush();expect(magnifier.hidden).toBe(true)
    expect(requests).toHaveBeenCalledTimes(fetched)
})

it.each(['draw','move','corner'])('magnifies %s operations and preserves their crop result',async(mode)=>{
    const api=await setup(),canvas=cropStage(),magnifier=document.querySelector<HTMLElement>('.sbe-magnifier')!
    let start=[80,60],end=[240,180],expected={x:40,y:30,width:80,height:60}
    if(mode==='draw')button('새 영역 그리기').click()
    if(mode==='move'){
        for(const [label,value] of [['너비','80'],['높이','60'],['왼쪽','20'],['위쪽','15']]){
            const input=[...document.querySelectorAll('label')].find(el=>el.firstChild?.textContent===label)!.querySelector('input')!
            input.value=value;input.dispatchEvent(new Event('change'))
        }
        start=[120,90];end=[160,110];expected={x:40,y:25,width:80,height:60}
    }
    if(mode==='corner'){start=[320,240];end=[280,200];expected={x:0,y:0,width:140,height:100}}
    await flush()
    canvas.dispatchEvent(new PointerEvent('pointerdown',{pointerId:1,button:0,buttons:1,clientX:start[0],clientY:start[1],pointerType:'mouse'}))
    canvas.dispatchEvent(new PointerEvent('pointermove',{pointerId:1,buttons:1,clientX:end[0],clientY:end[1],pointerType:'mouse'}))
    expect(magnifier.hidden).toBe(false)
    canvas.dispatchEvent(new PointerEvent('pointerup',{pointerId:1,clientX:end[0],clientY:end[1],pointerType:'mouse'}))
    expect(magnifier.hidden).toBe(true)
    button('모든 프레임에 크롭 적용').click();button('GIF 저장').click();await flush()
    expect(api.create.mock.calls[0][0].crop).toEqual(expected)
})

it.each(['pointercancel','lostpointercapture','blur'])('hides the magnifier and restores the crop on %s',async(type)=>{
    await setup();const canvas=cropStage(),magnifier=document.querySelector<HTMLElement>('.sbe-magnifier')!
    canvas.dispatchEvent(new PointerEvent('pointerdown',{pointerId:1,button:0,buttons:1,clientX:320,clientY:120,pointerType:'mouse'}))
    canvas.dispatchEvent(new PointerEvent('pointermove',{pointerId:1,buttons:1,clientX:280,clientY:120,pointerType:'mouse'}))
    expect(magnifier.hidden).toBe(false)
    if(type==='blur')window.dispatchEvent(new Event('blur'))
    else canvas.dispatchEvent(new PointerEvent(type,{pointerId:1}))
    expect(magnifier.hidden).toBe(true)
    const width=[...document.querySelectorAll('label')].find(el=>el.firstChild?.textContent==='너비')!.querySelector('input')!
    expect(width.value).toBe('160');expect(button('실행 취소').disabled).toBe(true)
    await flush();expect(magnifier.hidden).toBe(true)
})

it('removes an active magnifier and its window listener when closing the editor',async()=>{
    const removed=vi.spyOn(window,'removeEventListener')
    await setup();const canvas=cropStage(),magnifier=document.querySelector<HTMLElement>('.sbe-magnifier')!
    canvas.dispatchEvent(new PointerEvent('pointerdown',{pointerId:1,button:0,buttons:1,clientX:320,clientY:120,pointerType:'mouse'}))
    expect(magnifier.hidden).toBe(false)
    instance!.destroy();window.dispatchEvent(new Event('blur'));await flush()
    expect(document.querySelector('.sbe-magnifier')).toBeNull()
    expect(removed).toHaveBeenCalledWith('blur',expect.any(Function))
})
it.each([
    {corner:'top-left',from:[0,0],to:[40,30],crop:{x:20,y:15,width:140,height:105},pointer:'mouse'},
    {corner:'top-right',from:[320,0],to:[280,30],crop:{x:0,y:15,width:140,height:105},pointer:'touch'},
    {corner:'bottom-left',from:[0,240],to:[40,210],crop:{x:20,y:0,width:140,height:105},pointer:'mouse'},
    {corner:'bottom-right',from:[320,240],to:[280,210],crop:{x:0,y:0,width:140,height:105},pointer:'touch'},
])('resizes the existing $corner corner while keeping its opposite corner fixed',async({from,to,crop,pointer})=>{
    const api=await setup(),canvas=cropStage()
    expect(button('영역 이동·크기 조절').getAttribute('aria-pressed')).toBe('true')
    dragCrop(canvas,from as [number,number],to as [number,number],pointer)
    button('모든 프레임에 크롭 적용').click();button('GIF 저장').click();await flush()
    expect(api.create.mock.calls[0][0].crop).toEqual(crop)
})
it('prioritizes existing corners even in draw mode and preserves the pointer grab offset',async()=>{
    const api=await setup(),canvas=cropStage()
    button('새 영역 그리기').click()
    dragCrop(canvas,[312,232],[292,212],'touch')
    expect(button('영역 이동·크기 조절').getAttribute('aria-pressed')).toBe('true')
    button('실행 취소').click()
    const width=[...document.querySelectorAll('label')].find(el=>el.firstChild?.textContent==='너비')!.querySelector('input')!
    expect(width.value).toBe('160')
    button('다시 실행').click();expect(width.value).toBe('150')
    button('모든 프레임에 크롭 적용').click();button('GIF 저장').click();await flush()
    expect(api.create.mock.calls[0][0].crop).toEqual({x:0,y:0,width:150,height:110})
})
it.each([
    {edge:'left',from:[0,120],to:[40,150],crop:{x:20,y:0,width:140,height:120},pointer:'mouse'},
    {edge:'right',from:[320,120],to:[280,90],crop:{x:0,y:0,width:140,height:120},pointer:'touch'},
    {edge:'top',from:[160,0],to:[200,30],crop:{x:0,y:15,width:160,height:105},pointer:'mouse'},
    {edge:'bottom',from:[160,240],to:[130,210],crop:{x:0,y:0,width:160,height:105},pointer:'touch'},
])('resizes only the grabbed $edge edge even when the pointer also moves along that edge',async({from,to,crop,pointer})=>{
    const api=await setup(),canvas=cropStage()
    dragCrop(canvas,from as [number,number],to as [number,number],pointer)
    button('모든 프레임에 크롭 적용').click();button('GIF 저장').click();await flush()
    expect(api.create.mock.calls[0][0].crop).toEqual(crop)
})
it('keeps edge resizing in draw mode, preserves its grab offset and supports undo and redo',async()=>{
    const api=await setup(),canvas=cropStage()
    button('새 영역 그리기').click()
    dragCrop(canvas,[312,120],[292,160],'touch')
    const width=[...document.querySelectorAll('label')].find(el=>el.firstChild?.textContent==='너비')!.querySelector('input')!
    button('실행 취소').click();expect(width.value).toBe('160')
    button('다시 실행').click();expect(width.value).toBe('150')
    button('모든 프레임에 크롭 적용').click();button('GIF 저장').click();await flush()
    expect(api.create.mock.calls[0][0].crop).toEqual({x:0,y:0,width:150,height:120})
})
it('shows edge and corner resize cursors and restores the rectangle after a cancelled edge drag',async()=>{
    await setup();const canvas=cropStage()
    for(const [x,y,cursor] of [[320,120,'ew-resize'],[160,0,'ns-resize'],[0,0,'nwse-resize'],[320,0,'nesw-resize'],[160,120,'move']] as const) {
        canvas.dispatchEvent(new PointerEvent('pointermove',{pointerId:1,clientX:x,clientY:y,pointerType:'mouse'}))
        expect(canvas.style.cursor).toBe(cursor)
    }
    canvas.dispatchEvent(new PointerEvent('pointerdown',{pointerId:1,button:0,clientX:320,clientY:120}))
    canvas.dispatchEvent(new PointerEvent('pointermove',{pointerId:1,clientX:280,clientY:120}))
    const width=[...document.querySelectorAll('label')].find(el=>el.firstChild?.textContent==='너비')!.querySelector('input')!
    expect(width.value).toBe('140')
    canvas.dispatchEvent(new PointerEvent('pointercancel',{pointerId:1}))
    expect(width.value).toBe('160');expect(button('실행 취소').disabled).toBe(true)
})
it('moves an existing crop without redrawing and ignores outside drags in adjust mode',async()=>{
    const api=await setup(),canvas=cropStage()
    for(const [label,value] of [['너비','80'],['높이','60'],['왼쪽','20'],['위쪽','15']]) {
        const input=[...document.querySelectorAll('label')].find(el=>el.firstChild?.textContent===label)!.querySelector('input')!
        input.value=value;input.dispatchEvent(new Event('change'))
    }
    dragCrop(canvas,[280,200],[260,180])
    expect(canvas.setPointerCapture).not.toHaveBeenCalled()
    dragCrop(canvas,[120,90],[160,110])
    button('실행 취소').click()
    const left=[...document.querySelectorAll('label')].find(el=>el.firstChild?.textContent==='왼쪽')!.querySelector('input')!
    expect(left.value).toBe('20')
    button('다시 실행').click();expect(left.value).toBe('40')
    button('모든 프레임에 크롭 적용').click();button('GIF 저장').click();await flush()
    expect(api.create.mock.calls[0][0].crop).toEqual({x:40,y:25,width:80,height:60})
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
    const compression=[...document.querySelectorAll('label')].find(el=>el.firstChild?.textContent==='WebP 압축 방식')!.querySelector('select')!
    compression.value='lossless';compression.dispatchEvent(new Event('change'))
    button('화질 확인').click();await flush()
    expect(api.create.mock.calls[0][0]).toMatchObject({kind:'webp',deleted:[2],crop:{width:160,height:120},preview:true,quality:{preset:'custom',quality:91,lossless:true}})
    expect(document.body.textContent).toContain('10장 중 9장 남음')
})

it('sends maximum quality for both save and preview, preserving edits and separate format preferences',async()=>{
    const api=await setup()
    const labelled = (label: string) => [...document.querySelectorAll('label')].find(el=>el.firstChild?.textContent===label)!
    const preset=labelled('화질 프리셋').querySelector('select')!
    const quality=labelled('화질(1–100)').querySelector('input')!
    expect(labelled('압축률').querySelector('select')).not.toBeNull()
    expect(document.body.textContent).not.toContain('압축 노력')
    expect(preset.querySelector('option[value="max"]')!.textContent).toBe('최대 화질')
    expect(preset.value).toBe('balanced')
    document.querySelector<HTMLInputElement>('input[aria-label="3번 프레임 선택"]')!.click();button('선택 프레임 삭제').click()
    preset.value='max';preset.dispatchEvent(new Event('change'))
    expect(quality.value).toBe('100');expect(quality.disabled).toBe(true)
    button('GIF 저장').click();await flush()
    expect(api.create.mock.calls[0][0]).toMatchObject({kind:'gif',deleted:[2],crop:{width:160,height:120},quality:{preset:'max',quality:100,effort:'normal',lossless:false}})
    const format=labelled('작업 종류').querySelector('select')!
    format.value='webp';format.dispatchEvent(new Event('change'))
    expect(preset.value).toBe('balanced')
    preset.value='max';preset.dispatchEvent(new Event('change'))
    button('화질 확인').click();await flush()
    expect(api.create.mock.calls[1][0]).toMatchObject({kind:'webp',preview:true,deleted:[2],quality:{preset:'max',quality:100,lossless:false}})
    expect(JSON.parse(localStorage.getItem('storagebucket-quality:webp')!)).toMatchObject({preset:'max',quality:100})
    format.value='gif';format.dispatchEvent(new Event('change'))
    expect(preset.value).toBe('max');expect(quality.value).toBe('100')
    instance!.destroy();document.body.replaceChildren();await setup()
    expect(labelled('화질 프리셋').querySelector('select')!.value).toBe('max')
    expect(labelled('화질(1–100)').querySelector('input')!.value).toBe('100')
})

it('keeps algorithm controls in collapsed advanced settings and remembers GIF dithering independently of quality',async()=>{
    const api=await setup()
    const labelled=(label:string)=>[...document.querySelectorAll('label')].find(el=>el.firstChild?.textContent===label)!
    const advanced=document.querySelector<HTMLDetailsElement>('.sbe-quality-advanced')!
    expect(advanced.open).toBe(false);expect(advanced.querySelector('summary')!.textContent).toBe('고급 설정')
    const dither=labelled('GIF 디더링').querySelector('select')!
    expect(dither.closest('details')).toBe(advanced);expect(dither.value).toBe('auto');expect(dither.disabled).toBe(false)
    expect(labelled('화질(1–100)').hidden).toBe(true)
    advanced.open=true;dither.value='floyd_steinberg';dither.dispatchEvent(new Event('change'))
    const preset=labelled('화질 프리셋').querySelector('select')!;preset.value='max';preset.dispatchEvent(new Event('change'))
    expect(dither.value).toBe('floyd_steinberg');expect(dither.disabled).toBe(false)
    button('화질 확인').click();await flush();button('GIF 저장').click();await flush()
    expect(api.create).toHaveBeenCalledTimes(2)
    for(const [request] of api.create.mock.calls)expect(request.quality).toMatchObject({preset:'max',quality:100,dither:'floyd_steinberg'})
    instance!.destroy();document.body.replaceChildren();await setup()
    expect(labelled('GIF 디더링').querySelector('select')!.value).toBe('floyd_steinberg')
    expect(document.querySelector<HTMLDetailsElement>('.sbe-quality-advanced')!.open).toBe(false)
})

it('shows simple WebP compression outside advanced settings and preserves lossless against optimization presets',async()=>{
    const api=await setup()
    const labelled=(label:string)=>[...document.querySelectorAll('label')].find(el=>el.firstChild?.textContent===label)!
    const change=(select:HTMLSelectElement,value:string)=>{select.value=value;select.dispatchEvent(new Event('change'))}
    const format=labelled('작업 종류').querySelector('select')!
    change(format,'webp')
    const advanced=document.querySelector<HTMLDetailsElement>('.sbe-quality-advanced')!
    const compression=labelled('WebP 압축 방식').querySelector('select')!,optimization=labelled('WebP 최적화 유형').querySelector('select')!
    expect(compression.closest('details')).toBeNull();expect(optimization.closest('details')).toBe(advanced)
    expect(advanced.open).toBe(false);expect(labelled('GIF 디더링').hidden).toBe(true)
    advanced.open=true;change(optimization,'text')
    button('화질 확인').click();await flush()
    expect(api.create.mock.calls[0][0].quality).toMatchObject({lossless:false,webpPreset:'text'})
    expect(labelled('압축률').querySelector('select')!.disabled).toBe(true)
    change(compression,'lossless')
    expect(optimization.disabled).toBe(true);expect(optimization.value).toBe('text')
    button('WebP 저장').click();await flush()
    expect(api.create.mock.calls[1][0].quality).toMatchObject({lossless:true})
    expect(api.create.mock.calls[1][0].quality.webpPreset).toBeUndefined()
    change(format,'gif');expect(labelled('WebP 압축 방식').hidden).toBe(true)
    expect(labelled('GIF 디더링').querySelector('select')!.value).toBe('auto')
    change(format,'webp');expect(compression.value).toBe('lossless');expect(optimization.value).toBe('text')
    change(compression,'lossy');expect(optimization.disabled).toBe(false)
    button('WebP 저장').click();await flush()
    expect(api.create.mock.calls[2][0].quality).toMatchObject({lossless:false,webpPreset:'text'})
})

it('migrates saved lossless preferences and ignores unknown saved algorithms',async()=>{
    localStorage.setItem('storagebucket-quality:webp',JSON.stringify({preset:'custom',quality:91,effort:'precise',lossless:true,webpPreset:'not-supported'}))
    localStorage.setItem('storagebucket-quality:gif',JSON.stringify({preset:'balanced',dither:'not-supported'}))
    await setup()
    const labelled=(label:string)=>[...document.querySelectorAll('label')].find(el=>el.firstChild?.textContent===label)!
    expect(labelled('GIF 디더링').querySelector('select')!.value).toBe('auto')
    const format=labelled('작업 종류').querySelector('select')!;format.value='webp';format.dispatchEvent(new Event('change'))
    expect(labelled('WebP 압축 방식').querySelector('select')!.value).toBe('lossless')
    expect(labelled('WebP 최적화 유형').querySelector('select')!.value).toBe('none')
})

it('seeks the source and main preview when a numbered frame is clicked',async()=>{
    await setup(draft(),{id:1,title:'합성 영상',duration:1,src:'about:blank'})
    document.querySelector<HTMLButtonElement>('button[aria-label="6번 프레임 보기"]')!.click();await flush()
    expect(document.querySelector('video')!.currentTime).toBe(.5)
    expect(document.body.textContent).toContain('6 / 10장 · 00:00:00.50')
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
    expect(document.body.textContent).toContain('3 / 6장 · 00:00:00.14')
})

it('keeps a huge frame list bounded and reaches its last frame by scrolling',async()=>{
    vi.useFakeTimers();const session={...draft(),frameCount:100000000};await setup(session)
    const pane=document.querySelector<HTMLElement>('.sbe-main-pane')!;pane.scrollTop=600
    const nav=document.querySelector<HTMLElement>('.sbe-frames')!;nav.scrollTop=8000000-320;nav.dispatchEvent(new Event('scroll'))
    await vi.advanceTimersByTimeAsync(25);await flush()
    expect(document.querySelectorAll('.sbe-frame').length).toBeLessThanOrEqual(48)
    expect(document.querySelector('button[aria-label="100000000번 프레임 보기"]')).not.toBeNull()
    expect(pane.scrollTop).toBe(600)
})

it.each([true,false])('edits crops in the top surface with or without the original file available (%s)',async(withFile)=>{
    const api=await setup(draft(),withFile?{id:1,title:'합성 영상',duration:1,src:'about:blank'}:undefined)
    const surface=document.querySelector<HTMLElement>('.sbe-source-surface')!
    const crop=document.querySelector<HTMLElement>('[aria-label="크롭 편집"]')!
    expect(surface.hidden).toBe(false);expect(crop.hidden).toBe(false)
    expect(surface.contains(document.querySelector('.sbe-stage'))).toBe(true)
    expect(document.querySelector('.sbe-workspace .sbe-stage')).toBeNull()
    expect(document.querySelector<HTMLElement>('.sbe-crop-placeholder')!.hidden).toBe(true)
    expect(document.querySelector<HTMLElement>('.sbe-source-actions')!.hidden).toBe(true)
    if(withFile)expect(document.querySelector<HTMLVideoElement>('.sbe-source')!.hidden).toBe(true)
    const aside=document.querySelector<HTMLElement>('.sbe-frame-aside')!,body=document.querySelector('.sbe-body')!
    expect(aside.hidden).toBe(false);expect(body.firstElementChild).toBe(aside)
    const width=[...document.querySelectorAll('label')].find(el=>el.firstChild?.textContent==='너비')!.querySelector('input')!
    width.value='120';width.dispatchEvent(new Event('change'))
    const format=[...document.querySelectorAll('select')].find(el=>el.querySelector('option[value="webp"]'))!
    format.value='webp';format.dispatchEvent(new Event('change'))
    expect(crop.hidden).toBe(false)
    button('모든 프레임에 크롭 적용').click();button('WebP 저장').click();await flush()
    expect(api.create.mock.calls[0][0]).toMatchObject({kind:'webp',crop:{width:120,height:120}})
})
it('preserves the original video while frames are preparing and in the trimming workspace',async()=>{
    const file={id:1,title:'합성 영상',duration:1,src:'about:blank'}
    await setup({...draft(),state:'preparing'},file)
    expect(document.querySelector<HTMLElement>('.sbe-crop-placeholder')!.hidden).toBe(true)
    expect(document.querySelector<HTMLElement>('.sbe-source')!.hidden).toBe(false)
    expect(document.querySelector<HTMLElement>('.sbe-frame-aside')!.hidden).toBe(true)
    instance!.destroy();document.body.replaceChildren()
    await setup({...draft(),options:{...draft().options,kind:'trim'}},file)
    expect(document.querySelector<HTMLElement>('.sbe-source')!.hidden).toBe(false)
    expect(document.querySelector<HTMLElement>('[aria-label="크롭 편집"]')!.hidden).toBe(true)
    expect(document.querySelector<HTMLElement>('.sbe-crop-placeholder')!.hidden).toBe(true)
    expect(document.querySelector<HTMLElement>('.sbe-frame-aside')!.hidden).toBe(true)
    expect(document.querySelector<HTMLElement>('.sbe-source-actions')!.hidden).toBe(false)
    expect(document.querySelector('.sbe-body')!.classList.contains('has-frames')).toBe(false)
})

async function setupQuickVideo() {
    const ready={...draft(),frameCount:0,options:{...draft().options,quickVideo:true,fullSource:true,fps:1000,end:120}}
    let prepared=false
    const api={list:vi.fn(async()=>ok({sessions:prepared?[ready]:[],jobs:[]})),prepare:vi.fn(async()=>{prepared=true;return ok(ready)}),create:vi.fn(async()=>ok({id:'quick-job'})),frames:vi.fn(),frameStatus:vi.fn(),selection:vi.fn()}
    const host=document.createElement('div');document.body.append(host)
    instance=mountMediaEditor(host,{api,file:{id:1,title:'긴 원본',kind:'video',duration:120,src:'/video.mp4'}})
    const video=document.querySelector<HTMLVideoElement>('.sbe-source')!
    Object.defineProperties(video,{duration:{value:120},videoWidth:{value:160},videoHeight:{value:120},readyState:{value:2}})
    video.dispatchEvent(new Event('loadedmetadata'));await flush()
    return {api,video}
}
it('starts a video interval edit without preparing, indexing, or requesting thumbnails',async()=>{
    const {api,video}=await setupQuickVideo()
    expect(button('GIF 저장').disabled).toBe(false)
    expect(document.querySelector<HTMLElement>('[aria-label="남길 구간"]')!.hidden).toBe(false)
    expect(document.querySelector<HTMLElement>('.sbe-frame-aside')!.hidden).toBe(true)
    const handle=document.querySelector<HTMLElement>('[aria-label="남길 구간 시작 손잡이"]')!
    handle.dispatchEvent(new KeyboardEvent('keydown',{key:'ArrowRight',bubbles:true}));await flush()
    expect(video.currentTime).toBe(.001)
    expect(api.prepare).not.toHaveBeenCalled();expect(api.frames).not.toHaveBeenCalled();expect(api.frameStatus).not.toHaveBeenCalled()
    button('GIF 저장').click();await flush()
    expect(api.prepare).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({quickVideo:true,fullSource:true}))
    expect(api.create).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({sessionId:readySessionID(),kind:'gif',trimStart:.001,trimEnd:120,rangeStart:0,rangeEnd:0,deleted:[]}))
    expect(api.selection).not.toHaveBeenCalled()
})
function readySessionID(){return draft().id}
it('switches quick editing between WebP and video trimming with no frame setup',async()=>{
    const {api}=await setupQuickVideo()
    const format=[...document.querySelectorAll('select')].find(el=>el.querySelector('option[value="trim"]'))!
    format.value='webp';format.dispatchEvent(new Event('change'))
    button('WebP 저장').click();await flush()
    expect(api.create.mock.calls[0][0]).toMatchObject({kind:'webp',trimStart:0,trimEnd:120})
    format.value='trim';format.dispatchEvent(new Event('change'))
    button('잘라낸 영상 저장').click();await flush()
    expect(api.create.mock.calls[1][0]).toMatchObject({kind:'trim',trimStart:0,trimEnd:120})
    expect(api.prepare).toHaveBeenCalledOnce();expect(api.frames).not.toHaveBeenCalled()
})
it('opens precise frame editing for only the selected source interval',async()=>{
    const {api,video}=await setupQuickVideo()
    video.currentTime=15;button('현재 위치를 시작으로').click()
    video.currentTime=18;button('현재 위치를 종료로').click()
    button('프레임 정밀 편집').click();await flush()
    expect(api.prepare).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({originalFrames:true,fullSource:false,start:15,end:18,kind:'gif'}))
})
it('uses the decoded original video for quick cropping without requesting frame pages',async()=>{
    const {api}=await setupQuickVideo()
    button('크롭 영역 조절').click();await flush()
    expect(document.querySelector<HTMLElement>('[aria-label="크롭 편집"]')!.hidden).toBe(false)
    const width=[...document.querySelectorAll('label')].find(el=>el.firstChild?.textContent==='너비')!.querySelector('input')!
    width.value='80';width.dispatchEvent(new Event('change'))
    button('크롭 적용').click();button('GIF 저장').click();await flush()
    expect(api.create.mock.calls[0][0]).toMatchObject({crop:{width:80,height:120}})
    expect(api.frames).not.toHaveBeenCalled();expect(api.frameStatus).not.toHaveBeenCalled()
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

const shortcutHelp = () => document.querySelector<HTMLElement>('[aria-label="편집 단축키 안내"]')!
const shortcutGroup = (name: string) => [...shortcutHelp().querySelectorAll<HTMLElement>('.sbe-shortcut-group')].find(group=>group.querySelector('h4')!.textContent===name)!
function key(target: Element, value: string, options: KeyboardEventInit = {}) {
    const event = new KeyboardEvent('keydown',{key:value,bubbles:true,cancelable:true,...options})
    target.dispatchEvent(event)
    return event
}
it('opens an accessible shortcut guide and closes it without closing the editor or losing scroll position',async()=>{
    await setup()
    const toggle=button('단축키 안내'), help=shortcutHelp(), dialog=document.querySelector<HTMLElement>('.sb-editor')!, pane=document.querySelector<HTMLElement>('.sbe-main-pane')!
    expect(help.hidden).toBe(true);expect(toggle.getAttribute('aria-expanded')).toBe('false')
    expect(toggle.getAttribute('aria-controls')).toBe(help.id)
    expect(toggle.querySelector('svg')!.getAttribute('aria-hidden')).toBe('true')
    pane.scrollTop=320;toggle.click()
    expect(help.hidden).toBe(false);expect(toggle.getAttribute('aria-expanded')).toBe('true')
    expect(document.activeElement).toBe(help);expect(pane.scrollTop).toBe(0)
    expect(shortcutGroup('프레임 편집').hidden).toBe(false)
    expect(help.textContent).toContain('Ctrl+Z / ⌘+Z');expect(help.textContent).toContain('Shift+클릭')
    expect(help.textContent).not.toMatch(/Ctrl\+\+|Space|시작점 지정|종료점 지정/)
    expect(key(help,'Escape').defaultPrevented).toBe(true)
    expect(help.hidden).toBe(true);expect(toggle.getAttribute('aria-expanded')).toBe('false')
    expect(document.activeElement).toBe(toggle);expect(pane.scrollTop).toBe(320)
    expect(document.querySelector('.sb-editor')).toBe(dialog)
    toggle.click();button('단축키 안내 닫기').click()
    expect(help.hidden).toBe(true);expect(document.activeElement).toBe(toggle)
    expect(key(toggle,'Escape').defaultPrevented).toBe(false)
})
it('shows only applicable shortcuts before preparation and in video trim mode',async()=>{
    const pending={...draft(),state:'preparing'}
    await setup(pending)
    button('단축키 안내').click()
    expect(shortcutGroup('프레임 편집').hidden).toBe(true)
    expect(shortcutGroup('구간 손잡이').hidden).toBe(true)
    expect(shortcutGroup('공통').hidden).toBe(false)
    expect(shortcutHelp().textContent).toContain('준비가 끝나면')
    instance!.destroy();document.body.replaceChildren()
    const trim={...draft(),options:{...draft().options,kind:'trim'}}
    await setup(trim,{id:1,title:'합성 영상',duration:1,src:'about:blank'})
    button('단축키 안내').click()
    expect(shortcutGroup('프레임 편집').hidden).toBe(true)
    expect(shortcutGroup('구간 손잡이').hidden).toBe(false)
    expect(shortcutHelp().textContent).toContain('동영상 자르기에서는')
    const handle=document.querySelector<HTMLButtonElement>('[aria-label="남길 구간 시작 손잡이"]')!
    expect(handle.title).toContain('Home / End')
    expect(handle.getAttribute('aria-keyshortcuts')).toContain('ArrowRight')
    const previous=Number(handle.getAttribute('aria-valuenow'))
    handle.focus();expect(key(handle,'ArrowRight').defaultPrevented).toBe(true)
    expect(Number(handle.getAttribute('aria-valuenow'))).toBe(previous+1)
})
it('documents existing controls and keeps undo and redo working outside the guide and input fields',async()=>{
    await setup()
    expect(button('이전 프레임').title).toContain('←')
    expect(button('다음 프레임').getAttribute('aria-keyshortcuts')).toBe('ArrowRight')
    expect(button('선택 프레임 삭제').title).toContain('Delete')
    expect(button('실행 취소').title).toContain('Ctrl+Z')
    expect(button('다시 실행').getAttribute('aria-keyshortcuts')).toContain('Control+Shift+z')
    const checkbox=document.querySelector<HTMLInputElement>('input[aria-label="3번 프레임 선택"]')!
    expect(checkbox.title).toContain('Shift+클릭')
    checkbox.click();button('선택 프레임 삭제').click()
    expect(document.body.textContent).toContain('10장 중 9장 남음')
    button('단축키 안내').click()
    key(shortcutHelp(),'z',{ctrlKey:true});key(shortcutHelp(),'Delete')
    expect(document.body.textContent).toContain('10장 중 9장 남음')
    key(shortcutHelp(),'Escape')
    const input=document.querySelector<HTMLInputElement>('.sbe-fields input[type="number"]')!
    input.focus();expect(key(input,'z',{ctrlKey:true}).defaultPrevented).toBe(false)
    expect(document.body.textContent).toContain('10장 중 9장 남음')
    const stage=document.querySelector<HTMLElement>('.sbe-stage')!
    stage.focus();expect(key(stage,'z',{ctrlKey:true}).defaultPrevented).toBe(true)
    expect(document.body.textContent).toContain('10장 중 10장 남음')
    key(stage,'Z',{ctrlKey:true,shiftKey:true})
    expect(document.body.textContent).toContain('10장 중 9장 남음')
    key(stage,'z',{metaKey:true})
    expect(document.body.textContent).toContain('10장 중 10장 남음')
})


const selectedFrameNumbers = () => [...document.querySelectorAll<HTMLInputElement>('.sbe-frame input:checked')].map(input => Number(input.closest<HTMLElement>('.sbe-frame')!.dataset.frame) + 1)
function clickFrame(number: number, modifiers: MouseEventInit = {}) {
 const row = document.querySelector<HTMLButtonElement>(`button[aria-label="${number}번 프레임 보기"]`)!
 row.dispatchEvent(new MouseEvent('click',{bubbles:true,cancelable:true,detail:1,...modifiers}))
}
function framePointer(type: string, target: EventTarget, y: number, options: PointerEventInit = {}) {
 target.dispatchEvent(new PointerEvent(type,{bubbles:true,cancelable:true,pointerId:1,pointerType:'mouse',button:0,clientX:30,clientY:y,...options}))
}
function frameGeometry() {
 const grid=document.querySelector<HTMLElement>('.sbe-frames')!
 vi.spyOn(grid,'getBoundingClientRect').mockReturnValue(new DOMRect(0,100,180,176))
 Object.defineProperties(grid,{clientHeight:{value:176,configurable:true},scrollHeight:{value:440,configurable:true}})
 return grid
}
it('selects frames from row clicks with Ctrl toggles and stable Shift ranges',async()=>{
 await setup(); clickFrame(2); clickFrame(5,{ctrlKey:true});expect(selectedFrameNumbers()).toEqual([2,5])
 clickFrame(8,{shiftKey:true});expect(selectedFrameNumbers()).toEqual([5,6,7,8])
 clickFrame(6,{shiftKey:true});expect(selectedFrameNumbers()).toEqual([5,6])
 clickFrame(3,{shiftKey:true,ctrlKey:true});expect(selectedFrameNumbers()).toEqual([2,3,4,5])
 clickFrame(3,{ctrlKey:true});expect(selectedFrameNumbers()).toEqual([2,4,5])
 expect(document.querySelector('.sbe-frame-selection')?.textContent).toContain('3개 선택')
})
it('deletes selected frames from a focused checkbox and supports keyboard undo and redo',async()=>{
 const api=await setup(),checkbox=document.querySelector<HTMLInputElement>('[aria-label="3번 프레임 선택"]')!
 checkbox.click();checkbox.focus();expect(key(checkbox,'Delete').defaultPrevented).toBe(true)
 expect(document.body.textContent).toContain('10장 중 9장 남음')
 const grid=document.querySelector<HTMLElement>('.sbe-frames')!;expect(document.activeElement).toBe(grid)
 key(grid,'z',{ctrlKey:true});expect(document.body.textContent).toContain('10장 중 10장 남음')
 key(grid,'Z',{ctrlKey:true,shiftKey:true});expect(document.body.textContent).toContain('10장 중 9장 남음')
 const input=document.querySelector<HTMLInputElement>('.sbe-fields input[type="number"]')!;input.focus();expect(key(input,'Delete').defaultPrevented).toBe(false)
 button('GIF 저장').click();await flush();expect(api.create.mock.calls[0][0]).toMatchObject({deleted:[2]})
})
it('selects a dragged frame range and adds ranges with Ctrl without letting the release click change it',async()=>{
 editorLayoutHarness();await setup();const grid=frameGeometry()
 const row=document.querySelector('[data-frame="1"] button')!
 framePointer('pointerdown',row,166);framePointer('pointermove',window,298);framePointer('pointerup',window,298)
 expect(selectedFrameNumbers()).toEqual([2,3,4,5])
 clickFrame(2);expect(selectedFrameNumbers()).toEqual([2,3,4,5])
 await new Promise(resolve=>setTimeout(resolve,0));clickFrame(9)
 framePointer('pointerdown',row,166,{ctrlKey:true});framePointer('pointermove',window,210);framePointer('pointerup',window,210)
 expect(selectedFrameNumbers()).toEqual([2,3,9]);expect(grid.scrollTop).toBe(0)
})
it('cancels frame dragging with Escape and restores the previous selection',async()=>{
 editorLayoutHarness();await setup();frameGeometry();clickFrame(9)
 const row=document.querySelector('[data-frame="1"] button')!
 framePointer('pointerdown',row,166);framePointer('pointermove',window,254)
 expect(selectedFrameNumbers()).toEqual([2,3,4])
 expect(key(document.querySelector('.sbe-frames')!,'Escape').defaultPrevented).toBe(true)
 expect(selectedFrameNumbers()).toEqual([9])
 framePointer('pointerup',window,254);expect(selectedFrameNumbers()).toEqual([9])
})
it('auto-scrolls a dragged frame range and keeps virtual rows bounded',async()=>{
 const layout=editorLayoutHarness();await setup({...draft(),frameCount:1000});const grid=frameGeometry()
 Object.defineProperty(grid,'scrollHeight',{value:44000,configurable:true})
 const row=document.querySelector('[data-frame="1"] button')!
 framePointer('pointerdown',row,166);framePointer('pointermove',window,273)
 await layout.flush();expect(grid.scrollTop).toBeGreaterThan(0)
 expect(grid.querySelectorAll('.sbe-frame').length).toBeLessThanOrEqual(48)
 framePointer('pointerup',window,273)
})
it('skips deleted frames in new ranges and preserves at least one retained frame',async()=>{
 await setup();document.querySelector<HTMLInputElement>('[aria-label="3번 프레임 선택"]')!.click();button('선택 프레임 삭제').click()
 clickFrame(2);clickFrame(5,{shiftKey:true});expect(selectedFrameNumbers()).toEqual([2,4,5])
 clickFrame(1);clickFrame(10,{shiftKey:true});key(document.querySelector('.sbe-frames')!,'Delete')
 expect(document.body.textContent).toContain('최소 한 프레임은 남겨야 합니다.')
 expect(document.body.textContent).toContain('10장 중 9장 남음')
})
