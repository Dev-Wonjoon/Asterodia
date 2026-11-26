// Linked panes share one WebGL context and one upload of each decoded frame.
// Pane geometry is measured on resize/zoom, never inside the frame callback.
const renderers = new WeakMap();

function createRenderer(video, stage) {
    const canvas = stage.ownerDocument.createElement('canvas');
    let gl;
    try { gl = canvas.getContext('webgl', { alpha: true, antialias: false, depth: false, stencil: false, premultipliedAlpha: false }); } catch {}
    if (!gl || typeof gl.createShader !== 'function') return null;
    canvas.className = 'sbv-shared-canvas'; canvas.setAttribute('aria-hidden', 'true');
    const targets = new Set();
    let program, buffer, texture, rectLocation, resolutionLocation;
    let active = false, destroyed = false, dirty = true, lastSource = null, lastTime = -1, textureWidth = 0, textureHeight = 0;
    let width = 0, height = 0, ratio = 1, uploads = 0, draws = 0, maxTextureSize = 0;
    const shaders = [];
    function release() {
        if (texture) gl.deleteTexture(texture);
        if (buffer) gl.deleteBuffer(buffer);
        if (program) gl.deleteProgram(program);
        for (const shader of shaders) gl.deleteShader(shader);
        texture = buffer = program = null; shaders.length = 0;
    }
    function initialize() {
        const shader = (type, source) => {
            const value = gl.createShader(type); if (!value) throw new Error('shader allocation');
            shaders.push(value); gl.shaderSource(value, source); gl.compileShader(value);
            if (!gl.getShaderParameter(value, gl.COMPILE_STATUS)) throw new Error('shader compilation');
            return value;
        };
        program = gl.createProgram(); if (!program) throw new Error('program allocation');
        gl.attachShader(program, shader(gl.VERTEX_SHADER, `
            attribute vec2 position; uniform vec4 rect; uniform vec2 resolution; varying vec2 uv;
            void main() { vec2 pixel = rect.xy + position * rect.zw;
                gl_Position = vec4(pixel.x / resolution.x * 2.0 - 1.0, 1.0 - pixel.y / resolution.y * 2.0, 0, 1); uv = position; }`));
        gl.attachShader(program, shader(gl.FRAGMENT_SHADER, `
            precision mediump float; uniform sampler2D frame; varying vec2 uv;
            void main() { gl_FragColor = texture2D(frame, uv); }`));
        gl.linkProgram(program); if (!gl.getProgramParameter(program, gl.LINK_STATUS)) throw new Error('program linking');
        gl.useProgram(program);
        buffer = gl.createBuffer(); texture = gl.createTexture();
        if (!buffer || !texture) throw new Error('resource allocation');
        gl.bindBuffer(gl.ARRAY_BUFFER, buffer);
        gl.bufferData(gl.ARRAY_BUFFER, new Float32Array([0, 0, 1, 0, 0, 1, 0, 1, 1, 0, 1, 1]), gl.STATIC_DRAW);
        const position = gl.getAttribLocation(program, 'position');
        gl.enableVertexAttribArray(position); gl.vertexAttribPointer(position, 2, gl.FLOAT, false, 0, 0);
        rectLocation = gl.getUniformLocation(program, 'rect'); resolutionLocation = gl.getUniformLocation(program, 'resolution');
        gl.activeTexture(gl.TEXTURE0); gl.bindTexture(gl.TEXTURE_2D, texture);
        gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR); gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR);
        gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE); gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
        gl.uniform1i(gl.getUniformLocation(program, 'frame'), 0);
        maxTextureSize = gl.getParameter(gl.MAX_TEXTURE_SIZE);
        textureWidth = textureHeight = 0; lastSource = null; lastTime = -1; dirty = true;
        active = true; stage.classList.add('has-shared-video');
    }
    try { initialize(); } catch { release(); return null; }
    stage.append(canvas);
    function fallback() { active = false; stage.classList.remove('has-shared-video'); }
    function paint(frame) {
        if (!active || destroyed || !width || !height || gl.isContextLost()) return false;
        const time = frame === video ? video.currentTime : -1;
        const changed = frame !== lastSource || time !== lastTime;
        if (!changed && !dirty) return true;
        try {
            if (frame && changed) {
                const w = frame.videoWidth || frame.naturalWidth, h = frame.videoHeight || frame.naturalHeight;
                if (!w || !h) return false;
                if (w > maxTextureSize || h > maxTextureSize) { fallback(); return false; }
                gl.bindTexture(gl.TEXTURE_2D, texture);
                if (textureWidth !== w || textureHeight !== h) {
                    gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, gl.RGBA, gl.UNSIGNED_BYTE, frame); textureWidth = w; textureHeight = h;
                } else gl.texSubImage2D(gl.TEXTURE_2D, 0, 0, 0, gl.RGBA, gl.UNSIGNED_BYTE, frame);
                uploads++;
            }
            gl.viewport(0, 0, canvas.width, canvas.height); gl.disable(gl.SCISSOR_TEST);
            gl.clearColor(0, 0, 0, 0); gl.clear(gl.COLOR_BUFFER_BIT);
            gl.uniform2f(resolutionLocation, width, height); gl.enable(gl.SCISSOR_TEST);
            for (const target of targets) {
                const box = target.box, g = target.geometry; if (!box || !g) continue;
                gl.scissor(Math.round(box.x * ratio), Math.round((height - box.y - box.h) * ratio), Math.round(box.w * ratio), Math.round(box.h * ratio));
                gl.clearColor(0, 0, 0, 1); gl.clear(gl.COLOR_BUFFER_BIT);
                if (frame) { gl.uniform4f(rectLocation, box.x + g.left, box.y + g.top, g.width, g.height); gl.drawArrays(gl.TRIANGLES, 0, 6); draws++; }
            }
            gl.disable(gl.SCISSOR_TEST); lastSource = frame; lastTime = time; dirty = false;
            return true;
        } catch { fallback(); return false; }
    }
    const lost = event => { event.preventDefault(); fallback(); for (const target of targets) target.repaint(); };
    const restored = () => {
        release();
        try { initialize(); } catch { fallback(); }
        for (const target of targets) target.repaint();
    };
    canvas.addEventListener('webglcontextlost', lost); canvas.addEventListener('webglcontextrestored', restored);
    return {
        stage,
        attach(host, repaint) {
            const target = { geometry: null, box: null, repaint }; targets.add(target); dirty = true;
            return {
                paint,
                update(geometry, w, h) {
                    const parent = stage.getBoundingClientRect(), bounds = host.getBoundingClientRect();
                    width = stage.clientWidth; height = stage.clientHeight; ratio = Math.min(2, globalThis.devicePixelRatio || 1);
                    const cw = Math.max(1, Math.round(width * ratio)), ch = Math.max(1, Math.round(height * ratio));
                    if (canvas.width !== cw) canvas.width = cw;
                    if (canvas.height !== ch) canvas.height = ch;
                    target.geometry = geometry;
                    target.box = geometry && w > 0 && h > 0 ? { x: bounds.left - parent.left, y: bounds.top - parent.top, w, h } : null;
                    dirty = true;
                },
                getPerformance: () => ({ backend: active ? 'webgl' : 'canvas2d', uploads, draws, sharedSurfaces: targets.size }),
                destroy() {
                    targets.delete(target); dirty = true;
                    if (targets.size) { paint(lastSource); return; }
                    destroyed = true; fallback(); release(); canvas.remove();
                    canvas.removeEventListener('webglcontextlost', lost); canvas.removeEventListener('webglcontextrestored', restored);
                    renderers.delete(video);
                },
            };
        },
    };
}

export function attachSharedVideo(video, host, repaint) {
    const stage = host.closest('.sbv-stage'); if (!stage) return null;
    let renderer = renderers.get(video);
    if (!renderer) { renderer = createRenderer(video, stage); if (renderer) renderers.set(video, renderer); }
    return renderer?.stage === stage ? renderer.attach(host, repaint) : null;
}
