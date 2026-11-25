package engine_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/Dev-Wonjoon/StorageBucket/internal/engine"
	"golang.org/x/sys/windows"
)

func TestImageEngineInstallation(t *testing.T) {
	if os.Getenv("STORAGEBUCKET_IMAGE_INSTALL_TEST") != "1" {
		t.Skip("공식 배포본 다운로드 검증은 명시적으로 실행합니다")
	}
	root := engineTestRoot(t, "engine-parsers")
	bin := filepath.Join(root, "image-install")
	engine.ConfigureIntegrityRoot(bin)
	t.Cleanup(func() { engine.ConfigureIntegrityRoot("") })
	var phases []string
	manager, err := engine.NewManager(bin, func(progress engine.InstallProgress) { phases = append(phases, progress.Phase) })
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := manager.Install(t.Context(), "imagemagick", "fixture-install"); err != nil || !ok {
		t.Fatalf("공식 이미지 엔진 설치: %v", err)
	}
	status, err := manager.Status(t.Context())
	if err != nil || !status["imagemagick"].Installed {
		t.Fatalf("설치 엔진 상태: %v %+v", err, status)
	}
	if !strings.Contains(strings.Join(phases, ","), "extracting,dependencies,verifying,committing,completed") {
		t.Fatal("runtime preparation did not precede execution verification", phases)
	}
	imageDirectory := filepath.Join(bin, "imagemagick")
	for _, name := range []string{"vcomp140.dll", "vcomp140-LICENSE.txt"} {
		info, err := os.Stat(filepath.Join(imageDirectory, name))
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			t.Fatal("local runtime or notice missing", name, err)
		}
	}
	assertImageUsesLocalRuntime(t, imageDirectory, root)
	if ok, err := manager.Install(t.Context(), "imagemagick", "fixture-reuse"); err != nil || !ok {
		t.Fatalf("기존 엔진 재사용: %v", err)
	}
	// Even a PC with a system runtime must reject and repair a changed local DLL.
	if err := os.WriteFile(filepath.Join(imageDirectory, "vcomp140.dll"), []byte("tampered-dll"), 0600); err != nil {
		t.Fatal(err)
	}
	status, err = manager.Status(t.Context())
	if err != nil || status["imagemagick"].Installed || !strings.Contains(status["imagemagick"].Error, "런타임") {
		t.Fatal("changed local runtime accepted", status, err)
	}
	if ok, err := manager.Install(t.Context(), "imagemagick", "fixture-repair"); err != nil || !ok {
		t.Fatal("one-click local runtime repair failed", err)
	}
	// The same official archive must install on PCs without Windows tar.
	// Use Unicode and spaces as real user profile / download paths often do.
	t.Setenv("SystemRoot", filepath.Join(root, "no-windows-tools"))
	portableRoot := filepath.Join(root, "다른 PC 설치", "bin")
	engine.ConfigureIntegrityRoot(portableRoot)
	portable, err := engine.NewManager(portableRoot, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := portable.Install(t.Context(), "imagemagick", "without-tar"); err != nil || !ok {
		t.Fatalf("Windows 압축 도구 없이 설치: %v", err)
	}
	status, err = portable.Status(t.Context())
	if err != nil || !status["imagemagick"].Installed {
		t.Fatalf("다른 PC 경로 엔진 상태: %v %+v", err, status)
	}
	version := status["imagemagick"].Version
	if version == nil || !strings.HasPrefix(*version, "Version: ImageMagick 7.1.2-34 ") {
		t.Fatalf("공식 엔진 버전 불일치: %v", version)
	}
}

func assertImageUsesLocalRuntime(t *testing.T, directory, work string) {
	t.Helper()
	// Keep magick waiting for image input so its actual loaded DLL paths can
	// be inspected. This rules out a false pass from System32's runtime.
	cmd := exec.Command(filepath.Join(directory, "magick.exe"), "png:-", "webp:"+filepath.Join(work, "probe.webp"))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		input.Close()
		t.Fatal(err)
	}
	defer func() { input.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_INFORMATION|windows.PROCESS_VM_READ, false, uint32(cmd.Process.Pid))
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(process)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var modules [256]windows.Handle
		var needed uint32
		if windows.EnumProcessModules(process, &modules[0], uint32(unsafe.Sizeof(modules)), &needed) == nil {
			count := min(int(needed)/int(unsafe.Sizeof(modules[0])), len(modules))
			for _, module := range modules[:count] {
				var path [32768]uint16
				if windows.GetModuleFileNameEx(process, module, &path[0], uint32(len(path))) == nil {
					loaded := windows.UTF16ToString(path[:])
					if strings.EqualFold(loaded, filepath.Join(directory, "vcomp140.dll")) {
						return
					}
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("ImageMagick did not load the app-local runtime", stderr.String())
}
