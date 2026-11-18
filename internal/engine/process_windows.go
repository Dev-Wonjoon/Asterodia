package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func configureVersionProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow: true,
	}
}

type windowsJob struct {
	handle windows.Handle
}

// Windows의 JOBOBJECT_BASIC_ACCOUNTING_INFORMATION 구조체와 대응한다.
type jobAccounting struct {
	TotalUserTime             int64
	TotalKernelTime           int64
	ThisPeriodTotalUserTime   int64
	ThisPeriodTotalKernelTime int64
	TotalPageFaultCount       uint32
	TotalProcesses            uint32
	ActiveProcesses           uint32
	TotalTerminatedProcesses  uint32
}

func newWindowsJob() (*windowsJob, error) {
	handle, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("프로세스 그룹 생성: %w", err)
	}

	var limits windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	limits.BasicLimitInformation.LimitFlags =
		windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE

	_, err = windows.SetInformationJobObject(
		handle,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&limits)),
		uint32(unsafe.Sizeof(limits)),
	)
	if err != nil {
		windows.CloseHandle(handle)
		return nil, fmt.Errorf("프로세스 그룹 설정: %w", err)
	}

	return &windowsJob{handle: handle}, nil
}

// 엔진이 실행을 시작하기 전에 그룹에 연결한다.
func (j *windowsJob) assign(process windows.Handle) error {
	if err := windows.AssignProcessToJobObject(
		j.handle, process,
	); err != nil {
		return fmt.Errorf("엔진을 프로세스 그룹에 연결: %w", err)
	}

	return nil
}

// 그룹에 속한 프로세스 전체에 종료를 요청한다.
func (j *windowsJob) terminate() error {
	if err := windows.TerminateJobObject(j.handle, 1); err != nil {
		return fmt.Errorf(
			"%w: 프로세스 그룹 종료 요청: %w",
			ErrStopUnconfirmed,
			err,
		)
	}

	return nil
}

// 실행 중인 프로세스가 0개가 될 때까지 확인한다.
func (j *windowsJob) waitEmpty(timeout time.Duration) error {
	if timeout <= 0 {
		return fmt.Errorf(
			"%w: 종료 확인 제한 시간은 양수여야 합니다",
			ErrStopUnconfirmed,
		)
	}

	deadline := time.Now().Add(timeout)

	for {
		var info jobAccounting

		err := windows.QueryInformationJobObject(
			j.handle,
			windows.JobObjectBasicAccountingInformation,
			uintptr(unsafe.Pointer(&info)),
			uint32(unsafe.Sizeof(info)),
			nil,
		)
		if err != nil {
			return fmt.Errorf(
				"%w: 프로세스 그룹 상태 조회: %w",
				ErrStopUnconfirmed,
				err,
			)
		}

		if info.ActiveProcesses == 0 {
			return nil
		}

		if !time.Now().Before(deadline) {
			return fmt.Errorf(
				"%w: 남은 프로세스 %d개",
				ErrStopUnconfirmed,
				info.ActiveProcesses,
			)
		}

		time.Sleep(20 * time.Millisecond)
	}
}

func (j *windowsJob) close() error {
	if j.handle == 0 {
		return nil
	}

	if err := windows.CloseHandle(j.handle); err != nil {
		return fmt.Errorf("프로세스 그룹 핸들 해제: %w", err)
	}

	j.handle = 0
	return nil
}

// stdin, stdout, stderr는 해당 실행에서 준비한 자식용 핸들이다.
// 반환한 Process와 Thread 핸들의 소유권은 호출자에게 있다.
func createSuspendedProcess(
	executable string,
	args []string,
	directory string,
	stdin windows.Handle,
	stdout windows.Handle,
	stderr windows.Handle,
) (*windows.ProcessInformation, error) {
	if !filepath.IsAbs(executable) ||
		!filepath.IsAbs(directory) {
		return nil, fmt.Errorf(
			"실행 파일과 작업 디렉터리는 절대 경로여야 합니다",
		)
	}

	if !strings.EqualFold(filepath.Ext(executable), ".exe") {
		return nil, fmt.Errorf("엔진 실행 파일은 EXE여야 합니다")
	}

	executablePtr, err := windows.UTF16PtrFromString(executable)
	if err != nil {
		return nil, fmt.Errorf("실행 파일 경로 변환: %w", err)
	}

	directoryPtr, err := windows.UTF16PtrFromString(directory)
	if err != nil {
		return nil, fmt.Errorf("작업 디렉터리 변환: %w", err)
	}

	for _, arg := range args {
		if strings.ContainsRune(arg, '\x00') {
			return nil, fmt.Errorf("실행 인자에 NUL 문자가 있습니다")
		}
	}

	arguments := append([]string{executable}, args...)

	commandLine, err := windows.UTF16FromString(
		windows.ComposeCommandLine(arguments),
	)
	if err != nil {
		return nil, fmt.Errorf("실행 인자 변환: %w", err)
	}
	if len(commandLine) > 32767 {
		return nil, fmt.Errorf("실행 인자가 너무 깁니다")
	}

	// 중복 핸들을 제외하고 상속할 목록을 만든다.
	inherited := make([]windows.Handle, 0, 3)
	seen := make(map[windows.Handle]bool)

	for _, handle := range []windows.Handle{stdin, stdout, stderr} {
		if handle == 0 || handle == windows.InvalidHandle {
			return nil, fmt.Errorf("표준 입출력 핸들이 올바르지 않습니다")
		}
		if seen[handle] {
			continue
		}

		if err := windows.SetHandleInformation(
			handle,
			windows.HANDLE_FLAG_INHERIT,
			windows.HANDLE_FLAG_INHERIT,
		); err != nil {
			return nil, fmt.Errorf("입출력 핸들 상속 설정: %w", err)
		}

		seen[handle] = true
		inherited = append(inherited, handle)
	}

	attributes, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return nil, fmt.Errorf("프로세스 속성 생성: %w", err)
	}
	defer attributes.Delete()

	if err := attributes.Update(
		windows.PROC_THREAD_ATTRIBUTE_HANDLE_LIST,
		unsafe.Pointer(&inherited[0]),
		uintptr(len(inherited))*unsafe.Sizeof(inherited[0]),
	); err != nil {
		return nil, fmt.Errorf("입출력 상속 목록 설정: %w", err)
	}

	startup := windows.StartupInfoEx{
		StartupInfo: windows.StartupInfo{
			Cb:        uint32(unsafe.Sizeof(windows.StartupInfoEx{})),
			Flags:     windows.STARTF_USESTDHANDLES,
			StdInput:  stdin,
			StdOutput: stdout,
			StdErr:    stderr,
		},
		ProcThreadAttributeList: attributes.List(),
	}

	flags := uint32(
		windows.CREATE_SUSPENDED |
			windows.CREATE_NO_WINDOW |
			windows.CREATE_UNICODE_ENVIRONMENT |
			windows.EXTENDED_STARTUPINFO_PRESENT,
	)

	process := new(windows.ProcessInformation)

	err = windows.CreateProcess(
		executablePtr,
		&commandLine[0],
		nil,
		nil,
		true,
		flags,
		nil,
		directoryPtr,
		&startup.StartupInfo,
		process,
	)
	if err != nil {
		return nil, fmt.Errorf("엔진 프로세스 생성: %w", err)
	}

	return process, nil
}

func resumeWindowsProcess(
	process *windows.ProcessInformation,
) error {
	if process == nil || process.Thread == 0 {
		return fmt.Errorf("실행을 재개할 스레드 핸들이 없습니다")
	}

	previousCount, err := windows.ResumeThread(process.Thread)
	if err != nil {
		return fmt.Errorf("엔진 실행 재개: %w", err)
	}

	if previousCount != 1 {
		return fmt.Errorf(
			"예상하지 못한 스레드 정지 횟수: %d",
			previousCount,
		)
	}

	return nil
}

func (j *windowsJob) activeCount() (uint32, error) {
	var info jobAccounting

	err := windows.QueryInformationJobObject(
		j.handle,
		windows.JobObjectBasicAccountingInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
		nil,
	)
	if err != nil {
		return 0, fmt.Errorf("프로세스 그룹 상태 조회: %w", err)
	}

	return info.ActiveProcesses, nil
}

func closeWindowsHandle(handle *windows.Handle) error {
	if *handle == 0 {
		return nil
	}

	if err := windows.CloseHandle(*handle); err != nil {
		return err
	}

	*handle = 0
	return nil
}

func stopWindowsProcess(
	job *windowsJob,
	process *windows.ProcessInformation,
	assigned bool,
) error {
	deadline := time.Now().Add(processStopTimeout)

	threadErr := closeWindowsHandle(&process.Thread)

	var terminateErr error

	if assigned {
		terminateErr = job.terminate()
	} else if process.Process != 0 {
		// Job 연결에 실패한 경우에도 정지 상태의 엔진을 종료한다.
		terminateErr = windows.TerminateProcess(process.Process, 1)
	}

	var waitErr error

	if process.Process != 0 {
		milliseconds := uint32(max(
			int64(1),
			time.Until(deadline).Milliseconds(),
		))

		status, err := windows.WaitForSingleObject(
			process.Process,
			milliseconds,
		)
		if err != nil {
			waitErr = err
		} else if status != windows.WAIT_OBJECT_0 {
			waitErr = fmt.Errorf("엔진 종료 대기 시간 초과")
		}
	}

	handleErr := errors.Join(
		threadErr,
		closeWindowsHandle(&process.Process),
	)

	var groupErr error

	if assigned {
		groupErr = job.waitEmpty(max(
			time.Millisecond,
			time.Until(deadline),
		))
	}

	if waitErr != nil || groupErr != nil {
		return errors.Join(
			ErrStopUnconfirmed,
			terminateErr,
			waitErr,
			groupErr,
			handleErr,
		)
	}

	// 종료 요청이 실패했더라도 실제 종료가 확인되면
	// 종료 미확인 상태로 취급하지 않는다.
	return handleErr
}

func runWindowsProcess(
	ctx context.Context,
	request ProcessRequest,
) (result ProcessResult, runErr error) {
	result.ExitCode = -1

	job, err := newWindowsJob()
	if err != nil {
		return result, err
	}
	defer func() {
		runErr = errors.Join(runErr, job.close())
	}()

	var stdin, stdinWriter *os.File
	if request.Stdin == "" {
		stdin, err = os.Open(os.DevNull)
	} else {
		stdin, stdinWriter, err = os.Pipe()
	}
	if err != nil {
		return result, fmt.Errorf("엔진 표준 입력 열기: %w", err)
	}
	defer stdin.Close()
	defer func() {
		if stdinWriter != nil {
			_ = stdinWriter.Close()
		}
	}()

	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		return result, err
	}
	defer stdoutReader.Close()
	defer stdoutWriter.Close()

	stderrReader, stderrWriter, err := os.Pipe()
	if err != nil {
		return result, err
	}
	defer stderrReader.Close()
	defer stderrWriter.Close()

	if err := ctx.Err(); err != nil {
		return result, err
	}

	process, err := createSuspendedProcess(
		request.Executable,
		request.Arguments,
		request.Directory,
		windows.Handle(stdin.Fd()),
		windows.Handle(stdoutWriter.Fd()),
		windows.Handle(stderrWriter.Fd()),
	)
	if err != nil {
		return result, err
	}

	assigned := false
	confirmed := false
	pending := 0

	outputs := make(chan processOutputResult, 2)

	acceptOutput := func(output processOutputResult) {
		pending--

		if output.isStderr {
			result.Stderr = output.text
		} else {
			result.Stdout = output.text
		}
	}

	defer func() {
		if !confirmed {
			runErr = errors.Join(
				runErr,
				stopWindowsProcess(job, process, assigned),
			)
		}

		// 종료 후 남은 출력을 수집한다.
		timer := time.NewTimer(time.Second)
		defer timer.Stop()

		for pending > 0 {
			select {
			case output := <-outputs:
				acceptOutput(output)

				if runErr == nil && output.err != nil {
					runErr = output.err
				}

			case <-timer.C:
				runErr = errors.Join(
					runErr,
					fmt.Errorf("엔진 출력 수집 종료 대기 시간 초과"),
				)
				return
			}
		}
	}()

	// 자식에게 상속이 완료됐으므로 부모가 가진 복사본을 닫는다.
	if err := errors.Join(
		stdin.Close(),
		stdoutWriter.Close(),
		stderrWriter.Close(),
	); err != nil {
		return result, err
	}

	if err := job.assign(process.Process); err != nil {
		return result, err
	}
	assigned = true

	if err := ctx.Err(); err != nil {
		return result, err
	}

	pending = 2

	go func() {
		text, err := readProcessOutput(
			stdoutReader,
			16<<20,
			request.OnStdoutLine,
		)
		outputs <- processOutputResult{
			text: text,
			err:  err,
		}
	}()

	go func() {
		text, err := readProcessOutput(
			stderrReader,
			4<<20,
			nil,
		)
		outputs <- processOutputResult{
			isStderr: true,
			text:     text,
			err:      err,
		}
	}()

	if err := resumeWindowsProcess(process); err != nil {
		return result, err
	}

	if err := closeWindowsHandle(&process.Thread); err != nil {
		return result, err
	}

	var inputDone <-chan error
	if stdinWriter != nil {
		writer := stdinWriter
		stdinWriter = nil // 여기부터 쓰기 핸들은 고루틴이 닫는다.

		done := make(chan error, 1)
		inputDone = done
		go func() {
			_, writeErr := writer.WriteString(request.Stdin)
			done <- errors.Join(writeErr, writer.Close())
		}()
	}

	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()

	var exitedAt time.Time

	for {
		if err := ctx.Err(); err != nil {
			return result, err
		}

		if process.Process != 0 {
			status, err := windows.WaitForSingleObject(
				process.Process,
				0,
			)
			if err != nil {
				return result, err
			}

			switch status {
			case windows.WAIT_OBJECT_0:
				var exitCode uint32

				if err := windows.GetExitCodeProcess(
					process.Process,
					&exitCode,
				); err != nil {
					return result, err
				}

				result.ExitCode = int(exitCode)

				if err := closeWindowsHandle(
					&process.Process,
				); err != nil {
					return result, err
				}

				exitedAt = time.Now()

				if result.ExitCode != 0 {
					return result, fmt.Errorf(
						"엔진 종료 코드: %d",
						result.ExitCode,
					)
				}

			case uint32(windows.WAIT_TIMEOUT):
				// 엔진 실행 중

			default:
				return result, fmt.Errorf(
					"예상하지 못한 프로세스 대기 상태: %d",
					status,
				)
			}
		}

		if !exitedAt.IsZero() {
			if pending == 0 && inputDone == nil {
				active, err := job.activeCount()
				if err != nil {
					return result, err
				}

				if active == 0 {
					confirmed = true
					return result, nil
				}
			}

			if time.Since(exitedAt) >= processStopTimeout {
				return result, fmt.Errorf(
					"엔진 종료 후 자식 프로세스 또는 출력 대기 시간 초과",
				)
			}
		}

		select {
		case output := <-outputs:
			acceptOutput(output)

			if output.err != nil {
				return result, fmt.Errorf(
					"엔진 출력 수집: %w",
					output.err,
				)
			}

		case <-ctx.Done():
			return result, ctx.Err()

		case <-ticker.C:

		case err := <-inputDone:
			inputDone = nil
			if err != nil {
				return result, fmt.Errorf("엔진 표준 입력 쓰기: %w", err)
			}
		}
	}
}
