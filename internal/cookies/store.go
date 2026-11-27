package cookies

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

func filepathAbsolute(path string) bool { return filepath.IsAbs(path) }

type diskState struct {
	Version  int               `json:"version"`
	Records  map[string]Record `json:"records"`
	Disabled map[string]bool   `json:"disabled"`
	Removed  map[string]bool   `json:"removed"`
	Epoch    map[string]uint64 `json:"epoch"`
	Imported bool              `json:"imported"`
}

type Store struct {
	mu       sync.Mutex
	filename string
	state    diskState
	blocked  bool
}

// Unavailable preserves an unreadable vault while leaving unrelated app
// features usable. Mutations cannot overwrite the original encrypted file.
func Unavailable(filename string) *Store {
	return &Store{filename: filename, blocked: true, state: diskState{Version: 1, Records: map[string]Record{}, Disabled: map[string]bool{}, Removed: map[string]bool{}, Epoch: map[string]uint64{}}}
}

// Entry contains display metadata only. Cookie values never cross the UI API.
type Entry struct {
	ID         string `json:"id"`
	Domain     string `json:"domain"`
	Subdomains bool   `json:"subdomains"`
	Path       string `json:"path"`
	Name       string `json:"name"`
	Secure     bool   `json:"secure"`
	HTTPOnly   bool   `json:"httpOnly"`
	Expires    int64  `json:"expires"`
	Expired    bool   `json:"expired"`
	Source     string `json:"source"`
}
type Group struct {
	Site    string  `json:"site"`
	Count   int     `json:"count"`
	Active  int     `json:"active"`
	Enabled bool    `json:"enabled"`
	Entries []Entry `json:"entries"`
}
type Snapshot struct {
	Site     string
	Sites    []string
	Epoch    map[string]uint64
	Records  []Record
	Removed  map[string]bool
	Enabled  bool
	Imported bool
	All      bool
	Disabled map[string]bool
}

func Open(filename string) (*Store, error) {
	if !filepath.IsAbs(filename) {
		return nil, fmt.Errorf("쿠키 저장소 절대 경로가 필요합니다")
	}
	s := &Store{filename: filename, state: diskState{Version: 1, Records: map[string]Record{}, Disabled: map[string]bool{}, Removed: map[string]bool{}, Epoch: map[string]uint64{}}}
	sealed, err := os.ReadFile(filename)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("쿠키 저장소를 읽지 못했습니다")
	}
	if len(sealed) > 64<<20 {
		return nil, fmt.Errorf("쿠키 저장소가 너무 큽니다")
	}
	body, err := unprotect(sealed)
	if err != nil {
		return nil, fmt.Errorf("쿠키 저장소를 복호화하지 못했습니다")
	}
	if json.Unmarshal(body, &s.state) != nil || s.state.Version != 1 || s.state.Records == nil || s.state.Disabled == nil || s.state.Removed == nil || s.state.Epoch == nil {
		return nil, fmt.Errorf("쿠키 저장소 형식이 올바르지 않습니다")
	}
	return s, nil
}

func (s *Store) change(edit func(*diskState) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.blocked {
		return fmt.Errorf("쿠키 저장소를 열지 못했습니다. 원래 Windows 계정과 보관 파일을 확인하세요")
	}
	encoded, _ := json.Marshal(s.state)
	var next diskState
	_ = json.Unmarshal(encoded, &next)
	if err := edit(&next); err != nil {
		return err
	}
	body, err := json.Marshal(next)
	if err != nil {
		return err
	}
	sealed, err := protect(body)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.filename), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.filename), "cookies-*.tmp")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(sealed)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(name, s.filename); err != nil {
		return err
	}
	s.state = next
	return nil
}

func (s *Store) Imported() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.state.Imported }

func (s *Store) ImportFile(filename string) error {
	records, err := ReadFile(filename)
	if err != nil {
		return err
	}
	if len(records) == 0 {
		return fmt.Errorf("가져올 쿠키가 없습니다")
	}
	return s.change(func(state *diskState) error {
		sites := map[string]bool{}
		for _, r := range records {
			sites[groupSite(r.Domain)] = true
		}
		// Import replaces just the domains represented in this file's site groups.
		for id, r := range state.Records {
			if sites[groupSite(r.Domain)] {
				delete(state.Records, id)
			}
		}
		for _, r := range records {
			r.Source = "file:" + filepath.Base(filename)
			r.UpdatedAt = time.Now().Unix()
			state.Records[r.ID()] = r
			delete(state.Removed, r.ID())
		}
		for site := range sites {
			delete(state.Disabled, site)
			state.Epoch[site]++
		}
		state.Imported = true
		return nil
	})
}

// Alternate service domains share management policy, while each record keeps
// its original domain for HTTP cookie matching.
func groupSite(raw string) string {
	site := Site(raw)
	switch site {
	case "youtu.be", "googlevideo.com", "ytimg.com":
		return "youtube.com"
	case "twitter.com", "t.co", "twimg.com":
		return "x.com"
	case "pin.it", "pinimg.com":
		return "pinterest.com"
	default:
		return site
	}
}

func canonicalSite(raw string) (string, error) {
	domain, err := Domain(raw)
	if err != nil {
		return "", err
	}
	site := groupSite(domain)
	if site == "" {
		return "", fmt.Errorf("사이트 주소가 올바르지 않습니다")
	}
	return site, nil
}

func (s *Store) SetEnabled(raw string, enabled bool) error {
	site, err := canonicalSite(raw)
	if err != nil {
		return err
	}
	return s.change(func(state *diskState) error {
		if enabled {
			delete(state.Disabled, site)
		} else {
			state.Disabled[site] = true
		}
		state.Epoch[site]++
		return nil
	})
}
func (s *Store) DeleteSite(raw string) error {
	site, err := canonicalSite(raw)
	if err != nil {
		return err
	}
	return s.change(func(state *diskState) error {
		for id, r := range state.Records {
			if groupSite(r.Domain) == site {
				delete(state.Records, id)
			}
		}
		state.Disabled[site] = true
		state.Epoch[site]++
		return nil
	})
}
func (s *Store) Delete(id string) error {
	return s.change(func(state *diskState) error {
		r, ok := state.Records[id]
		if !ok {
			return fmt.Errorf("쿠키 항목을 찾을 수 없습니다")
		}
		delete(state.Records, id)
		state.Removed[id] = true
		state.Epoch[groupSite(r.Domain)]++
		return nil
	})
}
func (s *Store) ClearImported() error {
	return s.change(func(state *diskState) error {
		for id, r := range state.Records {
			if strings.HasPrefix(r.Source, "file:") {
				delete(state.Records, id)
				state.Removed[id] = true
				state.Epoch[groupSite(r.Domain)]++
			}
		}
		return nil
	})
}

func (s *Store) Groups() []Group {
	s.mu.Lock()
	defer s.mu.Unlock()
	groups := map[string]*Group{}
	get := func(site string) *Group {
		if groups[site] == nil {
			groups[site] = &Group{Site: site, Enabled: !s.state.Disabled[site], Entries: []Entry{}}
		}
		return groups[site]
	}
	for site, disabled := range s.state.Disabled {
		if disabled {
			get(site)
		}
	}
	for id, r := range s.state.Records {
		g := get(groupSite(r.Domain))
		g.Count++
		if r.Active() {
			g.Active++
		}
		g.Entries = append(g.Entries, Entry{ID: id, Domain: r.Domain, Subdomains: r.Subdomains, Path: r.Path, Name: r.Name, Secure: r.Secure, HTTPOnly: r.HTTPOnly, Expires: r.Expires, Expired: !r.Active(), Source: r.Source})
	}
	result := make([]Group, 0, len(groups))
	for _, g := range groups {
		sort.Slice(g.Entries, func(i, j int) bool {
			a, b := g.Entries[i], g.Entries[j]
			return a.Domain+"\x00"+a.Path+"\x00"+a.Name < b.Domain+"\x00"+b.Path+"\x00"+b.Name
		})
		result = append(result, *g)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Site < result[j].Site })
	return result
}

func relatedSites(site string) []string {
	switch site {
	case "youtube.com", "youtu.be":
		return []string{"youtube.com", "youtu.be", "google.com", "googlevideo.com", "ytimg.com"}
	case "x.com", "twitter.com", "t.co":
		return []string{"x.com", "twitter.com", "t.co", "twimg.com"}
	case "pinterest.com", "pin.it", "pinimg.com":
		return []string{"pinterest.com", "pin.it", "pinimg.com"}
	default:
		return []string{site}
	}
}

func (s *Store) Snapshot(raw string) Snapshot {
	return s.snapshot(raw, false)
}

// TaskSnapshot retains a complete enabled jar for generic pages that can
// delegate to another site's extractor. CookieJar still scopes every HTTP
// request. Known site extractors use their explicit related-domain groups.
func (s *Store) TaskSnapshot(raw string) Snapshot { return s.snapshot(raw, true) }

func (s *Store) snapshot(raw string, genericFallback bool) Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	site := groupSite(raw)
	snap := Snapshot{Site: site, Sites: []string{}, Epoch: map[string]uint64{}, Removed: map[string]bool{}, Disabled: map[string]bool{}, Enabled: !s.blocked && site != "" && !s.state.Disabled[site]}
	sites := relatedSites(site)
	strict := site == "berriz.in" || site == "instagram.com" || site == "threads.com" || site == "threads.net" || len(sites) > 1
	if genericFallback && !strict {
		snap.All = true
		seen := map[string]bool{site: true}
		for known := range s.state.Epoch {
			seen[known] = true
		}
		for _, r := range s.state.Records {
			seen[groupSite(r.Domain)] = true
		}
		sites = nil
		for known := range seen {
			sites = append(sites, known)
		}
	}
	for blocked, disabled := range s.state.Disabled {
		snap.Disabled[blocked] = disabled
	}
	allowed := map[string]bool{}
	for _, related := range sites {
		snap.Epoch[related] = s.state.Epoch[related]
		if !s.state.Disabled[related] {
			allowed[related] = true
			snap.Sites = append(snap.Sites, related)
		}
	}
	for id := range s.state.Removed {
		snap.Removed[id] = true
	}
	if snap.Enabled {
		for _, r := range s.state.Records {
			if allowed[groupSite(r.Domain)] && r.Active() {
				snap.Records = append(snap.Records, r)
				if strings.HasPrefix(r.Source, "file:") {
					snap.Imported = true
				}
			}
		}
	}
	return snap
}

func (snap Snapshot) Filter(records []Record) []Record {
	if !snap.Enabled {
		return nil
	}
	allowed := map[string]bool{}
	for _, site := range snap.Sites {
		allowed[site] = true
	}
	var result []Record
	for _, r := range records {
		if (allowed[groupSite(r.Domain)] || snap.All) && !snap.Disabled[groupSite(r.Domain)] && !snap.Removed[r.ID()] && r.Active() {
			result = append(result, r)
		}
	}
	return result
}

// Merge accepts only a successful task's selected groups, provided no manual
// change or earlier task has invalidated that group's snapshot in the meantime.
func (s *Store) Merge(snap Snapshot, records []Record) error {
	if !snap.Enabled {
		return nil
	}
	records = snap.Filter(records)
	if snap.All {
		seen := map[string]bool{}
		for _, site := range snap.Sites {
			seen[site] = true
		}
		for _, r := range records {
			site := groupSite(r.Domain)
			if !seen[site] {
				snap.Sites = append(snap.Sites, site)
				seen[site] = true
			}
		}
	}
	return s.change(func(state *diskState) error {
		if state.Disabled[snap.Site] || state.Epoch[snap.Site] != snap.Epoch[snap.Site] {
			return nil
		}
		original := map[string]Record{}
		origins := map[string]string{}
		for _, r := range snap.Records {
			original[r.ID()] = r
			if site := groupSite(r.Domain); origins[site] == "" || strings.HasPrefix(r.Source, "file:") {
				origins[site] = r.Source
			}
		}
		for _, site := range snap.Sites {
			if state.Disabled[site] || state.Epoch[site] != snap.Epoch[site] {
				continue
			}
			for id, r := range state.Records {
				if groupSite(r.Domain) == site {
					delete(state.Records, id)
				}
			}
			for _, r := range records {
				if groupSite(r.Domain) != site || state.Removed[r.ID()] {
					continue
				}
				if old, ok := original[r.ID()]; ok {
					r.Source = old.Source
					// Some CLI cookie writers omit the HttpOnly marker.
					r.HTTPOnly = r.HTTPOnly || old.HTTPOnly
				}
				if r.Source == "" {
					r.Source = origins[site]
					if r.Source == "" {
						r.Source = "browser"
					}
				}
				r.UpdatedAt = time.Now().Unix()
				state.Records[r.ID()] = r
			}
			state.Epoch[site]++
		}
		return nil
	})
}
