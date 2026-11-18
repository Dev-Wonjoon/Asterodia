package download

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf16"

	"github.com/google/uuid"
)

type Storage struct {
	Root string
}

var stagingJSONEscapes = strings.NewReplacer(
	`\\`, `\\`,
	`\u2028`, "\u2028",
	`\u2029`, "\u2029",
)

func (s Storage) PrepareStage(jobID, attemptID string) (string, error) {
	if !filepath.IsAbs(s.Root) {
		return "", fmt.Errorf("다운로드 저장소는 절대 경로여야 합니다")
	}
	if strings.TrimSpace(jobID) == "" || strings.TrimSpace(attemptID) == "" {
		return "", fmt.Errorf("작업 ID와 시도 ID가 필요합니다")
	}

	owner := stageOwner(jobID, attemptID)

	base, err := filepath.EvalSymlinks(s.Root)
	if err != nil {
		return "", fmt.Errorf("다운로드 저장소 경로 확인 실패: %w", err)
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		return "", fmt.Errorf("다운로드 저장소 열기 실패: %w", err)
	}
	defer root.Close()

	parent := filepath.Join(".storagebucket", "staging")
	for _, dir := range []string{".storagebucket", parent} {
		if err := root.Mkdir(dir, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return "", fmt.Errorf("작업 폴더의 상위 디렉터리 생성 실패: %w", err)
		}
		info, err := root.Lstat(dir)
		if err != nil {
			return "", fmt.Errorf("작업 폴더의 상위 디렉터리 확인 실패: %w", err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("작업 폴더의 상위 경로가 일반 디렉터리여야 합니다: %s", dir)
		}
	}

	relative := filepath.Join(parent, owner)
	if err := root.Mkdir(relative, 0o700); err != nil {
		return "", fmt.Errorf("시도별 작업 폴더 생성 실패: %w", err)
	}
	return filepath.Join(base, relative), nil
}

func stageOwner(jobID, attemptID string) string {
	var payload bytes.Buffer
	encoder := json.NewEncoder(&payload)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode([2]string{jobID, attemptID})
	encoded := stagingJSONEscapes.Replace(strings.TrimSuffix(payload.String(), "\n"))
	return fmt.Sprintf("%x", sha256.Sum256([]byte(encoded)))
}

func stageItemDir(stageDir string, position int) string {
	return filepath.Join(stageDir, fmt.Sprintf("item-%04d", position+1))
}

func prepareStageItem(stageDir string, position int) (string, error) {
	dir := stageItemDir(stageDir, position)
	if err := os.Mkdir(dir, 0o700); err != nil {
		return "", fmt.Errorf("대상 작업 폴더 생성 실패: %w", err)
	}
	return dir, nil
}

func verifyStagedFile(stageDir string, position int, path string) error {
	itemDir := stageItemDir(stageDir, position)
	relative, err := filepath.Rel(itemDir, path)
	if err != nil || !filepath.IsAbs(path) ||
		relative == "." || relative == ".." || filepath.Base(relative) != relative {
		return fmt.Errorf("저장 파일이 해당 대상의 작업 폴더 바로 아래에 있어야 합니다")
	}

	resolved, err := filepath.EvalSymlinks(itemDir)
	if err != nil {
		return fmt.Errorf("대상 작업 폴더 확인 실패: %w", err)
	}
	sameDir, err := filepath.Rel(itemDir, resolved)
	if err != nil || sameDir != "." {
		return fmt.Errorf("대상 작업 폴더의 실제 경로가 변경됐습니다")
	}

	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("저장 파일 확인 실패: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 {
		return fmt.Errorf("저장 결과는 크기 0 초과의 일반 파일이어야 합니다")
	}
	return nil
}

type publicationEntry struct {
	Path     string `json:"path"`
	Identity string `json:"identity"`
	Hash     string `json:"sha256"`
	Size     int64  `json:"size"`
}
type publicationRecord struct {
	Job     string             `json:"job"`
	Attempt string             `json:"attempt"`
	Files   []publicationEntry `json:"files"`
}

var fileHashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func pathComponent(value, fallback string, limit int) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}
		return r
	}, value)
	value = strings.Trim(strings.Join(strings.Fields(value), " "), " .")
	if value == "" {
		value = fallback
	}
	var out strings.Builder
	units := 0
	for _, r := range value {
		n := utf16.RuneLen(r)
		if n < 0 {
			n = 1
		}
		if units+n > limit {
			break
		}
		out.WriteRune(r)
		units += n
	}
	value = strings.TrimRight(out.String(), " .")
	if value == "" {
		value = fallback
	}
	base, _, _ := strings.Cut(strings.ToUpper(value), ".")
	base = strings.TrimSpace(base)
	characters := []rune(base)
	if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" || base == "CONIN$" || base == "CONOUT$" || (len(characters) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && strings.ContainsRune("123456789¹²³", characters[3])) {
		value = "_" + value
	}
	return value
}

func publicationName(file verifiedFile, suffix int) string {
	site := strings.ToLower(file.metadata.PlatformName)
	if site == "" || site == "generic" || site == "html5mediaembed" {
		if page, e := url.Parse(file.source); e == nil {
			site = strings.ToLower(page.Hostname())
		}
	}
	owner := pathComponent(file.metadata.OwnerID, "unknown", 64)
	title := pathComponent(file.metadata.Title, pathComponent(file.metadata.ExternalID, "untitled", 100), 120)
	if suffix > 1 {
		title += fmt.Sprintf(" (%d)", suffix)
	}
	ext := strings.ToLower(filepath.Ext(file.final))
	return filepath.Join(pathComponent(site, "unknown", 60), owner, title+ext)
}

func publicationPath(job, attempt string) string {
	_, dir := workPaths(job, attempt)
	return filepath.Join(dir, "publication.json")
}

func checkPublicationParents(root *os.Root, path string, create bool) error {
	if !filepath.IsLocal(path) || filepath.IsAbs(path) {
		return fmt.Errorf("저장소 내부 상대 경로가 필요합니다")
	}
	parts := strings.Split(filepath.Clean(path), string(filepath.Separator))
	for i := 1; i < len(parts); i++ {
		dir := filepath.Join(parts[:i]...)
		if create {
			if e := root.Mkdir(dir, 0700); e != nil && !errors.Is(e, os.ErrExist) {
				return e
			}
		}
		info, e := root.Lstat(dir)
		if e != nil {
			return e
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("저장 경로가 일반 폴더가 아닙니다: %s", dir)
		}
	}
	return nil
}

func writePublication(root *os.Root, record publicationRecord) error {
	path := publicationPath(record.Job, record.Attempt)
	if e := checkPublicationParents(root, path, false); e != nil {
		return e
	}
	temp := filepath.Join(filepath.Dir(path), "publication-"+uuid.NewString()+".pending")
	f, e := root.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	defer root.Remove(temp)
	e = json.NewEncoder(f).Encode(record)
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e != nil {
		return e
	}
	if closeErr != nil {
		return closeErr
	}
	return root.Rename(temp, path)
}

func readPublication(root *os.Root, job, attempt string) (publicationRecord, error) {
	var record publicationRecord
	path := publicationPath(job, attempt)
	if e := checkPublicationParents(root, path, false); e != nil {
		return record, e
	}
	info, e := root.Lstat(path)
	if e != nil {
		return record, e
	}
	if !info.Mode().IsRegular() || info.Size() > 8*1024*1024 {
		return record, fmt.Errorf("파일 배치 소유권 기록을 확인할 수 없습니다")
	}
	f, e := root.Open(path)
	if e != nil {
		return record, e
	}
	defer f.Close()
	if e = json.NewDecoder(f).Decode(&record); e != nil {
		return record, e
	}
	if record.Job != job || record.Attempt != attempt || len(record.Files) == 0 || len(record.Files) > 5000 {
		return record, fmt.Errorf("다른 시도의 파일 배치 기록입니다")
	}
	for _, entry := range record.Files {
		parts := strings.Split(entry.Path, string(filepath.Separator))
		if !filepath.IsLocal(entry.Path) || filepath.Clean(entry.Path) != entry.Path || len(parts) != 3 || strings.EqualFold(parts[0], ".storagebucket") || entry.Identity == "" || !fileHashPattern.MatchString(entry.Hash) || entry.Size <= 0 {
			return record, fmt.Errorf("파일 배치 기록이 올바르지 않습니다")
		}
	}
	return record, nil
}

// publish uses exclusive, handle-based renames. The durable record is written
// before every move, so a crash can reclaim only files from this exact attempt.
func (s Storage) publish(root *os.Root, job, attempt string, files []verifiedFile) ([]verifiedFile, error) {
	record := publicationRecord{Job: job, Attempt: attempt}
	for i := range files {
		file := &files[i]
		rel, e := filepath.Rel(root.Name(), file.final)
		if e != nil || !filepath.IsLocal(rel) {
			return nil, fmt.Errorf("검증된 파일이 저장소 밖에 있습니다")
		}
		if e = checkPublicationParents(root, rel, false); e != nil {
			return nil, e
		}
		source, e := openPublicationFile(root, rel)
		if e != nil {
			return nil, e
		}
		identity, e := publicationIdentity(source)
		if e != nil {
			source.Close()
			return nil, e
		}
		entry := publicationEntry{Identity: identity, Hash: file.hash, Size: file.size}
		if e = verifyPublicationHandle(source, entry); e != nil {
			source.Close()
			return nil, e
		}
		record.Files = append(record.Files, entry)
		moved := false
		for suffix := 1; suffix <= 10000; suffix++ {
			path := publicationName(*file, suffix)
			if e = checkPublicationParents(root, path, true); e != nil {
				break
			}
			if _, e = root.Lstat(path); e == nil {
				continue
			} else if !os.IsNotExist(e) {
				break
			}
			record.Files[i].Path = path
			if e = writePublication(root, record); e != nil {
				break
			}
			e = renamePublicationFile(root, source, path)
			if errors.Is(e, os.ErrExist) {
				continue
			}
			if e != nil {
				break
			}
			file.final = filepath.Join(root.Name(), path)
			moved = true
			break
		}
		source.Close()
		if !moved {
			if e != nil {
				return nil, fmt.Errorf("최종 파일 배치 실패: %w", e)
			}
			return nil, fmt.Errorf("같은 이름의 파일이 너무 많습니다")
		}
	}
	return files, nil
}

func (s Storage) removePublished(root *os.Root, job, attempt string, only string) error {
	record, e := readPublication(root, job, attempt)
	if os.IsNotExist(e) {
		if only != "" {
			return fmt.Errorf("파일 배치 소유권 기록이 없습니다")
		}
		return nil
	}
	if e != nil {
		return e
	}
	matched := false
	for _, entry := range record.Files {
		if only != "" && !strings.EqualFold(filepath.Join(root.Name(), entry.Path), only) {
			continue
		}
		matched = true
		if e = checkPublicationParents(root, entry.Path, false); os.IsNotExist(e) {
			continue
		} else if e != nil {
			return e
		}
		f, e := openPublicationFile(root, entry.Path)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return e
		}
		e = verifyPublicationHandle(f, entry)
		if e == nil {
			e = deletePublicationFile(f)
		}
		f.Close()
		if e != nil {
			return e
		}
	}
	if only != "" && !matched {
		return fmt.Errorf("해당 파일의 배치 소유권이 없습니다")
	}
	return nil
}

// Recheck the public names and keep their contents and identities stable until
// the caller finishes its DB commit. Closing these handles releases the locks.
func holdPublished(root *os.Root, job, attempt string, files []verifiedFile) (func(), error) {
	handles := []*os.File{}
	release := func() {
		for _, handle := range handles {
			handle.Close()
		}
	}
	record, err := readPublication(root, job, attempt)
	if err != nil {
		return nil, err
	}
	if len(record.Files) != len(files) {
		return nil, fmt.Errorf("최종 파일 배치 기록 수가 다릅니다")
	}
	for i, entry := range record.Files {
		if !strings.EqualFold(filepath.Join(root.Name(), entry.Path), files[i].final) || entry.Hash != files[i].hash || entry.Size != files[i].size {
			release()
			return nil, fmt.Errorf("최종 파일 배치 기록이 변경되었습니다")
		}
		if err = checkPublicationParents(root, entry.Path, false); err != nil {
			release()
			return nil, err
		}
		f, err := openPublicationFile(root, entry.Path)
		if err != nil {
			release()
			return nil, err
		}
		handles = append(handles, f)
		if err = verifyPublicationHandle(f, entry); err != nil {
			release()
			return nil, err
		}
	}
	return release, nil
}

// RemovePublishedFile is called only after the caller has ruled out live DB
// references. Both the Windows file identity and content must match our record.
func (s Storage) RemovePublishedFile(job, attempt, path string) error {
	if !filepath.IsAbs(s.Root) || !filepath.IsAbs(path) {
		return fmt.Errorf("절대 경로가 필요합니다")
	}
	base, e := filepath.EvalSymlinks(s.Root)
	if e != nil {
		return e
	}
	rel, e := filepath.Rel(base, path)
	if e != nil || !filepath.IsLocal(rel) {
		return fmt.Errorf("정리 대상이 저장소 밖에 있습니다")
	}
	root, e := os.OpenRoot(base)
	if e != nil {
		return e
	}
	defer root.Close()
	return s.removePublished(root, job, attempt, filepath.Join(base, rel))
}
