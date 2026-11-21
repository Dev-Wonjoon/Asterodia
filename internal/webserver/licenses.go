package webserver

import (
	"bytes"
	"html/template"
	"net/http"
	"time"

	"github.com/Dev-Wonjoon/StorageBucket/internal/licenses"
)

var licensesPage = template.Must(template.New("licenses").Parse(`<!doctype html>
<html lang="ko"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>오픈소스 라이선스 · StorageBucket</title><link rel="stylesheet" href="/licenses.css"></head>
<body><main><nav><a href="/">← 갤러리</a> · <a href="/licenses.txt" download="StorageBucket-licenses.txt">전체 원문 저장</a></nav>
<h1>오픈소스 라이선스</h1><p>앱에 포함된 라이브러리·리소스와 별도 외부 도구의 라이선스·저작권·추가 고지를 인터넷 연결 없이 열람할 수 있습니다.</p>
<p>bundled: 앱에 포함 · external: 별도 설치 도구 · build: 빌드·개발·테스트 도구. 외부 도구는 실제 설치한 배포본의 조건을 따릅니다.</p>
{{range .}}<details><summary><strong>{{.Name}}</strong><span>{{.Version}} · {{.Scope}} · {{.License}}</span></summary>
<p>{{.Notice}}</p><p><a href="{{.URL}}" target="_blank" rel="noopener noreferrer">공식 프로젝트</a> · <a href="{{.SourceURL}}" target="_blank" rel="noopener noreferrer">소스 코드·배포 정보</a></p>
<pre tabindex="0">{{.Text}}</pre></details>{{end}}</main></body></html>`))

func (s *Service) licenseNotices(w http.ResponseWriter, r *http.Request) {
	var body bytes.Buffer
	if err := licensesPage.Execute(&body, licenses.List()); err != nil {
		http.Error(w, "고지를 불러오지 못했습니다", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.ServeContent(w, r, "licenses.html", time.Time{}, bytes.NewReader(body.Bytes()))
}

func (s *Service) licenseText(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="StorageBucket-licenses.txt"`)
	http.ServeContent(w, r, "licenses.txt", time.Time{}, bytes.NewReader([]byte(licenses.Text())))
}
