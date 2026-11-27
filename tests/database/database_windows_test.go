package database_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Dev-Wonjoon/Asterodia/internal/database"
)

func TestOpenExisting(t *testing.T) {
	configured := os.Getenv("ASTERODIA_TEST_ROOT")
	if !filepath.IsAbs(configured) {
		t.Fatal("ASTERODIA_TEST_ROOT에 절대 경로를 지정하세요")
	}
	root, err := filepath.EvalSymlinks(configured)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	repo, err = filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	category := filepath.Join(repo, ".test-data", "database-open")
	relative, err := filepath.Rel(category, root)
	if err != nil || relative == "." || relative == ".." ||
		strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		t.Fatal("실행별 테스트 폴더는 .test-data/database-open 아래에 있어야 합니다")
	}

	t.Run("v5-connection", func(t *testing.T) {
		ctx := t.Context()
		filename := filepath.Join(root, "fixture #한글.sqlite")
		createSQLiteFixture(t, filename, "PRAGMA user_version = 14",
			"CREATE TABLE parent (id INTEGER PRIMARY KEY)",
			"CREATE TABLE child (parent_id INTEGER REFERENCES parent(id))",
			"INSERT INTO parent (id) VALUES (1)",
		)

		db, err := database.OpenExisting(ctx, filename)
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()

		var timeout int
		if err := db.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&timeout); err != nil {
			t.Fatal(err)
		}
		if timeout != 5000 {
			t.Fatalf("busy_timeout=%d, 기대값=5000", timeout)
		}

		if _, err := db.ExecContext(ctx, "INSERT INTO child (parent_id) VALUES (1)"); err != nil {
			t.Fatalf("기존 부모 행을 참조하는 쓰기 실패: %v", err)
		}
		if _, err := db.ExecContext(ctx, "INSERT INTO child (parent_id) VALUES (999)"); err == nil {
			t.Fatal("없는 부모를 참조하면 외래 키 제약으로 실패해야 합니다")
		}
	})

	t.Run("missing-file", func(t *testing.T) {
		filename := filepath.Join(root, "missing.sqlite")
		if db, err := database.OpenExisting(t.Context(), filename); err == nil {
			db.Close()
			t.Fatal("존재하지 않는 DB 파일을 열었습니다")
		}
		if _, err := os.Stat(filename); !os.IsNotExist(err) {
			t.Fatalf("누락된 DB 파일은 생성되지 않아야 합니다: %v", err)
		}
	})

	t.Run("older-version", func(t *testing.T) {
		filename := filepath.Join(root, "v2.sqlite")
		createSQLiteFixture(t, filename, "PRAGMA user_version = 2")

		if db, err := database.OpenExisting(t.Context(), filename); err == nil {
			db.Close()
			t.Fatal("v2 파일을 v3 연결로 열었습니다")
		}
	})
}

func createSQLiteFixture(t *testing.T, filename string, statements ...string) {
	t.Helper()

	db, err := sql.Open("sqlite", filename)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	for _, statement := range statements {
		if _, err := db.ExecContext(t.Context(), statement); err != nil {
			t.Fatal(err)
		}
	}
}
