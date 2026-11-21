package mediaedit

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/Dev-Wonjoon/StorageBucket/internal/api"
	"github.com/Dev-Wonjoon/StorageBucket/internal/engine"
)

type Usage struct {
	Known             bool   `json:"known"`
	CurrentBytes      uint64 `json:"currentBytes"`
	ObservedPeakBytes uint64 `json:"observedPeakBytes"`
	SampledAt         int64  `json:"sampledAt"`
	OutputBytes       int64  `json:"outputBytes"`
	OutputFrames      int    `json:"outputFrames"`
}
type ResourceEstimate struct {
	MemoryKnown       bool   `json:"memoryKnown"`
	AvailableRAM      uint64 `json:"availableRam"`
	TotalRAM          uint64 `json:"totalRam"`
	EstimateKnown     bool   `json:"estimateKnown"`
	LowRAM            uint64 `json:"lowRam"`
	HighRAM           uint64 `json:"highRam"`
	Basis             string `json:"basis"`
	Note              string `json:"note"`
	Pressure          bool   `json:"pressure"`
	DiskKnown         bool   `json:"diskKnown"`
	DiskAvailable     uint64 `json:"diskAvailable"`
	DiskEstimateKnown bool   `json:"diskEstimateKnown"`
	DiskEstimate      uint64 `json:"diskEstimate"`
	Live              Usage  `json:"live"`
}
type usageTracker struct {
	mu        sync.Mutex
	processes map[uint32]engine.ProcessMemory
	value     Usage
}

func (u *usageTracker) observe(sample engine.ProcessMemory) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if sample.Active {
		u.processes[sample.PID] = sample
	} else {
		delete(u.processes, sample.PID)
	}
	var current uint64
	for _, v := range u.processes {
		if v.Known {
			current += v.WorkingSetBytes
			u.value.Known = true
		}
	}
	u.value.Known = u.value.Known || sample.Known
	u.value.CurrentBytes = current
	u.value.ObservedPeakBytes = max(u.value.ObservedPeakBytes, current, sample.PeakWorkingSetBytes)
	u.value.SampledAt = now()
}
func (u *usageTracker) snapshot() Usage { u.mu.Lock(); defer u.mu.Unlock(); return u.value }
func (s *Service) liveUsage(id string, fallback Usage) Usage {
	s.usageMu.Lock()
	tracker := s.usage[id]
	s.usageMu.Unlock()
	if tracker != nil {
		return tracker.snapshot()
	}
	return fallback
}

func (s *Service) noteOutput(id string, bytes int64, frames int) {
	s.usageMu.Lock()
	tracker := s.usage[id]
	s.usageMu.Unlock()
	if tracker != nil {
		tracker.mu.Lock()
		tracker.value.OutputBytes = bytes
		tracker.value.OutputFrames = frames
		tracker.mu.Unlock()
	}
}
func (s *Service) trackUsage(ctx context.Context, id string, persist bool) (context.Context, func()) {
	u := &usageTracker{processes: map[uint32]engine.ProcessMemory{}}
	s.usageMu.Lock()
	if s.usage == nil {
		s.usage = map[string]*usageTracker{}
	}
	s.usage[id] = u
	s.usageMu.Unlock()
	done := make(chan struct{})
	finished := make(chan struct{})
	write := func() {
		if persist {
			_, _ = s.store.db.Exec("UPDATE media_edit_job SET resources_json=? WHERE id=?", raw(u.snapshot()), id)
		}
	}
	go func() {
		defer close(finished)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				write()
			}
		}
	}()
	return engine.WithMemoryObserver(ctx, u.observe), func() {
		close(done)
		<-finished
		write()
		s.usageMu.Lock()
		delete(s.usage, id)
		s.usageMu.Unlock()
	}
}

func estimateBase(width, height, cropW, cropH int, format string, frames int) (uint64, uint64, bool) {
	pixels := float64(width) * float64(height)
	crop := float64(cropW) * float64(cropH)
	// Decoder reference frames, two processes, filters, and palettes. These are
	// a planning range, not an allocation limit or a promised maximum.
	low := 96*1024*1024 + pixels*8 + crop*8
	high := 256*1024*1024 + pixels*24 + crop*24
	if format == "webp" {
		low += crop * float64(frames) * 0.005
		high += crop * float64(frames) * 0.12
	}
	if math.IsNaN(high) || math.IsInf(high, 0) || high > float64(^uint64(0)) {
		return 0, 0, false
	}
	return uint64(low), uint64(high), true
}

func (s *Service) Resources(ctx context.Context, req CreateRequest) api.Result {
	if !validID(req.SessionID) {
		return invalid("편집 자료 ID를 확인하세요")
	}
	v, e := s.store.session(ctx, req.SessionID)
	if e != nil {
		return fail(e)
	}
	if req.Kind == "" {
		req.Kind = "gif"
	}
	if req.Kind != "gif" && req.Kind != "webp" && req.Kind != "trim" {
		return invalid("출력 형식을 확인하세요")
	}
	if req.Crop.Width == 0 {
		req.Crop = engine.EditCrop{Width: v.Width, Height: v.Height}
	}
	if req.Crop.X < 0 || req.Crop.Y < 0 || req.Crop.Width < 1 || req.Crop.Height < 1 || req.Crop.Width > v.Width || req.Crop.Height > v.Height || req.Crop.X > v.Width-req.Crop.Width || req.Crop.Y > v.Height-req.Crop.Height {
		return invalid("크롭 영역을 확인하세요")
	}
	available, total, disk, memoryKnown, diskKnown := systemResources(s.output())
	r := ResourceEstimate{MemoryKnown: memoryKnown, AvailableRAM: available, TotalRAM: total, DiskKnown: diskKnown, DiskAvailable: disk, Basis: "calculated", Note: "버퍼 계산에 따른 참고 범위입니다. 전체 작업의 최대 사용량을 보장하지 않습니다."}
	r.LowRAM, r.HighRAM, r.EstimateKnown = estimateBase(v.Width, v.Height, req.Crop.Width, req.Crop.Height, req.Kind, v.FrameCount)
	if v.Options.NativeGIF && r.EstimateKnown {
		factor := 128.0
		low := float64(r.LowRAM) + float64(v.FrameCount)*24
		high := float64(r.HighRAM) + float64(v.FrameCount)*factor
		if high >= float64(^uint64(0)) {
			r.EstimateKnown = false
		} else {
			r.LowRAM = uint64(low)
			r.HighRAM = uint64(high)
		}
	}
	ids, e := s.store.ids(ctx, "media_edit_job", "WHERE session_id=? AND preview=1 AND state='completed' ORDER BY created_at DESC LIMIT 20", v.ID)
	if e != nil {
		return fail(e)
	}
	q, _ := engine.NormalizeAnimationQuality(req.Kind, req.Quality)
	for _, id := range ids {
		j, e := s.store.job(ctx, id)
		if e != nil {
			continue
		}
		if j.Options.Kind != req.Kind || j.Options.Crop != req.Crop || j.Options.Quality != q {
			continue
		}
		u := j.Resources
		if u.Known {
			r.LowRAM = max(r.LowRAM, u.ObservedPeakBytes*8/10)
			r.HighRAM = max(r.HighRAM, u.ObservedPeakBytes*18/10)
			r.Basis = "sampled"
			r.Note = "최대 2초 미리보기의 실측을 반영했습니다. 긴 작업의 후반 최대값은 달라질 수 있습니다."
		}
		if u.OutputFrames > 0 && u.OutputBytes > 0 {
			estimate := float64(u.OutputBytes) * float64(v.FrameCount) / float64(u.OutputFrames) * 2.5
			if estimate < float64(^uint64(0)) {
				r.DiskEstimateKnown = true
				r.DiskEstimate = uint64(estimate)
			}
		}
		break
	}
	r.Pressure = r.MemoryKnown && r.EstimateKnown && r.HighRAM > r.AvailableRAM
	s.usageMu.Lock()
	for _, u := range s.usage {
		live := u.snapshot()
		r.Live.Known = r.Live.Known || live.Known
		r.Live.CurrentBytes += live.CurrentBytes
		r.Live.ObservedPeakBytes = max(r.Live.ObservedPeakBytes, live.ObservedPeakBytes)
		r.Live.SampledAt = max(r.Live.SampledAt, live.SampledAt)
	}
	s.usageMu.Unlock()
	return api.Success(r)
}
