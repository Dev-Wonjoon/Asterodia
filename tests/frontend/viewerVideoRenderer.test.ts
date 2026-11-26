// @vitest-environment happy-dom
import { afterEach, expect, it, vi } from 'vitest'
import { attachSharedVideo } from '../../internal/webserver/web/viewer-video-renderer.js'

afterEach(() => { document.body.replaceChildren(); vi.restoreAllMocks() })
function fixture() {
    const gl: any = {}
    for (const name of ['shaderSource', 'compileShader', 'attachShader', 'linkProgram', 'useProgram', 'bindBuffer', 'bufferData',
        'enableVertexAttribArray', 'vertexAttribPointer', 'activeTexture', 'bindTexture', 'texParameteri', 'uniform1i', 'viewport',
        'disable', 'clearColor', 'clear', 'uniform2f', 'enable', 'scissor', 'uniform4f', 'drawArrays', 'texImage2D', 'texSubImage2D',
        'deleteTexture', 'deleteBuffer', 'deleteProgram', 'deleteShader']) gl[name] = vi.fn()
    for (const name of ['createShader', 'createProgram', 'createTexture', 'createBuffer', 'getUniformLocation']) gl[name] = vi.fn(() => ({}))
    gl.getShaderParameter = gl.getProgramParameter = vi.fn(() => true)
    gl.getAttribLocation = vi.fn(() => 0); gl.getParameter = vi.fn(() => 8192); gl.isContextLost = vi.fn(() => false)
    vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockImplementation(type => type === 'webgl' ? gl : null)
    const stage = document.createElement('div'); stage.className = 'sbv-stage'; document.body.append(stage)
    Object.defineProperties(stage, { clientWidth: { value: 1600 }, clientHeight: { value: 450 } })
    vi.spyOn(stage, 'getBoundingClientRect').mockReturnValue({ left: 0, top: 0 } as DOMRect)
    const hosts = [0, 800].map(left => {
        const host = document.createElement('div'); stage.append(host)
        vi.spyOn(host, 'getBoundingClientRect').mockReturnValue({ left, top: 0 } as DOMRect); return host
    })
    const video = document.createElement('video'); Object.defineProperties(video, { videoWidth: { value: 3840 }, videoHeight: { value: 2160 } })
    return { gl, stage, hosts, video }
}

it('uploads a linked frame once, draws each crop, and releases GPU resources only after the last view', () => {
    const { gl, stage, hosts, video } = fixture()
    const first = attachSharedVideo(video, hosts[0], vi.fn())!, second = attachSharedVideo(video, hosts[1], vi.fn())!
    first.update({ left: 0, top: 0, width: 800, height: 450 }, 800, 450)
    second.update({ left: -400, top: -225, width: 1600, height: 900 }, 800, 450)
    expect(first.paint(video)).toBe(true); expect(second.paint(video)).toBe(true)
    expect(stage.querySelectorAll('.sbv-shared-canvas')).toHaveLength(1)
    expect(gl.texImage2D).toHaveBeenCalledOnce(); expect(gl.drawArrays).toHaveBeenCalledTimes(2)
    expect(gl.uniform4f.mock.calls.map((args: any[]) => args.slice(1))).toEqual([[0, 0, 800, 450], [400, -225, 1600, 900]])
    video.currentTime = 1 / 60; first.paint(video); second.paint(video)
    expect(gl.texSubImage2D).toHaveBeenCalledOnce(); expect(first.getPerformance()).toMatchObject({ backend: 'webgl', uploads: 2, draws: 4 })
    first.destroy(); expect(gl.deleteTexture).not.toHaveBeenCalled()
    second.destroy(); expect(gl.deleteTexture).toHaveBeenCalledOnce(); expect(stage.querySelector('canvas')).toBeNull()
    expect(stage.classList.contains('has-shared-video')).toBe(false)
})

it('falls back on context loss and recreates textures before resuming after restoration', () => {
    const { gl, stage, hosts, video } = fixture(), repaint = vi.fn()
    const view = attachSharedVideo(video, hosts[0], repaint)!
    view.update({ left: 0, top: 0, width: 800, height: 450 }, 800, 450); view.paint(video)
    const canvas = stage.querySelector('canvas')!, lost = new Event('webglcontextlost', { cancelable: true })
    canvas.dispatchEvent(lost); expect(lost.defaultPrevented).toBe(true)
    expect(view.paint(video)).toBe(false); expect(view.getPerformance().backend).toBe('canvas2d'); expect(repaint).toHaveBeenCalledOnce()
    canvas.dispatchEvent(new Event('webglcontextrestored')); expect(view.paint(video)).toBe(true)
    expect(gl.texImage2D).toHaveBeenCalledTimes(2); expect(repaint).toHaveBeenCalledTimes(2)
    view.destroy()
})

it('keeps Canvas fallback available when shader setup or WebGL creation fails', () => {
    const { gl, stage, hosts, video } = fixture(); gl.getShaderParameter.mockReturnValue(false)
    expect(attachSharedVideo(video, hosts[0], vi.fn())).toBeNull()
    expect(gl.deleteProgram).toHaveBeenCalledOnce(); expect(stage.querySelector('canvas')).toBeNull()
    vi.mocked(HTMLCanvasElement.prototype.getContext).mockReturnValue(null)
    expect(attachSharedVideo(video, hosts[0], vi.fn())).toBeNull()
})
