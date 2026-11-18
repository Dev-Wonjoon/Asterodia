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

	if request.Timeout <= 0 {
		return ProcessResult{ExitCode: -1},
			fmt.Errorf("프로세스 실행 제한 시간은 양수여야 합니다")
	}

	ctx, cancel := context.WithTimeout(ctx, request.Timeout)
	defer cancel()

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
