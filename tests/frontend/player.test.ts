// @vitest-environment happy-dom
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { mountVideoPlayer, readSeekSeconds } from '../../internal/webserver/web/player.js'
let player: ReturnType<typeof mountVideoPlayer> | undefined
beforeEach(() => {
    localStorage.clear(); vi.useFakeTimers()
    vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => {})
    vi.spyOn(HTMLMediaElement.prototype, 'load').mockImplementation(() => {})
    vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue(undefined)
})
afterEach(() => { player?.destroy(); player=undefined; document.body.replaceChildren(); vi.useRealTimers(); vi.restoreAllMocks() })
function setup() {
    player=mountVideoPlayer(document.body,{src:'/fixture.mp4',title:'영상'})
    const video=player.video
    Object.defineProperty(video,'duration',{value:1000,configurable:true})
    vi.spyOn(video,'getBoundingClientRect').mockReturnValue({left:0,width:800} as DOMRect)
    video.dispatchEvent(new Event('loadedmetadata'))
    return video
}
const button=(label:string)=>document.querySelector<HTMLButtonElement>(`button[aria-label="${label}"]`)!
it('keeps all eight jumps outside the video screen and clamps both ends',()=>{
    const video=setup()
    expect([...document.querySelectorAll('.sbp-jumps button')].map(el=>el.textContent)).toEqual(['-5분','-1분','-30초','-10초','+10초','+30초','+1분','+5분'])
    expect(document.querySelector('.sbp-screen .sbp-jumps')).toBeNull()
    video.currentTime=900;button('+5분 이동').click();expect(video.currentTime).toBe(1000)
    video.currentTime=5;button('-10초 이동').click();expect(video.currentTime).toBe(0)
    expect(video.controls).toBe(false)
})
it('double-clicks seek without also toggling playback, persists a configurable interval',async()=>{
    const video=setup();video.currentTime=200
    video.dispatchEvent(new MouseEvent('click',{clientX:20,detail:1}))
    video.dispatchEvent(new MouseEvent('click',{clientX:20,detail:2}))
    video.dispatchEvent(new MouseEvent('dblclick',{clientX:20,detail:2,cancelable:true}))
    await vi.advanceTimersByTimeAsync(600)
    expect(video.currentTime).toBe(190);expect(video.play).not.toHaveBeenCalled()
    button('플레이어 설정').click()
    const input=document.querySelector<HTMLInputElement>('.sbp-settings input')!;input.value='30';input.dispatchEvent(new Event('change'))
    expect(readSeekSeconds()).toBe(30)
    video.dispatchEvent(new MouseEvent('dblclick',{clientX:700}));expect(video.currentTime).toBe(220)
    input.value='0';input.dispatchEvent(new Event('change'));expect(input.value).toBe('30')
})
it('recognizes double tap once and ignores synthetic mouse events and swipes',async()=>{
    const video=setup();video.currentTime=100
    function tap(x:number) {
        video.dispatchEvent(new PointerEvent('pointerdown',{pointerType:'touch',pointerId:1,button:0,clientX:x,clientY:20}))
        video.dispatchEvent(new PointerEvent('pointerup',{pointerType:'touch',pointerId:1,clientX:x,clientY:20}))
    }
    tap(700);await vi.advanceTimersByTimeAsync(120);tap(700)
    video.dispatchEvent(new MouseEvent('click',{clientX:700,detail:1}))
    video.dispatchEvent(new MouseEvent('dblclick',{clientX:700,detail:2}))
    await vi.advanceTimersByTimeAsync(600)
    expect(video.currentTime).toBe(110);expect(video.play).not.toHaveBeenCalled()
    video.dispatchEvent(new PointerEvent('pointerdown',{pointerType:'touch',pointerId:2,clientX:600,clientY:20}))
    video.dispatchEvent(new PointerEvent('pointerup',{pointerType:'touch',pointerId:2,clientX:690,clientY:20}))
    await vi.advanceTimersByTimeAsync(600);expect(video.play).not.toHaveBeenCalled()
})
it('plays on a single click, handles keyboard seeks, and releases pending gestures on destroy',async()=>{
    const video=setup();video.currentTime=50
    video.dispatchEvent(new MouseEvent('click',{detail:1}));await vi.advanceTimersByTimeAsync(600);expect(video.play).toHaveBeenCalledOnce()
    player!.root.dispatchEvent(new KeyboardEvent('keydown',{key:'ArrowLeft',bubbles:true,cancelable:true}));expect(video.currentTime).toBe(40)
    video.dispatchEvent(new MouseEvent('click',{detail:1}));player!.destroy();player=undefined
    await vi.advanceTimersByTimeAsync(600);expect(video.play).toHaveBeenCalledOnce()
})
it('expands the entire player with its jump buttons when native fullscreen is unavailable',async()=>{
    setup()
    player!.root.requestFullscreen=vi.fn().mockRejectedValue(new Error('denied'))
    button('전체화면').click();await Promise.resolve();await Promise.resolve()
    expect(document.querySelector('dialog[open] .sbp-jumps')).not.toBeNull()
    expect(player!.root.classList.contains('is-expanded')).toBe(true)
    button('전체화면 종료').click();await vi.advanceTimersByTimeAsync(0)
    expect(document.querySelector('.sbp-fullscreen-dialog')).toBeNull()
    expect(player!.root.parentElement).toBe(document.body)
})


const audioKey = 'storagebucket:player-audio:v1'
function changeVolume(value: string) {
 const volume=document.querySelector<HTMLInputElement>('.sbp-volume')!
 volume.value=value;volume.dispatchEvent(new Event('input'))
}
it('persists volume immediately and restores it for a newly created player',()=>{
 setup();changeVolume('0.35')
 expect(JSON.parse(localStorage.getItem(audioKey)!)).toEqual({volume:.35,muted:false})
 player!.destroy();player=undefined
 const video=setup();expect(video.volume).toBe(.35);expect(video.muted).toBe(false)
 expect(document.querySelector<HTMLInputElement>('.sbp-volume')!.value).toBe('0.35')
})
it('remembers mute separately from volume and restores the level when unmuted',()=>{
 setup();changeVolume('0.4');button('음소거').click()
 expect(JSON.parse(localStorage.getItem(audioKey)!)).toEqual({volume:.4,muted:true})
 player!.destroy();player=undefined
 const video=setup();expect(video.volume).toBe(.4);expect(video.muted).toBe(true)
 expect(document.querySelector<HTMLInputElement>('.sbp-volume')!.value).toBe('0')
 button('음소거 해제').click();expect(video.volume).toBe(.4);expect(video.muted).toBe(false)
 expect(JSON.parse(localStorage.getItem(audioKey)!)).toEqual({volume:.4,muted:false})
 player!.root.dispatchEvent(new KeyboardEvent('keydown',{key:'m',bubbles:true}))
 expect(JSON.parse(localStorage.getItem(audioKey)!)).toEqual({volume:.4,muted:true})
})
it('restores zero volume and records native volume changes without saving on destroy',()=>{
 localStorage.setItem(audioKey,JSON.stringify({volume:0,muted:true}))
 const video=setup();expect(video.volume).toBe(0);expect(video.muted).toBe(true)
 video.volume=.6;video.muted=false;video.dispatchEvent(new Event('volumechange'))
 expect(JSON.parse(localStorage.getItem(audioKey)!)).toEqual({volume:.6,muted:false})
 player!.destroy();player=undefined
 localStorage.setItem(audioKey,JSON.stringify({volume:.2,muted:false}))
 video.volume=.9;video.dispatchEvent(new Event('volumechange'))
 expect(JSON.parse(localStorage.getItem(audioKey)!)).toEqual({volume:.2,muted:false})
})
it.each(['broken','null','[]','{"volume":2,"muted":false}','{"volume":-1,"muted":false}','{"volume":"0.3","muted":false}','{"volume":0.3,"muted":"false"}'])('ignores an invalid saved audio preference: %s',raw=>{
 localStorage.setItem(audioKey,raw)
 const video=setup();expect(video.volume).toBe(1);expect(video.muted).toBe(false)
})
it('keeps volume controls usable when preference storage is unavailable',()=>{
 vi.spyOn(Storage.prototype,'getItem').mockImplementation(()=>{throw new Error('blocked')})
 vi.spyOn(Storage.prototype,'setItem').mockImplementation(()=>{throw new Error('full')})
 const video=setup();expect(video.volume).toBe(1)
 expect(()=>changeVolume('0.25')).not.toThrow();expect(video.volume).toBe(.25)
 expect(()=>button('음소거').click()).not.toThrow();expect(video.muted).toBe(true)
})

it('plays audio with artwork and the same seek, speed, volume and keyboard controls', async () => {
 player=mountVideoPlayer(document.body,{src:'/song.mp3',kind:'audio',title:'노래',poster:'/cover.jpg'})
 const audio=document.querySelector('audio')!
 expect(document.querySelector('video')).toBeNull();expect(audio.controls).toBe(false)
 expect(document.querySelector('.sbp-artwork img')?.getAttribute('src')).toBe('/cover.jpg')
 Object.defineProperty(audio,'duration',{value:180,configurable:true});audio.dispatchEvent(new Event('loadedmetadata'))
 button('재생').click();expect(audio.play).toHaveBeenCalledOnce()
 audio.currentTime=45;button('+10초 이동').click();expect(audio.currentTime).toBe(55)
 const rate=document.querySelector<HTMLSelectElement>('.sbp-rate')!;rate.value='1.5';rate.dispatchEvent(new Event('change'));expect(audio.playbackRate).toBe(1.5)
 changeVolume('0.35');expect(audio.volume).toBe(.35)
 player.root.dispatchEvent(new KeyboardEvent('keydown',{key:'j',bubbles:true}));expect(audio.currentTime).toBe(50)
 document.querySelector('img')!.dispatchEvent(new Event('error'));expect(document.querySelector('.sbp-artwork')!.textContent).toBe('♫')
 const surface=document.querySelector('.sbp-artwork')!;surface.dispatchEvent(new MouseEvent('click',{detail:1}));await vi.advanceTimersByTimeAsync(600)
 expect(audio.play).toHaveBeenCalledTimes(2)
 player.destroy();player=undefined;expect(audio.getAttribute('src')).toBeNull()
})
