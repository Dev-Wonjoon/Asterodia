// A dialog is not an eligible requestFullscreen target. Keep modal content in
// the top layer while the document (or desktop host window) enters fullscreen.
export function createFullscreenController(doc, native, onExit, onError = () => {}) {
    let desired = false, active = false, disposed = false, owned = false, generation = 0;
    const changed = () => {
        if (active && !native && !doc.fullscreenElement) { active = desired = false; onExit(); }
    };
    const unsubscribe = native?.onExit?.(() => { if (active) { active = desired = owned = false; onExit(); } });
    doc.addEventListener('fullscreenchange', changed);
    async function enter() {
        desired = true; const own = ++generation;
        try {
            if (native) {
                const entered = await native.enter();
                if (disposed || !desired || own !== generation) { if (entered) await native.exit(); return; }
                owned = entered; active = true;
            } else if (doc.documentElement.requestFullscreen) {
                if (!doc.fullscreenElement) { owned = true; await doc.documentElement.requestFullscreen(); }
                if (disposed || !desired || own !== generation) { if (owned && doc.fullscreenElement === doc.documentElement) await doc.exitFullscreen(); return; }
                active = true;
            }
        } catch { owned = active = false; if (!disposed && desired) onError(); }
    }
    function exit() {
        desired = active = false; generation++;
        if (owned) { owned = false; const pending = native ? native.exit() : doc.fullscreenElement === doc.documentElement ? doc.exitFullscreen?.() : null; pending?.catch?.(onError); }
    }
    return { enter, exit, destroy() { disposed = true; exit(); unsubscribe?.(); doc.removeEventListener('fullscreenchange', changed); } };
}
