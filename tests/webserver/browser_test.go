package webserver_test

import (
	"bufio"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Dev-Wonjoon/StorageBucket/internal/engine"
	"github.com/Dev-Wonjoon/StorageBucket/internal/media"
)

// Opt-in fixture bridge for the isolated browser test; never part of the app.
func TestBrowserFixture(t *testing.T) {
	if os.Getenv("STORAGEBUCKET_BROWSER_FIXTURE") != "1" {
		return
	}
	f := newFixture(t)
	for i := 0; i < 28; i++ {
		title := fmt.Sprintf("이미지 샘플 %02d", i+1)
		if i == 0 {
			title = `xss <img src=x onerror=window.injected=1>`
		}
		_, _, path := f.seed(t, title, "image", ".png")
		photo := image.NewRGBA(image.Rect(0, 0, 320, 240))
		for y := 0; y < 240; y++ {
			for x := 0; x < 320; x++ {
				photo.Set(x, y, color.RGBA{uint8((x + i*20) % 256), uint8((y + i*13) % 256), uint8(100 + i*4), 255})
			}
		}
		out, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		err = png.Encode(out, photo)
		out.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	id, _, path := f.seed(t, "여러 파일 게시글", "image", ".png")
	for position := 1; position < 26; position++ {
		if _, err := f.db.Exec("INSERT INTO media_file(media_id,file_key,storage_kind,filepath,kind,position,created_at) VALUES(?,?,'unverified_legacy',?,'image',?,0)", id, fmt.Sprint(position), path, position); err != nil {
			t.Fatal(err)
		}
	}
	_, _, imagePath := f.seed(t, "사진·영상·오디오", "image", ".png")
	var mixedID int64
	if err := f.db.QueryRow("SELECT media_id FROM media_file WHERE filepath=?", imagePath).Scan(&mixedID); err != nil {
		t.Fatal(err)
	}
	ffmpeg := os.Getenv("STORAGEBUCKET_TEST_FFMPEG_EXE")
	if !filepath.IsAbs(ffmpeg) {
		t.Fatal("FFmpeg 절대 경로가 필요합니다")
	}
	for index, kind := range []string{"video", "audio"} {
		ext := ".mp4"
		args := []string{"-nostdin", "-loglevel", "error", "-f", "lavfi", "-i", "color=c=blue:s=160x120:d=1", "-f", "lavfi", "-i", "sine=frequency=800:duration=1", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest", "-movflags", "+faststart"}
		if kind == "audio" {
			ext = ".mp3"
			args = []string{"-nostdin", "-loglevel", "error", "-f", "lavfi", "-i", "sine=frequency=800:duration=1", "-c:a", "libmp3lame"}
		}
		file := filepath.Join(f.root, kind+ext)
		args = append(args, file)
		if _, err := new(engine.ProcessRunner).Run(t.Context(), engine.ProcessRequest{Executable: ffmpeg, Directory: f.root, Timeout: time.Minute, Arguments: args}); err != nil {
			t.Fatal(err)
		}
		if _, err := f.db.Exec("INSERT INTO media_file(media_id,file_key,storage_kind,filepath,kind,position,created_at) VALUES(?,?,'unverified_legacy',?,?,?,0)", mixedID, kind, file, kind, index+1); err != nil {
			t.Fatal(err)
		}
	}
	var imageID, videoID, audioID int64
	if err := f.db.QueryRow("SELECT min(CASE kind WHEN 'image' THEN id END),min(CASE kind WHEN 'video' THEN id END),min(CASE kind WHEN 'audio' THEN id END) FROM media_file WHERE media_id=?", mixedID).Scan(&imageID, &videoID, &audioID); err != nil {
		t.Fatal(err)
	}
	albumName := "사용자 앨범 · 감상 테스트"
	if result := media.NewService(f.db).SaveAlbum(t.Context(), media.AlbumRequest{Title: &albumName, AddFileIDs: []int64{videoID, audioID, imageID, 1}}); !result.OK {
		t.Fatal(result.Error)
	}
	emit := func(id int, result any) {
		data, _ := json.Marshal(map[string]any{"id": id, "result": result})
		fmt.Println("__FIXTURE__" + string(data))
	}
	emit(0, f.server.Status())
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var command struct {
			ID int    `json:"id"`
			Op string `json:"op"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &command); err != nil {
			t.Fatal(err)
		}
		switch command.Op {
		case "start":
			emit(command.ID, f.server.Start())
		case "stop":
			emit(command.ID, f.server.Stop())
		case "status":
			emit(command.ID, f.server.Status())
		case "exit":
			emit(command.ID, f.server.Stop())
			return
		default:
			t.Fatal("알 수 없는 fixture 명령")
		}
	}
}
