package mediaedit

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/Dev-Wonjoon/Asterodia/internal/engine"
	"github.com/Dev-Wonjoon/Asterodia/internal/media"
)

func (s *Service) render(ctx context.Context, id string) (err error) {
	j, err := s.store.job(ctx, id)
	if err != nil {
		return err
	}
	v, err := s.store.session(ctx, j.SessionID)
	if err != nil {
		return err
	}
	var outputRoot, resultUUID string
	if err = s.store.db.QueryRowContext(ctx, "SELECT output_root,result_uuid FROM media_edit_job WHERE id=?", id).Scan(&outputRoot, &resultUUID); err != nil {
		return err
	}
	if err = os.MkdirAll(outputRoot, 0700); err != nil {
		return err
	}
	actual, err := filepath.EvalSymlinks(outputRoot)
	if err != nil {
		return err
	}
	rootLock, err := media.LockDirectory(actual)
	if err != nil {
		return err
	}
	defer rootLock.Close()
	root, err := os.OpenRoot(actual)
	if err != nil {
		return err
	}
	defer root.Close()
	direct := j.Options.OutputDirectory != "" && !j.Preview
	stageDirectory := "edits"
	if direct {
		stageDirectory = ".asterodia-tmp"
	}
	if err = root.MkdirAll(stageDirectory, 0700); err != nil {
		return err
	}
	editsLock, err := media.LockDirectory(filepath.Join(actual, stageDirectory))
	if err != nil {
		return err
	}
	defer editsLock.Close()
	workRel := filepath.Join(stageDirectory, "."+id)
	if err = root.Mkdir(workRel, 0700); err != nil {
		return err
	}
	workPath := filepath.Join(actual, workRel)
	workLock, err := media.LockDirectory(workPath)
	if err != nil {
		return err
	}
	defer workLock.Close()
	work, err := root.OpenRoot(workRel)
	if err != nil {
		return err
	}
	defer work.Close()
	defer func() {
		work.Close()
		workLock.Close()
		if !errors.Is(err, engine.ErrStopUnconfirmed) {
			_ = root.RemoveAll(workRel)
			if direct {
				editsLock.Close()
				_ = root.Remove(stageDirectory)
			}
		}
	}()
	ext := ".gif"
	if j.Options.Kind == "trim" {
		ext = ".mp4"
	} else if j.Options.Kind == "webp" {
		ext = ".webp"
	}
	stagedRel := filepath.Join(workRel, "output"+ext)
	staged := filepath.Join(actual, stagedRel)
	count := 0
	expectedDuration := 0.0
	if j.Options.Kind != "trim" && v.Options.QuickVideo {
		pin, path, e := s.source(ctx, v.ID)
		if e != nil {
			return e
		}
		defer pin.Close()
		start, end, e := trimBounds(v, j.Options)
		if e != nil {
			return e
		}
		if j.Preview {
			end = min(end, start+2)
		}
		duration, width, height := v.Options.End, v.Width, v.Height
		timeline, e := s.editor.ReadVideoClipTimeline(ctx, path, engine.LocalMetadata{Width: &width, Height: &height, Duration: &duration}, start, end)
		if e != nil {
			return e
		}
		if j.Options.Kind == "gif" {
			for _, delay := range timeline.Delays {
				if delay < 10 || delay > 655350 {
					return errGIFFrameTiming
				}
			}
		}
		count, expectedDuration = len(timeline.Delays), timeline.Duration()
		request := engine.AnimationRequest{Source: path, Start: start, End: end, FPS: 1000, FrameCount: count, ExpectedFrames: count, Crop: j.Options.Crop, Format: j.Options.Kind, Quality: j.Options.Quality, NativeVideo: true, DelayRate: timeline.Rate(), Delays: timeline.Delays, Duration: expectedDuration}
		err = s.editor.Animation(ctx, request, staged, s.progress("media_edit_job", id, 0, 88))
	} else if j.Options.Kind != "trim" {
		first, end, expected, factory, planErr := s.framePlan(v, j.Options)
		if planErr != nil {
			return planErr
		}
		count = expected
		sourcePath := filepath.Join(s.cache, v.ID, "%04d.png")
		var pin *os.File
		if v.RenderVersion >= 2 {
			pin, sourcePath, err = s.source(ctx, v.ID)
			if err != nil {
				return err
			}
			defer pin.Close()
		}
		request := engine.AnimationRequest{Source: sourcePath, Start: v.Options.Start, End: v.Options.End, FPS: v.Options.FPS, FrameCount: end, FirstFrame: first, ExpectedFrames: expected, Crop: j.Options.Crop, Format: j.Options.Kind, Quality: j.Options.Quality, NewSkip: factory, PictureSequence: v.RenderVersion == 1}
		expectedDuration = float64(expected) / float64(v.Options.FPS)
		if v.Options.NativeGIF || v.Options.NativeVideo || v.Options.NativeWebP {
			timeline, e := s.timing(v)
			if e != nil {
				return e
			}
			request.NativeGIF = v.Options.NativeGIF
			request.NativeWebP = v.Options.NativeWebP
			request.NativeVideo = v.Options.NativeVideo
			request.DelayRate = timeline.Rate()
			request.Delays = append([]int(nil), timeline.Delays...)
			skip := factory()
			ticks := 0
			for n := first; n < end; n++ {
				drop, e := skip(n)
				if e != nil {
					return e
				}
				if drop {
					continue
				}
				if n >= len(request.Delays) {
					return fmt.Errorf("GIF 재생 정보 오류")
				}
				if j.Preview {
					request.Delays[n] = min(request.Delays[n], 2*timeline.Rate()-ticks)
				}
				ticks += request.Delays[n]
			}
			expectedDuration = float64(ticks) / float64(timeline.Rate())
		}
		err = s.editor.Animation(ctx, request, staged, s.progress("media_edit_job", id, 0, 88))
	} else {
		pin, path, sourceErr := s.source(ctx, v.ID)
		if sourceErr != nil {
			return sourceErr
		}
		defer pin.Close()
		trimStart, trimEnd, e := trimBounds(v, j.Options)
		if e != nil {
			return e
		}
		expectedDuration = trimEnd - trimStart
		err = s.editor.Trim(ctx, path, staged, trimStart, trimEnd, j.Options.Mute, s.progress("media_edit_job", id, 0, 88))
	}
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = s.store.state("media_edit_job", id, "verifying", ""); err != nil {
		return err
	}
	metadata, frames, err := s.editor.Verify(ctx, staged)
	if err != nil {
		return err
	}
	if j.Options.Kind != "trim" && ((j.Options.Kind == "gif" && frames != count) || *metadata.Width != j.Options.Crop.Width || *metadata.Height != j.Options.Crop.Height) {
		return fmt.Errorf("GIF 프레임 또는 크롭 결과가 일치하지 않습니다")
	}
	if j.Options.Kind == "trim" && (*metadata.Duration < expectedDuration-0.2 || *metadata.Duration > expectedDuration+0.3) {
		return fmt.Errorf("영상 구간 길이가 일치하지 않습니다")
	}
	if j.Options.Kind == "webp" {
		expected := expectedDuration
		if frames == 1 && *metadata.Duration == 0 {
			metadata.Duration = &expected
		} else if diff := math.Abs(*metadata.Duration - expected); diff > max(0.025, 1/float64(v.Options.FPS)+0.002) {
			return fmt.Errorf("WebP 재생 시간이 일치하지 않습니다")
		}
	}
	if j.Preview {
		info, e := os.Stat(staged)
		if e != nil {
			return e
		}
		previewRel := filepath.Join("previews", id+ext)
		if e = root.MkdirAll("previews", 0700); e != nil {
			return e
		}
		input, e := root.Open(stagedRel)
		if e != nil {
			return e
		}
		defer input.Close()
		output, e := root.OpenFile(previewRel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return e
		}
		_, e = io.Copy(output, input)
		if e == nil {
			e = output.Sync()
		}
		closeErr := output.Close()
		if e == nil {
			e = closeErr
		}
		if e != nil {
			_ = root.Remove(previewRel)
			return e
		}
		s.noteOutput(id, info.Size(), count)
		_, e = s.store.db.ExecContext(ctx, "UPDATE media_edit_job SET state='completed',progress=100,result_path=?,updated_at=? WHERE id=?", filepath.Join(actual, previewRel), now(), id)
		if e == nil {
			s.expirePreviews(ctx, id)
		}
		return e
	}
	// Flush before taking the immutable publication handle.
	flush, err := root.OpenFile(stagedRel, os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	err = flush.Sync()
	flush.Close()
	if err != nil {
		return err
	}
	file, err := media.OpenOwnedFile(root, stagedRel)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if info.Size() <= 0 {
		return fmt.Errorf("결과 파일이 비어 있습니다")
	}
	hash := sha256.New()
	if _, err = io.Copy(hash, file); err != nil {
		return err
	}
	name := media.PathComponent(v.Title, "영상", 70) + "_" + id + ext
	proof := media.FileProof{Path: filepath.Join("edits", name), Size: info.Size(), Hash: fmt.Sprintf("%x", hash.Sum(nil))}
	if direct {
		proof.Path = name
	}
	finalPath := filepath.Join(actual, proof.Path)
	targetRoot := root
	if !direct {
		targetRoot, err = root.OpenRoot("edits")
		if err != nil {
			return err
		}
		defer targetRoot.Close()
	}
	// Cancellation ends here. Exclusive creation denies writes/deletes until
	// commit. Persist expected contents and actual ownership before copying.
	var published *os.File
	s.mu.Lock()
	if err = ctx.Err(); err == nil {
		published, err = media.CreateOwnedFile(targetRoot, name)
		if err == nil {
			proof.Identity, err = media.FileIdentity(published)
			if err == nil {
				_, err = s.store.db.ExecContext(ctx, `UPDATE media_edit_job SET state='publishing',progress=95,result_path=?,proof_json=?,updated_at=? WHERE id=?`, finalPath, raw(proof), now(), id)
			}
		}
	}
	s.mu.Unlock()
	if published != nil {
		defer published.Close()
	}
	if err != nil {
		if published != nil {
			_ = media.DeleteOwnedFile(published)
		}
		return err
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if _, err = io.Copy(published, file); err != nil {
		return err
	}
	if err = published.Sync(); err != nil {
		return err
	}
	if err = media.VerifyOwnedFile(published, proof); err != nil {
		return err
	}
	// Downgrade to a read-only pin before publishing the DB row so thumbnails
	// and clients can read immediately, while replacement remains forbidden.
	published.Close()
	readPin, err := media.OpenReadFile(targetRoot, name)
	if err != nil {
		return err
	}
	defer readPin.Close()
	if err = media.VerifyOwnedFile(readPin, proof); err != nil {
		return err
	}
	if err = s.commit(context.Background(), j, v, resultUUID, finalPath, proof, metadata, frames); err != nil {
		return err
	}
	return nil
}

func (s *Service) commit(ctx context.Context, j Job, v Session, resultUUID, path string, proof media.FileProof, metadata engine.LocalMetadata, frames int) error {
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var existing *int64
	if err = tx.QueryRowContext(ctx, "SELECT result_file_id FROM media_edit_job WHERE id=?", j.ID).Scan(&existing); err != nil {
		return err
	}
	if existing != nil {
		return nil
	}
	title := v.Title + " · GIF"
	if j.Options.Kind == "webp" {
		title = v.Title + " · WebP"
	}
	if j.Options.Kind == "trim" {
		title = v.Title + " · 잘라낸 영상"
	}
	res, err := tx.ExecContext(ctx, "INSERT INTO media(source_kind,source_title,created_at,updated_at)VALUES('edited',?,?,?)", title, now(), now())
	if err != nil {
		return err
	}
	mediaID, err := res.LastInsertId()
	if err != nil {
		return err
	}
	dirID, err := media.NewStore(tx).EnsureDirectory(ctx, filepath.Dir(path), "recorded-v1:"+filepath.Dir(path), 0, false)
	if err != nil {
		return err
	}
	res, err = tx.ExecContext(ctx, `INSERT INTO media_file(media_id,file_key,file_uuid,storage_kind,filepath,directory_id,kind,position,filesize,hash_algorithm,content_hash,duration,detected_format,width,height,frame_count,created_at)VALUES(?,?,?,'app_managed',?,?,?,0,?,'sha256',?,?,?,?,?,?,?)`, mediaID, "edit:"+j.ID, resultUUID, path, dirID, metadata.Kind, proof.Size, proof.Hash, metadata.Duration, metadata.Format, metadata.Width, metadata.Height, frames, now())
	if err != nil {
		return err
	}
	fileID, err := res.LastInsertId()
	if err != nil {
		return err
	}
	skippedAlbums, err := classifyOutput(ctx, tx, j.Options, mediaID, fileID)
	if err != nil {
		return err
	}
	notice := ""
	if skippedAlbums > 0 {
		notice = "일부 출력 앨범이 삭제되었습니다. 결과 파일과 태그는 저장했습니다."
	}
	if _, err = tx.ExecContext(ctx, "UPDATE media_edit_job SET state='completed',progress=100,result_file_id=?,error_message=?,updated_at=? WHERE id=?", fileID, notice, now(), j.ID); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if s.changed != nil {
		s.changed(mediaID)
	}
	return nil
}

func (s *Service) recover() error {
	ctx := context.Background()
	if _, err := s.store.db.Exec("UPDATE media_edit_page SET state='failed',error_message='앱 종료로 프레임 읽기가 중단됐습니다' WHERE state IN ('pending','running')"); err != nil {
		return err
	}
	// Pending work is never silently restarted after an application restart.
	if _, err := s.store.db.ExecContext(ctx, `UPDATE media_edit_session SET state=CASE WHEN cancel_requested=1 THEN 'cancelled' ELSE 'interrupted' END,error_message='앱 종료로 중단됐습니다. 새 편집으로 다시 준비하세요.',updated_at=? WHERE state IN ('pending','preparing','stop_unconfirmed')`, now()); err != nil {
		return err
	}
	ids, err := s.store.ids(ctx, "media_edit_job", "WHERE state IN ('publishing','interrupted') AND proof_json IS NOT NULL AND preview=0")
	if err != nil {
		return err
	}
	for _, id := range ids {
		j, err := s.store.job(ctx, id)
		if err != nil {
			return err
		}
		v, err := s.store.session(ctx, j.SessionID)
		if err != nil {
			return err
		}
		var path, rootPath, resultUUID, proofJSON string
		if err = s.store.db.QueryRowContext(ctx, "SELECT result_path,output_root,result_uuid,proof_json FROM media_edit_job WHERE id=?", id).Scan(&path, &rootPath, &resultUUID, &proofJSON); err != nil {
			return err
		}
		var proof media.FileProof
		if json.Unmarshal([]byte(proofJSON), &proof) != nil || !filepath.IsLocal(proof.Path) {
			continue
		}
		if j.Options.OutputDirectory != "" {
			if proof.Path != filepath.Base(proof.Path) {
				continue
			}
		} else if !strings.HasPrefix(proof.Path, "edits"+string(filepath.Separator)) {
			continue
		}
		// Preserve a conflicting/missing file; never delete or overwrite it.
		root, err := os.OpenRoot(rootPath)
		if err != nil {
			continue
		}
		f, err := media.OpenReadFile(root, proof.Path)
		root.Close()
		if err != nil {
			continue
		}
		err = media.VerifyOwnedFile(f, proof)
		if err == nil {
			var metadata engine.LocalMetadata
			var frames int
			metadata, frames, err = s.editor.Verify(ctx, path)
			if err == nil {
				err = s.commit(ctx, j, v, resultUUID, path, proof, metadata, frames)
			}
		}
		f.Close()
		if err != nil {
			continue
		}
	}
	_, err = s.store.db.ExecContext(ctx, `UPDATE media_edit_job SET state=CASE WHEN cancel_requested=1 THEN 'cancelled' ELSE 'interrupted' END,error_message='앱 종료 또는 저장 확정 중단으로 멈췄습니다. 원본과 기존 결과는 보존했습니다.',updated_at=? WHERE state IN ('pending','running','verifying','publishing','stop_unconfirmed')`, now())
	return err
}

// Encoded samples are disposable; measured statistics remain in job history.
func (s *Service) expirePreviews(ctx context.Context, keep string) {
	ids, err := s.store.ids(ctx, "media_edit_job", "WHERE preview=1 AND state='completed' AND result_path<>'' ORDER BY updated_at DESC,id DESC LIMIT -1 OFFSET 8")
	if err != nil {
		return
	}
	root, err := os.OpenRoot(s.cache)
	if err != nil {
		return
	}
	defer root.Close()
	for _, id := range ids {
		if id == keep {
			continue
		}
		j, err := s.store.job(ctx, id)
		if err != nil {
			continue
		}
		ext := ".gif"
		if j.Options.Kind == "webp" {
			ext = ".webp"
		}
		if root.Remove(filepath.Join("previews", id+ext)) == nil {
			_, _ = s.store.db.ExecContext(ctx, "UPDATE media_edit_job SET result_path='' WHERE id=?", id)
		}
	}
}
