package engine

import (
	"archive/zip"
	"context"
	"fmt"
	"image"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/Dev-Wonjoon/StorageBucket/internal/licenses"
	_ "golang.org/x/image/webp"
)

const imageRuntimeName = "vcomp140.dll"
const imageRuntimeEntry = "Contents/VC/Redist/MSVC/14.44.35112/x64/Microsoft.VC143.OpenMP/vcomp140.dll"

func verifyImageRuntime(directory string) error {
	if err := verifyEngineDigest(filepath.Join(directory, imageRuntimeName), imageRuntimePin.BinarySHA); err != nil {
		return fmt.Errorf("ImageMagick 필수 런타임 검증 실패: %w. 설정에서 엔진을 다시 설치하세요", err)
	}
	return nil
}

func prepareImageRuntime(ctx context.Context, directory, staging string) error {
	if verifyImageRuntime(directory) == nil {
		return writeImageRuntimeNotice(directory)
	}
	archive := filepath.Join(staging, "image-runtime.vsix")
	if err := downloadEngineAsset(ctx, imageRuntimePin.URL, archive, nil); err != nil {
		return fmt.Errorf("ImageMagick 필수 런타임 다운로드: %w", err)
	}
	if err := extractImageRuntime(ctx, archive, directory, imageRuntimePin); err != nil {
		return err
	}
	return writeImageRuntimeNotice(directory)
}

func writeImageRuntimeNotice(directory string) error {
	for _, item := range licenses.List() {
		if item.Name == "Microsoft Visual C++ OpenMP Runtime" {
			return os.WriteFile(filepath.Join(directory, "vcomp140-LICENSE.txt"), []byte(item.Text), 0600)
		}
	}
	return fmt.Errorf("Microsoft Visual C++ 런타임 라이선스 고지가 없습니다")
}

func extractImageRuntime(ctx context.Context, archive, directory string, pin enginePin) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := verifyEngineDigest(archive, pin.ArchiveSHA); err != nil {
		return fmt.Errorf("ImageMagick 런타임 패키지 검증: %w", err)
	}
	reader, err := zip.OpenReader(archive)
	if err != nil {
		return fmt.Errorf("ImageMagick 런타임 압축 파일 읽기: %w", err)
	}
	defer reader.Close()
	var selected *zip.File
	for _, entry := range reader.File {
		if entry.Name != imageRuntimeEntry {
			continue
		}
		if selected != nil || !entry.Mode().IsRegular() || entry.UncompressedSize64 == 0 || entry.UncompressedSize64 > 16<<20 {
			return fmt.Errorf("ImageMagick 런타임 압축 항목이 올바르지 않습니다")
		}
		selected = entry
	}
	if selected == nil {
		return fmt.Errorf("ImageMagick 런타임 패키지에 %s가 없습니다", imageRuntimeName)
	}
	source, err := selected.Open()
	if err != nil {
		return err
	}
	defer source.Close()
	// Extract only the pinned release DLL to a fixed filename. No VSIX paths
	// or other payloads (including debug DLLs) are materialized or executed.
	target, err := os.CreateTemp(directory, ".runtime-*.dll")
	if err != nil {
		return err
	}
	temporary := target.Name()
	defer os.Remove(temporary)
	written, copyErr := io.Copy(target, io.LimitReader(imageArchiveReader{ctx, source}, int64(selected.UncompressedSize64)+1))
	closeErr := target.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if uint64(written) != selected.UncompressedSize64 {
		return fmt.Errorf("ImageMagick 런타임 DLL 크기가 올바르지 않습니다")
	}
	if err = verifyEngineDigest(temporary, pin.BinarySHA); err != nil {
		return fmt.Errorf("ImageMagick 런타임 DLL 검증: %w", err)
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return replaceEngineBinary(temporary, filepath.Join(directory, imageRuntimeName))
}

func validateImageConversion(ctx context.Context, executable, staging string) error {
	output := filepath.Join(staging, "image-install-check.webp")
	defer os.Remove(output)
	_, err := new(ProcessRunner).Run(ctx, ProcessRequest{
		Executable: executable, Directory: filepath.Dir(executable), Timeout: 15 * time.Second,
		Arguments: []string{"-limit", "thread", "1", "-size", "2x2", "xc:#4f46e5", "-strip", "webp:" + output},
	})
	if err != nil {
		return fmt.Errorf("ImageMagick 이미지 변환 검증 실패: %w", err)
	}
	file, err := os.Open(output)
	if err != nil {
		return fmt.Errorf("ImageMagick 이미지 변환 결과 확인: %w", err)
	}
	defer file.Close()
	config, format, err := image.DecodeConfig(file)
	if err != nil || format != "webp" || config.Width != 2 || config.Height != 2 {
		return fmt.Errorf("ImageMagick 이미지 변환 결과가 올바르지 않습니다")
	}
	return nil
}
