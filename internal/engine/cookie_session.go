package engine

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"

	"github.com/Dev-Wonjoon/Asterodia/internal/cookies"
)

type ManagedAuth struct {
	Store    *cookies.Store
	Browser  string
	Exporter *Ytdlp
}

func (a *ManagedAuth) HasCookies(raw string) bool {
	if a == nil || a.Store == nil {
		return false
	}
	snap := a.Store.Snapshot(raw)
	return snap.Enabled && (len(snap.Records) > 0 || a.Browser != "")
}

func (a *ManagedAuth) prepare(ctx context.Context, raw, dir string) ([]string, func(bool) error, error) {
	noop := func(bool) error { return nil }
	if err := ctx.Err(); err != nil {
		return nil, noop, err
	}
	if a.Store == nil {
		return nil, noop, fmt.Errorf("쿠키 저장소가 연결되지 않았습니다")
	}
	snap := a.Store.TaskSnapshot(raw)
	if !snap.Enabled {
		return nil, noop, nil
	}
	records := snap.Records
	if a.Browser != "" && !snap.Imported {
		if a.Exporter == nil {
			return nil, noop, fmt.Errorf("브라우저 쿠키를 읽을 yt-dlp 엔진이 필요합니다")
		}
		exporter := *a.Exporter
		exporter.CookieBrowser = a.Browser
		exporter.CookieFile = ""
		exporter.Auth = nil
		fresh, err := exporter.browserCookies(ctx, dir)
		if err != nil {
			return nil, noop, err
		}
		// The latest browser state replaces the previous browser snapshot.
		records = snap.Filter(fresh)
		for i := range records {
			records[i].Source = "browser:" + a.Browser
		}
	}
	if !filepath.IsAbs(dir) {
		return nil, noop, fmt.Errorf("인증 작업 폴더는 절대 경로여야 합니다")
	}
	filename := filepath.Join(dir, "cookies.txt")
	f, err := os.OpenFile(filename, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, noop, err
	}
	_, writeErr := f.Write(cookies.Encode(records))
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(filename)
		if writeErr != nil {
			return nil, noop, writeErr
		}
		return nil, noop, closeErr
	}
	snap.Records = records
	var once sync.Once
	var result error
	finish := func(success bool) error {
		once.Do(func() {
			defer os.Remove(filename)
			if !success {
				return
			}
			updated, err := cookies.ReadFile(filename)
			if err != nil {
				result = err
				return
			}
			result = a.Store.Merge(snap, updated)
		})
		return result
	}
	return []string{"--cookies", filename}, finish, nil
}

func prepareAuthentication(ctx context.Context, managed *ManagedAuth, browser, file, address, dir string) ([]string, func(bool) error, error) {
	if managed != nil {
		return managed.prepare(ctx, address, dir)
	}
	args, err := authArguments(browser, file, dir)
	var once sync.Once
	var result error
	finish := func(bool) error {
		once.Do(func() {
			if file != "" {
				result = os.Remove(filepath.Join(dir, "cookies.txt"))
				if os.IsNotExist(result) {
					result = nil
				}
			}
		})
		return result
	}
	return args, finish, err
}

func (a *ManagedAuth) forURL(ctx context.Context, address *url.URL, dir string) ([]*http.Cookie, error) {
	args, finish, err := a.prepare(ctx, address.String(), dir)
	if err != nil {
		return nil, err
	}
	defer finish(false)
	if len(args) == 0 {
		return nil, ErrAuthenticationRequired
	}
	records, err := cookies.ReadFile(args[1])
	if err != nil {
		return nil, err
	}
	selected := cookies.ForURL(records, address)
	if err := finish(true); err != nil {
		return nil, err
	}
	if len(selected) == 0 {
		return nil, ErrAuthenticationRequired
	}
	return selected, nil
}
