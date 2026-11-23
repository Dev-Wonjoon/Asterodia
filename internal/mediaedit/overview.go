package mediaedit

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Dev-Wonjoon/StorageBucket/internal/api"
	"github.com/Dev-Wonjoon/StorageBucket/internal/engine"
	"github.com/Dev-Wonjoon/StorageBucket/internal/media"
)

const overviewCount = 12

type OverviewRequest struct {
	FileID int64 `json:"fileId"`
	Index  int   `json:"index"`
}

type OverviewFrame struct {
	Index      int     `json:"index"`
	Count      int     `json:"count"`
	Time       float64 `json:"time"`
	Duration   float64 `json:"duration"`
	FrameCount int     `json:"frameCount"`
	Image      string  `json:"image"`
}

type overviewCache struct {
	duration   float64
	frameCount int
	images     map[int]string
}

// Overview returns one small representative image. A separate, single decoder
// lets the editor appear immediately, and closing the service cancels its work.
func (s *Service) Overview(ctx context.Context, req OverviewRequest) api.Result {
	leave, allowed := s.UpdateGate.Enter()
	if !allowed {
		return api.Failure("UPDATE_RESTARTING", api.UpdateBlocked)
	}
	defer leave()
	if req.FileID <= 0 || req.Index < 0 || req.Index >= overviewCount {
		return invalid("미리보기 위치를 확인하세요")
	}
	s.mu.Lock()
	if s.closed || s.halted || s.overviewCtx == nil {
		s.mu.Unlock()
		return api.Failure("NOT_READY", "편집 서비스를 사용할 준비가 되지 않았습니다")
	}
	serviceCtx := s.overviewCtx
	s.overviewWG.Add(1)
	s.mu.Unlock()
	defer s.overviewWG.Done()
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	stop := context.AfterFunc(serviceCtx, cancel)
	defer stop()
	select {
	case s.overviewSlot <- struct{}{}:
		defer func() { <-s.overviewSlot }()
	case <-ctx.Done():
		return fail(ctx.Err())
	}
	if ctx.Err() != nil {
		return fail(ctx.Err())
	}
	content, err := media.NewService(s.store.db).OpenContent(ctx, req.FileID, "content")
	if err != nil {
		return fail(err)
	}
	defer content.File.Close()
	if content.Kind != "video" {
		return invalid("등록된 영상을 선택하세요")
	}
	identity, err := media.FileIdentity(content.File)
	if err != nil {
		return fail(err)
	}
	key := fmt.Sprintf("%d:%s:%d:%d", req.FileID, identity, content.Info.Size(), content.Info.ModTime().UnixNano())
	cached := s.overviews[key]
	if cached == nil {
		metadata, err := s.editor.ProbeVideo(ctx, content.File.Name())
		if err != nil {
			return fail(err)
		}
		cached = &overviewCache{duration: *metadata.Duration, images: make(map[int]string)}
		if metadata.FrameCount != nil {
			cached.frameCount = *metadata.FrameCount
		}
		if len(s.overviews) >= 8 {
			for old := range s.overviews {
				delete(s.overviews, old)
				break
			}
		}
		s.overviews[key] = cached
	}
	at := cached.duration * float64(req.Index) / overviewCount
	if cached.images[req.Index] == "" {
		dir, err := os.MkdirTemp(s.cache, "overview-")
		if err != nil {
			return fail(err)
		}
		defer os.RemoveAll(dir)
		path := filepath.Join(dir, "frame.jpg")
		if err = s.editor.OverviewFrame(ctx, content.File.Name(), path, at); err != nil {
			if errors.Is(err, engine.ErrStopUnconfirmed) {
				s.mu.Lock()
				s.halted = true
				s.mu.Unlock()
			}
			return fail(err)
		}
		current, err := os.Stat(content.File.Name())
		if err != nil || !os.SameFile(content.Info, current) || current.Size() != content.Info.Size() || !current.ModTime().Equal(content.Info.ModTime()) {
			delete(s.overviews, key)
			return invalid("원본 영상이 변경되었습니다. 다시 열어 주세요.")
		}
		info, err := os.Stat(path)
		if err != nil {
			return fail(err)
		}
		if info.Size() <= 0 || info.Size() > 256<<10 {
			return invalid("미리보기 이미지 크기를 확인하세요")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fail(err)
		}
		cached.images[req.Index] = "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(data)
	}
	return api.Success(OverviewFrame{Index: req.Index, Count: overviewCount, Time: at, Duration: cached.duration, FrameCount: cached.frameCount, Image: cached.images[req.Index]})
}
