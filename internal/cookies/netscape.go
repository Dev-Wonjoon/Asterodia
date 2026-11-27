package cookies

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/idna"
	"golang.org/x/net/publicsuffix"
)

const FileLimit = 16 << 20

type Record struct {
	Domain     string `json:"domain"`
	Subdomains bool   `json:"subdomains"`
	Path       string `json:"path"`
	Secure     bool   `json:"secure"`
	HTTPOnly   bool   `json:"httpOnly"`
	Expires    int64  `json:"expires"`
	Name       string `json:"name"`
	Value      string `json:"value"`
	Source     string `json:"source"`
	UpdatedAt  int64  `json:"updatedAt"`
}

func (r Record) ID() string {
	name := r.Name
	if name == "" {
		name = "\x00" + r.Value
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(r.Domain+"\x00"+r.Path+"\x00"+name)))
}
func (r Record) Active() bool { return r.Expires == 0 || r.Expires > time.Now().Unix() }

func Domain(raw string) (string, error) {
	domain := strings.TrimSuffix(strings.TrimPrefix(strings.ToLower(strings.TrimSpace(raw)), "."), ".")
	if strings.Contains(domain, "://") {
		u, err := url.Parse(domain)
		if err != nil || u.User != nil {
			return "", fmt.Errorf("사이트 주소가 올바르지 않습니다")
		}
		domain = u.Hostname()
	}
	if ip := net.ParseIP(strings.Trim(domain, "[]")); ip != nil {
		return ip.String(), nil
	}
	domain, err := idna.Lookup.ToASCII(domain)
	if err != nil || domain == "" || strings.ContainsAny(domain, "/\\:@ \t\r\n\x00") {
		return "", fmt.Errorf("쿠키 도메인이 올바르지 않습니다")
	}
	if net.ParseIP(domain) == nil {
		for _, label := range strings.Split(domain, ".") {
			if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
				return "", fmt.Errorf("쿠키 도메인이 올바르지 않습니다")
			}
			for _, c := range label {
				if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
					return "", fmt.Errorf("쿠키 도메인이 올바르지 않습니다")
				}
			}
		}
	}
	return domain, nil
}

func Site(raw string) string {
	domain, err := Domain(raw)
	if err != nil {
		return ""
	}
	if net.ParseIP(domain) != nil {
		return domain
	}
	if site, err := publicsuffix.EffectiveTLDPlusOne(domain); err == nil {
		return site
	}
	return domain
}

func ReadFile(filename string) ([]Record, error) {
	if !filepathAbsolute(filename) {
		return nil, fmt.Errorf("쿠키 파일 절대 경로가 필요합니다")
	}
	f, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("쿠키 파일을 열지 못했습니다")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > FileLimit {
		return nil, fmt.Errorf("일반 쿠키 파일은 16 MiB 이하여야 합니다")
	}
	body, err := io.ReadAll(io.LimitReader(f, FileLimit+1))
	if err != nil || len(body) > FileLimit {
		return nil, fmt.Errorf("쿠키 파일을 읽지 못했거나 너무 큽니다")
	}
	return Parse(body)
}

func Parse(body []byte) ([]Record, error) {
	if len(body) > FileLimit {
		return nil, fmt.Errorf("쿠키 파일은 16 MiB 이하여야 합니다")
	}
	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 4096), 1<<20)
	if !scanner.Scan() {
		return nil, fmt.Errorf("Netscape 형식의 쿠키 파일이 필요합니다")
	}
	header := strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "\ufeff"))
	if header != "# Netscape HTTP Cookie File" && header != "# HTTP Cookie File" {
		return nil, fmt.Errorf("Netscape 형식의 쿠키 파일이 필요합니다")
	}
	records := map[string]Record{}
	lineNumber := 1
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSuffix(scanner.Text(), "\r")
		httpOnly := strings.HasPrefix(line, "#HttpOnly_")
		if httpOnly {
			line = strings.TrimPrefix(line, "#HttpOnly_")
		}
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		bad := func() error {
			return fmt.Errorf("쿠키 파일 %d번째 줄의 형식이 올바르지 않습니다", lineNumber)
		}
		if len(fields) != 7 {
			return nil, bad()
		}
		domain, err := Domain(fields[0])
		if err != nil {
			return nil, bad()
		}
		if (fields[1] != "TRUE" && fields[1] != "FALSE") || (fields[3] != "TRUE" && fields[3] != "FALSE") || !strings.HasPrefix(fields[2], "/") || strings.ContainsAny(fields[2], "\r\n\x00") {
			return nil, bad()
		}
		expires := int64(0)
		if fields[4] != "" {
			n, err := strconv.ParseFloat(fields[4], 64)
			if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n >= float64(math.MaxInt64) {
				return nil, bad()
			}
			expires = int64(n)
		}
		// A Netscape jar is not a Go HTTP Cookie header. yt-dlp preserves
		// quoted/JSON values and non-token names, so retain them verbatim here.
		// Validate native HTTP serialization only when selecting request cookies.
		if hasControl(fields[5]) || hasControl(fields[6]) {
			return nil, bad()
		}
		r := Record{Domain: domain, Subdomains: fields[1] == "TRUE", Path: fields[2], Secure: fields[3] == "TRUE", HTTPOnly: httpOnly, Expires: expires, Name: fields[5], Value: fields[6]}
		records[r.ID()] = r
		if len(records) > 100000 {
			return nil, fmt.Errorf("쿠키 항목 수가 너무 많습니다")
		}
	}
	if scanner.Err() != nil {
		return nil, fmt.Errorf("쿠키 파일 항목을 읽지 못했습니다")
	}
	result := make([]Record, 0, len(records))
	for _, r := range records {
		result = append(result, r)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID() < result[j].ID() })
	return result, nil
}

func hasControl(value string) bool {
	for _, c := range value {
		if c < 0x20 || c == 0x7f {
			return true
		}
	}
	return false
}

func Encode(records []Record) []byte {
	var body strings.Builder
	body.WriteString("# Netscape HTTP Cookie File\n\n")
	flag := func(v bool) string {
		if v {
			return "TRUE"
		}
		return "FALSE"
	}
	for _, r := range records {
		domain := r.Domain
		if r.Subdomains {
			domain = "." + domain
		}
		if r.HTTPOnly {
			domain = "#HttpOnly_" + domain
		}
		fmt.Fprintf(&body, "%s\t%s\t%s\t%s\t%d\t%s\t%s\n", domain, flag(r.Subdomains), r.Path, flag(r.Secure), r.Expires, r.Name, r.Value)
	}
	return []byte(body.String())
}

func ForURL(records []Record, address *url.URL) []*http.Cookie {
	if address == nil {
		return nil
	}
	host := strings.ToLower(address.Hostname())
	path := address.EscapedPath()
	if path == "" {
		path = "/"
	}
	var result []*http.Cookie
	for _, r := range records {
		// Go's structured Cookie header cannot represent a bare-token cookie.
		// Keep it intact for CLI export, while native API auth uses named cookies.
		if r.Name == "" {
			continue
		}
		if !r.Active() || (r.Secure && address.Scheme != "https") {
			continue
		}
		if host != r.Domain && !(r.Subdomains && strings.HasSuffix(host, "."+r.Domain)) {
			continue
		}
		if path != r.Path && !(strings.HasPrefix(path, r.Path) && (strings.HasSuffix(r.Path, "/") || strings.HasPrefix(path[len(r.Path):], "/"))) {
			continue
		}
		cookie := &http.Cookie{Name: r.Name, Value: r.Value, Path: r.Path}
		if len(cookie.Value) >= 2 && cookie.Value[0] == '"' && cookie.Value[len(cookie.Value)-1] == '"' {
			cookie.Value = cookie.Value[1 : len(cookie.Value)-1]
			cookie.Quoted = true
		}
		// AddCookie would otherwise silently strip unsupported bytes and log a
		// warning. Keep those records intact in the CLI jar instead.
		if cookie.Valid() != nil {
			continue
		}
		result = append(result, cookie)
	}
	sort.SliceStable(result, func(i, j int) bool { return len(result[i].Path) > len(result[j].Path) })
	return result
}
