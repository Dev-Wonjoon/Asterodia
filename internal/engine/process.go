package engine

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type ProcessRunner struct{}

func (r *ProcessRunner) ReadVersion(
	ctx context.Context,
	name string,
	executable string,
) (string, error) {

	definition, err := lookupBinary(name)
	if err != nil {
		return "", err
	}

	if !filepath.IsAbs(executable) {
		return "", fmt.Errorf("실행 파일은 절대 경로여야 합니다")
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(
		ctx,
		executable,
		definition.versionArgs...,
	)
	cmd.Dir = filepath.Dir(executable)
	cmd.WaitDelay = time.Second
	configureVersionProcess(cmd)

	var stdout, stderr limitedOutput
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("%s 버전 조회: %w", name, ctx.Err())
		}

		return "", fmt.Errorf("%s 버전 조회: %w", name, err)
	}

	if stdout.truncated || stderr.truncated {
		return "", fmt.Errorf("%s 버전 조회 출력이 너무 큽니다", name)
	}

	version := strings.TrimSpace(
		strings.SplitN(stdout.String(), "\n", 2)[0],
	)
	if version == "" {
		return "", fmt.Errorf("%s 버전 응답이 비어 있습니다", name)
	}

	if name == "ffmpeg" &&
		!strings.HasPrefix(version, "ffmpeg version ") {
		return "", fmt.Errorf("FFmpeg 버전 응답이 올바르지 않습니다")
	}
	if name == "ffprobe" && !strings.HasPrefix(version, "ffprobe version ") {
		return "", fmt.Errorf("FFprobe 버전 응답이 올바르지 않습니다")
	}
	if name == "imagemagick" && !strings.HasPrefix(version, "Version: ImageMagick 7.1.2-34 ") {
		return "", fmt.Errorf("ImageMagick 7.1.2-34가 필요합니다")
	}

	if definition.expectedVersion != "" &&
		version != definition.expectedVersion {
		return "", fmt.Errorf(
			"%s 버전 불일치: 필요 %s, 현재 %s",
			name,
			definition.expectedVersion,
			version,
		)
	}

	return version, nil
}

// 출력은 스트림별 최대 64 KiB까지만 보관한다.
type limitedOutput struct {
	buffer    bytes.Buffer
	truncated bool
}

func (b *limitedOutput) Write(p []byte) (int, error) {
	size := len(p)
	remaining := 64*1024 - b.buffer.Len()

	if size > remaining {
		p = p[:remaining]
		b.truncated = true
	}

	_, err := b.buffer.Write(p)
	return size, err
}

func (b *limitedOutput) String() string {
	return b.buffer.String()
}

const processStopTimeout = 15 * time.Second

func (r *ProcessRunner) Run(
	ctx context.Context,
	request ProcessRequest,
) (ProcessResult, error) {
	if err := ctx.Err(); err != nil {
		return ProcessResult{ExitCode: -1}, err
	}

	if request.Timeout <= 0 && !request.Unbounded {
		return ProcessResult{ExitCode: -1},
			fmt.Errorf("프로세스 실행 제한 시간은 양수여야 합니다")
	}

	if !request.Unbounded {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, request.Timeout)
		defer cancel()
	}
	if observer, ok := ctx.Value(memoryKey{}).(func(ProcessMemory)); ok {
		next := request.OnMemory
		request.OnMemory = func(sample ProcessMemory) {
			observer(sample)
			if next != nil {
				next(sample)
			}
		}
	}
	observer, _ := ctx.Value(outputKey{}).(func(Output))
	if observer != nil {
		name := strings.TrimSuffix(strings.ToLower(filepath.Base(request.Executable)), ".exe")
		wrap := func(stream string, next func(string) error) func(string) error {
			return func(line string) error {
				observer(Output{Engine: name, Stream: stream, Text: line})
				if next != nil {
					return next(line)
				}
				return nil
			}
		}
		request.OnStdoutLine = wrap("stdout", request.OnStdoutLine)
		request.OnStderrLine = wrap("stderr", request.OnStderrLine)
		observer(Output{Engine: name, Stream: "system", Text: "프로세스 실행 시작"})
		result, err := runWindowsProcess(ctx, request)
		message := fmt.Sprintf("프로세스 종료 (코드 %d)", result.ExitCode)
		if err != nil {
			message += " · " + err.Error()
		}
		observer(Output{Engine: name, Stream: "system", Text: message})
		return result, err
	}
	return runWindowsProcess(ctx, request)
}

type processOutputResult struct {
	isStderr bool
	text     string
	err      error
}

func readProcessOutput(
	reader io.Reader,
	limit int,
	onLine func(string) error,
) (string, error) {
	source := &io.LimitedReader{
		R: reader,
		N: int64(limit) + 1,
	}

	scanner := bufio.NewScanner(source)
	scanner.Buffer(
		make([]byte, min(64<<10, limit+2)),
		limit+2,
	)

	var output strings.Builder

	for scanner.Scan() {
		if source.N == 0 {
			return output.String(), ErrOutputLimit
		}

		line := scanner.Text()
		output.WriteString(line)
		output.WriteByte('\n')

		if onLine != nil {
			if err := onLine(line); err != nil {
				return output.String(), err
			}
		}
	}

	if source.N == 0 {
		return output.String(), ErrOutputLimit
	}

	return output.String(), scanner.Err()
}

func readStreamingOutput(reader io.Reader, onLine func(string) error) (string, error) {
	const tailLimit = 64 << 10
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	var tail string
	for scanner.Scan() {
		line := scanner.Text()
		if onLine != nil {
			if err := onLine(line); err != nil {
				return tail, err
			}
		}
		// Keep memory bounded even for a long-running download's diagnostics.
		if len(line) >= tailLimit {
			tail = line[len(line)-tailLimit:]
		} else {
			if extra := len(tail) + len(line) + 1 - tailLimit; extra > 0 {
				tail = tail[extra:]
			}
			tail += line + "\n"
		}
	}
	return strings.ToValidUTF8(tail, "�"), scanner.Err()
}
