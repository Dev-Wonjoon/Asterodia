package webserver

import (
	"bytes"
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/Dev-Wonjoon/StorageBucket/internal/licenses"
)

var licensesPage = template.Must(template.New("licenses").Parse(`<!doctype html>
<html lang="{{.Language}}"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Labels.title}} · StorageBucket</title><link rel="stylesheet" href="/licenses.css"></head>
<body><main><nav><a href="/">← {{.Labels.gallery}}</a> · <a href="/licenses.txt" download="StorageBucket-licenses.txt">{{.Labels.download}}</a> · <a href="?lang=ko" lang="ko">한국어</a> · <a href="?lang=en" lang="en">English</a></nav>
<h1>{{.Labels.title}}</h1><p>{{.Labels.intro}}</p>
<p>{{.Labels.scopes}}</p>
{{range .Entries}}<details><summary><strong>{{.Name}}</strong><span>{{.Version}} · {{.Scope}} · {{.License}}</span></summary>
<p>{{.Notice}}</p><p><a href="{{.URL}}" target="_blank" rel="noopener noreferrer">{{$.Labels.project}}</a> · <a href="{{.SourceURL}}" target="_blank" rel="noopener noreferrer">{{$.Labels.source}}</a></p>
<pre tabindex="0">{{.Text}}</pre></details>{{end}}</main></body></html>`))

func (s *Service) licenseNotices(w http.ResponseWriter, r *http.Request) {
	language := r.URL.Query().Get("lang")
	if language != "ko" && language != "en" {
		primary := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("Accept-Language"), ",")[0]))
		primary = strings.TrimSpace(strings.Split(primary, ";")[0])
		if primary == "" || primary == "ko" || strings.HasPrefix(primary, "ko-") {
			language = "ko"
		} else {
			language = "en"
		}
	}
	labels := map[string]string{
		"title": "오픈소스 라이선스", "gallery": "갤러리", "download": "전체 원문 저장",
		"intro":   "앱에 포함된 라이브러리·리소스와 별도 외부 도구의 라이선스·저작권·추가 고지를 인터넷 연결 없이 열람할 수 있습니다.",
		"scopes":  "bundled: 앱에 포함 · external: 별도 설치 도구 · build: 빌드·개발·테스트 도구. 외부 도구는 실제 설치한 배포본의 조건을 따릅니다.",
		"project": "공식 프로젝트", "source": "소스 코드·배포 정보",
	}
	if language == "en" {
		labels = map[string]string{
			"title": "Open-source licenses", "gallery": "Gallery", "download": "Save all notice texts",
			"intro":   "Read licenses, copyrights, and additional notices for bundled libraries, resources, and external tools offline.",
			"scopes":  "bundled: included in the app · external: separately installed tools · build: build, development, and test tools. External tools follow the terms of the installed distribution.",
			"project": "Official project", "source": "Source code and distribution information",
		}
	}
	var body bytes.Buffer
	if err := licensesPage.Execute(&body, struct {
		Language string
		Labels   map[string]string
		Entries  any
	}{language, labels, licenses.List()}); err != nil {
		http.Error(w, "고지를 불러오지 못했습니다", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Language", language)
	http.ServeContent(w, r, "licenses.html", time.Time{}, bytes.NewReader(body.Bytes()))
}

func (s *Service) licenseText(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="StorageBucket-licenses.txt"`)
	http.ServeContent(w, r, "licenses.txt", time.Time{}, bytes.NewReader([]byte(licenses.Text())))
}
