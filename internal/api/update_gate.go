package api

import "sync"

// UpdateGate fences work submission while the executable is being replaced.
// Enter is non-blocking so UI and mobile requests fail instead of hanging.
// A nil gate preserves the behaviour of independently constructed services.
type UpdateGate struct{ mu sync.RWMutex }

const UpdateBlocked = "업데이트를 적용하고 있습니다. 앱이 다시 시작된 뒤 시도하세요."

func (g *UpdateGate) Enter() (func(), bool) {
	if g == nil {
		return func() {}, true
	}
	if !g.mu.TryRLock() {
		return nil, false
	}
	return g.mu.RUnlock, true
}

func (g *UpdateGate) Freeze() bool { return g.mu.TryLock() }
func (g *UpdateGate) Thaw()        { g.mu.Unlock() }
