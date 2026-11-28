package mediaedit

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/Dev-Wonjoon/Asterodia/internal/engine"
	"github.com/Dev-Wonjoon/Asterodia/internal/media"
)

func (s *Service) work(ctx context.Context) {
	defer close(s.done)
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.wake:
		case <-time.After(2 * time.Second):
		}
		s.mu.Lock()
		_ = s.pruneHistory(ctx, "")
		s.mu.Unlock()
		for ctx.Err() == nil {
			// Serve one display page, then let an older output job run as well.
			paged := s.runNextPage(ctx)
			s.mu.Lock()
			if s.halted {
				s.mu.Unlock()
				break
			}
			var id, table string
			err := s.store.db.QueryRowContext(ctx, `SELECT id,kind FROM (SELECT id,'media_edit_session' kind,created_at FROM media_edit_session WHERE state='pending' UNION ALL SELECT id,'media_edit_job' kind,created_at FROM media_edit_job WHERE state='pending') ORDER BY created_at,id LIMIT 1`).Scan(&id, &table)
			if err != nil {
				s.mu.Unlock()
				if paged {
					continue
				}
				break
			}
			runCtx, cancel := context.WithCancel(ctx)
			runCtx, finishUsage := s.trackUsage(runCtx, id, table == "media_edit_job")
			s.activeID = id
			s.activeCancel = cancel
			state := "running"
			if table == "media_edit_session" {
				state = "preparing"
			}
			err = s.store.state(table, id, state, "")
			s.mu.Unlock()
			if err == nil {
				if table == "media_edit_session" {
					err = s.prepare(runCtx, id)
				} else {
					err = s.render(runCtx, id)
				}
			}
			cancel()
			finishUsage()
			s.mu.Lock()
			if err != nil {
				var message string
				state, message = "failed", "미디어 처리에 실패했습니다. 원본 파일·엔진 설치·저장 공간을 확인하세요."
				if errors.Is(err, errGIFFrameTiming) {
					message = errGIFFrameTiming.Error()
				}
				if errors.Is(err, errBatchStart) {
					message = errBatchStart.Error()
				}
				if errors.Is(err, errBatchNoAudio) {
					message = errBatchNoAudio.Error()
				}
				if errors.Is(err, context.Canceled) {
					state, message = "cancelled", ""
				}
				if errors.Is(err, engine.ErrStopUnconfirmed) {
					s.halted = true
					state, message = "stop_unconfirmed", "엔진 종료를 확인하지 못했습니다. 앱을 다시 시작한 뒤 작업을 확인하세요."
				}
				if ctx.Err() != nil {
					state, message = "interrupted", "앱이 종료되어 중단됐습니다. 새 작업으로 다시 실행하세요."
				}
				// A published file retains its recovery record when DB commit fails.
				var current string
				if e := s.store.db.QueryRowContext(context.Background(), "SELECT state FROM "+table+" WHERE id=?", id).Scan(&current); e == nil && current == "publishing" {
					state, message = "interrupted", "파일 저장 확정을 마치지 못했습니다. 앱을 다시 시작하면 결과를 확인합니다."
				}
				_ = s.store.state(table, id, state, message)
				_, _ = s.store.db.ExecContext(context.Background(), "UPDATE "+table+" SET diagnostic=? WHERE id=?", err.Error(), id)
			}
			s.activeID = ""
			s.activeCancel = nil
			_ = s.pruneHistory(ctx, "")
			s.mu.Unlock()
		}
	}
}

func (s *Service) progress(table, id string, from, to int) func(int) {
	last := -1
	return func(p int) {
		value := from + (to-from)*p/100
		if value == last {
			return
		}
		last = value
		_, _ = s.store.db.ExecContext(context.Background(), "UPDATE "+table+" SET progress=?,updated_at=? WHERE id=?", value, now(), id)
	}
}

// The handle denies write/delete sharing for the entire FFmpeg operation.
func (s *Service) source(ctx context.Context, id string) (*os.File, string, error) {
	var path, identity, mtime string
	var size int64
	err := s.store.db.QueryRowContext(ctx, `SELECT source_path,source_identity,source_size,source_mtime FROM media_edit_session WHERE id=? AND source_file_id IS NOT NULL`, id).Scan(&path, &identity, &size, &mtime)
	if err != nil {
		return nil, "", err
	}
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, "", err
	}
	defer root.Close()
	f, err := media.OpenReadFile(root, filepath.Base(path))
	if err != nil {
		return nil, "", err
	}
	actual, err := media.FileIdentity(f)
	info, se := f.Stat()
	if err != nil || se != nil || actual != identity || info.Size() != size || fmt.Sprint(info.ModTime().UnixNano()) != mtime {
		f.Close()
		return nil, "", fmt.Errorf("원본이 변경됐습니다")
	}
	return f, path, nil
}

func (s *Service) prepare(ctx context.Context, id string) error {
	v, err := s.store.session(ctx, id)
	if err != nil {
		return err
	}
	if v.RenderVersion >= 2 {
		return s.prepareTimeline(ctx, id)
	}
	source, path, err := s.source(ctx, id)
	if err != nil {
		return err
	}
	defer source.Close()
	metadata, err := (engine.LocalInspector{FFprobe: s.editor.FFprobe, FFmpeg: s.editor.FFmpeg}).Inspect(ctx, path, "video")
	if err != nil {
		return err
	}
	if metadata.Duration == nil || *metadata.Duration <= 0 || v.Options.End > *metadata.Duration+0.02 {
		return fmt.Errorf("선택 구간이 원본 재생 시간을 초과합니다")
	}
	width, height, count := *metadata.Width, *metadata.Height, 0
	if v.Options.Kind == "gif" {
		cache, err := os.OpenRoot(s.cache)
		if err != nil {
			return err
		}
		defer cache.Close()
		if err = cache.Mkdir(id, 0700); err != nil {
			return err
		}
		dir := filepath.Join(s.cache, id)
		lock, err := media.LockDirectory(dir)
		if err != nil {
			return err
		}
		defer lock.Close()
		frames, err := cache.OpenRoot(id)
		if err != nil {
			return err
		}
		defer frames.Close()
		run, stop := context.WithCancel(ctx)
		defer stop()
		monitor := watchSize(run, frames, 256<<20, stop)
		err = s.editor.Prepare(run, path, dir, v.Options.Start, v.Options.End, v.Options.FPS, v.Options.Width, s.progress("media_edit_session", id, 0, 95))
		stop()
		<-monitor
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if errors.Is(err, engine.ErrStopUnconfirmed) {
				return err
			}
			return fmt.Errorf("프레임 준비 실패: %v", err)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err = checkSize(frames, 256<<20); err != nil {
			return err
		}
		for n := 0; n < 900; n++ {
			f, e := frames.Open(fmt.Sprintf("%04d.png", n))
			if os.IsNotExist(e) {
				break
			}
			if e != nil {
				return e
			}
			config, _, e := image.DecodeConfig(f)
			if e != nil {
				f.Close()
				return e
			}
			if config.Width < 1 || config.Height < 1 || config.Width > 960 || config.Height > 960 {
				f.Close()
				return fmt.Errorf("프레임 크기 한도 초과")
			}
			if n == 0 {
				width, height = config.Width, config.Height
			} else if width != config.Width || height != config.Height {
				f.Close()
				return fmt.Errorf("프레임 크기가 일치하지 않습니다")
			}
			if _, e = f.Seek(0, io.SeekStart); e != nil {
				f.Close()
				return e
			}
			img, _, e := image.Decode(f)
			f.Close()
			if e != nil {
				return e
			}
			if e = frameThumbnail(frames, n, img); e != nil {
				return e
			}
			if e = ctx.Err(); e != nil {
				return e
			}
			count++
		}
		if err = checkSize(frames, 256<<20); err != nil {
			return err
		}
		if count == 0 {
			return fmt.Errorf("프레임이 없습니다")
		}
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err = ctx.Err(); err != nil {
		return err
	}
	_, err = s.store.db.ExecContext(ctx, `UPDATE media_edit_session SET state='ready',width=?,height=?,frame_count=?,progress=100,updated_at=? WHERE id=?`, width, height, count, now(), id)
	return err
}

func frameThumbnail(root *os.Root, index int, source image.Image) error {
	b := source.Bounds()
	w, h := b.Dx(), b.Dy()
	tw, th := 120, 120
	if w >= h {
		th = max(1, h*tw/w)
	} else {
		tw = max(1, w*th/h)
	}
	thumb := image.NewRGBA(image.Rect(0, 0, tw, th))
	for y := 0; y < th; y++ {
		for x := 0; x < tw; x++ {
			thumb.Set(x, y, source.At(b.Min.X+x*w/tw, b.Min.Y+y*h/th))
		}
	}
	f, e := root.OpenFile(fmt.Sprintf("t%04d.jpg", index), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return e
	}
	e = jpeg.Encode(f, thumb, &jpeg.Options{Quality: 72})
	ce := f.Close()
	if e != nil {
		return e
	}
	return ce
}

func checkSize(root *os.Root, limit int64) error {
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	entries, err := dir.ReadDir(-1)
	dir.Close()
	if err != nil {
		return err
	}
	var size int64
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() {
			return fmt.Errorf("편집 자료에 허용하지 않는 항목이 있습니다")
		}
		info, e := entry.Info()
		if e != nil {
			return e
		}
		size += info.Size()
		if size > limit {
			return fmt.Errorf("편집 자료 크기 한도 초과")
		}
	}
	return nil
}
func watchSize(ctx context.Context, root *os.Root, limit int64, cancel context.CancelFunc) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(300 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if checkSize(root, limit) != nil {
					cancel()
					return
				}
			}
		}
	}()
	return done
}

func copyFrames(ctx context.Context, source, dest *os.Root, count int, deleted []int) (int, error) {
	removed := map[int]bool{}
	for _, n := range deleted {
		removed[n] = true
	}
	position := 0
	for n := 0; n < count; n++ {
		if removed[n] {
			continue
		}
		if e := ctx.Err(); e != nil {
			return 0, e
		}
		in, e := source.Open(fmt.Sprintf("%04d.png", n))
		if e != nil {
			return 0, e
		}
		out, e := dest.OpenFile(fmt.Sprintf("%04d.png", position), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			in.Close()
			return 0, e
		}
		_, e = io.Copy(out, io.LimitReader(in, 8<<20))
		ce := out.Close()
		in.Close()
		if e != nil {
			return 0, e
		}
		if ce != nil {
			return 0, ce
		}
		position++
	}
	return position, nil
}
