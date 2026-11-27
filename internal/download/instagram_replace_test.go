package download

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Dev-Wonjoon/Asterodia/internal/database"
	"github.com/Dev-Wonjoon/Asterodia/internal/engine"
)

func TestInstagramRegisterAppendAndReplacePreserveReferences(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db, err := database.Open(ctx, filepath.Join(root, "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	service := NewService(db, nil, nil, nil)
	scalar := func(query string, args ...any) int64 {
		t.Helper()
		var value int64
		if e := db.QueryRowContext(ctx, query, args...).Scan(&value); e != nil {
			t.Fatal(e, query)
		}
		return value
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, e := db.ExecContext(ctx, query, args...); e != nil {
			t.Fatal(e, query)
		}
	}
	sequence := 0
	commit := func(post string, replace *int64, keys ...string) int64 {
		t.Helper()
		sequence++
		input := InitialAttemptInput{OriginalURL: "https://www.instagram.com/p/" + post + "/", StorageRoot: root, Options: engine.Options{Type: "video", Quality: "best"}}
		if replace != nil {
			revision := scalar("SELECT revision FROM media WHERE id=?", *replace)
			input.ReplaceMediaID = replace
			input.ExpectedRevision = &revision
		}
		attempt, e := NewStore(db).CreateInitialAttempt(ctx, input)
		if e != nil {
			t.Fatal(e)
		}
		exec("UPDATE download_attempt SET phase='committing' WHERE id=?", attempt.AttemptID)
		width, height := 1, 1
		if err := os.MkdirAll(filepath.Join(root, attempt.AttemptID), 0700); err != nil {
			t.Fatal(err)
		}
		files := []verifiedFile{}
		for i, key := range keys {
			files = append(files, verifiedFile{key: key, source: input.OriginalURL, final: filepath.Join(root, attempt.AttemptID, key+".png"),
				token: "abcdef12", position: i, size: 10, mtime: int64(sequence), hash: strings.Repeat("0", 64), format: "png", width: &width, height: &height,
				metadata: TargetMetadata{Kind: "image", Title: "post " + post, ExternalID: key, PlatformName: "Instagram", OwnerID: "9001", OwnerName: "account"}})
		}
		id, e := service.register(ctx, attempt.JobID, attempt.AttemptID, files)
		if e != nil {
			t.Fatal(e)
		}
		return id
	}
	first := commit("first", nil, "one", "two")
	album := scalar("SELECT media_id FROM instagram_profile_album WHERE owner_id='9001'")
	one := scalar("SELECT id FROM media_file WHERE media_id=? AND file_key='one'", first)
	two := scalar("SELECT id FROM media_file WHERE media_id=? AND file_key='two'", first)
	var uuid string
	if err = db.QueryRow("SELECT file_uuid FROM media_file WHERE id=?", one).Scan(&uuid); err != nil {
		t.Fatal(err)
	}
	second := commit("second", nil, "three")
	exec("INSERT INTO media(source_kind,custom_title,created_at) VALUES('local','user selection',1)")
	user := scalar("SELECT last_insert_rowid()")
	exec("INSERT INTO user_album(media_id,cover_file_id) VALUES(?,?)", user, one)
	exec("INSERT INTO user_album_file(album_id,file_id,position) VALUES(?,?,0),(?,?,1)", user, one, user, two)
	exec("INSERT INTO gallery_pin(file_id,created_at) VALUES(?,1)", one)
	exec("UPDATE user_album SET cover_file_id=? WHERE media_id=?", one, album)
	commit("first", &first, "two", "one", "four")
	if scalar("SELECT id FROM media_file WHERE media_id=? AND file_key='one'", first) != one || scalar("SELECT id FROM media_file WHERE media_id=? AND file_key='two'", first) != two {
		t.Fatal("re-download replaced file identities")
	}
	var after string
	if err = db.QueryRow("SELECT file_uuid FROM media_file WHERE id=?", one).Scan(&after); err != nil || uuid != after {
		t.Fatal("file UUID changed", err)
	}
	if scalar("SELECT count(*) FROM user_album_file WHERE album_id=?", user) != 2 || scalar("SELECT cover_file_id FROM user_album WHERE media_id=?", user) != one || scalar("SELECT count(*) FROM gallery_pin WHERE file_id=?", one) != 1 {
		t.Fatal("user references lost")
	}
	if scalar("SELECT count(*) FROM media_file WHERE media_id=?", second) != 1 || scalar("SELECT count(*) FROM user_album_file WHERE album_id=?", album) != 4 {
		t.Fatal("another post changed, or new files not appended")
	}
	commit("first", &first, "one")
	if scalar("SELECT count(*) FROM media_file WHERE media_id=?", first) != 1 || scalar("SELECT count(*) FROM media_file WHERE media_id=?", second) != 1 || scalar("SELECT count(*) FROM user_album_file WHERE album_id=?", album) != 2 {
		t.Fatal("obsolete files removed outside their post")
	}
	if scalar("SELECT count(*) FROM user_album_file WHERE album_id=? AND file_id=?", user, one) != 1 || scalar("SELECT count(*) FROM pragma_foreign_key_check") != 0 {
		t.Fatal("broken references")
	}
}
