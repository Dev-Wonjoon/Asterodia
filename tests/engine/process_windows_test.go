package engine_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Dev-Wonjoon/StorageBucket/internal/engine"
)

const maxCancellationWait = 20 * time.Second

func processPathInside(base, target string) bool {
	relative, err := filepath.Rel(base, target)

	return err == nil &&
		!filepath.IsAbs(relative) &&
		relative != ".." &&
		!strings.HasPrefix(
			relative,
			".."+string(filepath.Separator),
		)
}

func processTestRoot(t *testing.T) string {
	t.Helper()

	configured := os.Getenv("STORAGEBUCKET_TEST_ROOT")
	if !filepath.IsAbs(configured) {
		t.Fatal("STORAGEBUCKET_TEST_ROOT에 절대 경로를 지정하세요")
	}

	root, err := filepath.EvalSymlinks(configured)
	if err != nil {
		t.Fatal(err)
	}

	_, source, _, ok := runtime.Caller(0)
	if !ok || !filepath.IsAbs(source) {
		t.Fatal("테스트 소스 경로를 확인할 수 없습니다")
	}

	repository, err := filepath.EvalSymlinks(
		filepath.Join(filepath.Dir(source), "..", ".."),
	)
	if err != nil {
		t.Fatal(err)
	}

	allowed := filepath.Join(
		repository,
		".test-data",
		"engine-process",
	)

	if !processPathInside(allowed, root) ||
		strings.EqualFold(root, allowed) {
		t.Fatal(
			"테스트 경로는 .test-data/engine-process/실행별폴더여야 합니다",
		)
	}

	return root
}

func processFixtureArgs(mode string) []string {
	return []string{
		"-test.run=^TestProcessFixture$",
		"--",
		"sb-process-fixture",
		mode,
	}
}

func processRequestForTest(
	t *testing.T,
	mode string,
) engine.ProcessRequest {
	t.Helper()

	root := processTestRoot(t)

	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		t.Fatal(err)
	}

	if !processPathInside(root, executable) {
		t.Fatal(
			"테스트 실행 파일도 테스트 전용 폴더 안에 있어야 합니다",
		)
	}

	directory, err := os.MkdirTemp(root, "case-")
	if err != nil {
		t.Fatal(err)
	}

	return engine.ProcessRequest{
		Executable: executable,
		Arguments:  processFixtureArgs(mode),
		Directory:  directory,
		Timeout:    10 * time.Second,
	}
}

// 테스트 바이너리 자체를 가짜 엔진으로 실행하는 진입점이다.
func TestProcessFixture(t *testing.T) {
	mode := ""

	for i := 1; i+2 < len(os.Args); i++ {
		if os.Args[i] == "--" &&
			os.Args[i+1] == "sb-process-fixture" {
			mode = os.Args[i+2]
			break
		}
	}

	if mode == "" {
		t.Skip("자식 프로세스 전용 진입점")
	}

	root := processTestRoot(t)

	directory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	directory, err = filepath.EvalSymlinks(directory)
	if err != nil {
		t.Fatal(err)
	}
	if !processPathInside(root, directory) {
		t.Fatal("가짜 엔진의 작업 경로가 테스트 폴더 밖입니다")
	}

	// 실행된 가짜 엔진이 계속 남지 않도록 자체 종료 시간을 둔다.
	time.AfterFunc(30*time.Second, func() {
		os.Exit(124)
	})

	switch mode {
	case "normal":
		fmt.Println("첫째")
		fmt.Println("둘째")
		fmt.Fprintln(os.Stderr, "진단 출력")

	case "failure":
		fmt.Fprintln(os.Stderr, "합성 실패")
		os.Exit(7)

	case "wait":
		fmt.Println("ready")
		time.Sleep(time.Minute)

	case "overflow":
		chunk := strings.Repeat("x", 64<<10)

		for i := 0; i < 300; i++ {
			fmt.Println(chunk)
		}

		time.Sleep(time.Minute)

	case "parent":
		child := exec.Command(
			os.Args[0],
			processFixtureArgs("heartbeat")...,
		)
		child.Stdout = os.Stdout
		child.Stderr = os.Stderr
		child.SysProcAttr = &syscall.SysProcAttr{
			HideWindow: true,
		}

		if err := child.Run(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(3)
		}

	case "heartbeat":
		filename := filepath.Join(directory, "heartbeat.txt")
		first := true

		for {
			data := []byte(fmt.Sprint(time.Now().UnixNano()))

			if err := os.WriteFile(filename, data, 0o600); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(2)
			}

			if first {
				fmt.Println("child-ready")
				first = false
			}

			time.Sleep(25 * time.Millisecond)
		}

	case "stdin":
		hash := sha256.New()
		if _, err := io.Copy(hash, os.Stdin); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(3)
		}
		fmt.Printf("%x\n", hash.Sum(nil))

	default:
		os.Exit(2)
	}

	os.Exit(0)
}

func TestProcessRunnerResults(t *testing.T) {
	for _, mode := range []string{"normal", "failure"} {
		t.Run(mode, func(t *testing.T) {
			request := processRequestForTest(t, mode)

			result, err := new(engine.ProcessRunner).Run(
				context.Background(),
				request,
			)

			if errors.Is(err, engine.ErrStopUnconfirmed) {
				t.Fatalf("종료 확인 실패: %v", err)
			}

			if mode == "normal" {
				if err != nil {
					t.Fatal(err)
				}

				if result.ExitCode != 0 {
					t.Fatalf(
						"정상 종료 코드가 아닙니다: %d",
						result.ExitCode,
					)
				}
				if result.Stdout != "첫째\n둘째\n" {
					t.Fatalf(
						"표준 출력 불일치: %q",
						result.Stdout,
					)
				}
				if result.Stderr != "진단 출력\n" {
					t.Fatalf(
						"오류 출력 불일치: %q",
						result.Stderr,
					)
				}

				return
			}

			if err == nil || result.ExitCode != 7 {
				t.Fatalf(
					"실패 종료 처리 오류: 코드=%d, 오류=%v",
					result.ExitCode,
					err,
				)
			}
			if !strings.Contains(result.Stderr, "합성 실패") {
				t.Fatalf("오류 출력 누락: %q", result.Stderr)
			}
		})
	}
}

func TestProcessRunnerStopCases(t *testing.T) {
	callbackErr := errors.New("합성 콜백 오류")

	cases := []struct {
		name     string
		mode     string
		timeout  time.Duration
		expected error
		onLine   func(string) error
	}{
		{
			name:     "시간초과",
			mode:     "wait",
			timeout:  150 * time.Millisecond,
			expected: context.DeadlineExceeded,
		},
		{
			name:     "출력제한",
			mode:     "overflow",
			timeout:  10 * time.Second,
			expected: engine.ErrOutputLimit,
		},
		{
			name:     "콜백오류",
			mode:     "wait",
			timeout:  10 * time.Second,
			expected: callbackErr,
			onLine: func(string) error {
				return callbackErr
			},
		},
	}

	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			request := processRequestForTest(t, item.mode)
			request.Timeout = item.timeout
			request.OnStdoutLine = item.onLine

			_, err := new(engine.ProcessRunner).Run(
				context.Background(),
				request,
			)

			if !errors.Is(err, item.expected) {
				t.Fatalf(
					"예상 오류=%v, 실제 오류=%v",
					item.expected,
					err,
				)
			}
			if errors.Is(err, engine.ErrStopUnconfirmed) {
				t.Fatalf(
					"프로세스 종료를 확인하지 못했습니다: %v",
					err,
				)
			}
		})
	}
}

func TestProcessRunnerCancellation(t *testing.T) {
	for _, mode := range []string{"wait", "parent"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			request := processRequestForTest(t, mode)
			cancelled := make(chan time.Time, 1)

			ready := "ready"
			if mode == "parent" {
				ready = "child-ready"
			}

			request.OnStdoutLine = func(line string) error {
				if line == ready {
					select {
					case cancelled <- time.Now():
					default:
					}

					cancel()
				}

				return nil
			}

			_, err := new(engine.ProcessRunner).Run(ctx, request)

			if !errors.Is(err, context.Canceled) {
				t.Fatalf("취소 오류가 아닙니다: %v", err)
			}
			if errors.Is(err, engine.ErrStopUnconfirmed) {
				t.Fatalf("취소 후 종료 확인 실패: %v", err)
			}

			select {
			case started := <-cancelled:
				if time.Since(started) > maxCancellationWait {
					t.Fatal("취소 후 종료 처리가 너무 오래 걸렸습니다")
				}
			default:
				t.Fatal("가짜 엔진의 실행을 확인하지 못했습니다")
			}

			if mode != "parent" {
				return
			}

			filename := filepath.Join(
				request.Directory,
				"heartbeat.txt",
			)

			before, err := os.ReadFile(filename)
			if err != nil {
				t.Fatal(err)
			}

			time.Sleep(250 * time.Millisecond)

			after, err := os.ReadFile(filename)
			if err != nil {
				t.Fatal(err)
			}

			if string(before) != string(after) {
				t.Fatal("취소 후에도 자식 프로세스가 파일을 기록합니다")
			}
		})
	}
}

func TestProcessRunnerStdin(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{name: "빈 입력"},
		{
			name:  "큰 입력",
			input: strings.Repeat("한글\r\n\x00", 256<<10),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			request := processRequestForTest(t, "stdin")
			request.Stdin = tc.input

			result, err := new(engine.ProcessRunner).Run(
				context.Background(),
				request,
			)
			if err != nil {
				t.Fatal(err)
			}

			expected := fmt.Sprintf(
				"%x\n",
				sha256.Sum256([]byte(tc.input)),
			)
			if result.Stdout != expected {
				t.Fatalf(
					"입력 전달 불일치: 예상=%q, 실제=%q",
					expected,
					result.Stdout,
				)
			}
		})
	}
}

func TestProcessRunnerStdinTimeout(t *testing.T) {
	// wait 모드는 표준 입력을 읽지 않고 대기한다.
	request := processRequestForTest(t, "wait")
	request.Stdin = strings.Repeat("x", 8<<20)
	request.Timeout = 500 * time.Millisecond

	started := time.Now()
	result, err := new(engine.ProcessRunner).Run(
		context.Background(),
		request,
	)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("시간 초과 오류가 아닙니다: %v", err)
	}
	if errors.Is(err, engine.ErrStopUnconfirmed) {
		t.Fatalf("시간 초과 후 종료 확인 실패: %v", err)
	}
	if time.Since(started) > maxCancellationWait {
		t.Fatal("입력 대기 중 종료 처리가 너무 오래 걸렸습니다")
	}
	if !strings.Contains(result.Stdout, "ready\n") {
		t.Fatal("가짜 엔진의 실행을 확인하지 못했습니다")
	}
}
