# 언어 파일 / Language catalogs

`ko.json`과 `en.json`은 데스크톱과 웹이 공유하는 언어별 번역입니다.
각 파일에 같은 기능 키를 정의합니다. 표시 문구가 바뀌어도 키는 유지합니다.

```ts
translate('gallery.selection.count', { count: selection.length })
```

- 키는 `기능.의미`로 작성하고 한국어·영어 문장을 키로 사용하지 않습니다.
- 변수는 `{count}`, `{title}`, `{path}`처럼 이름으로 구분합니다.
- 사용자 제목, 태그, 경로와 원본 엔진 로그는 번역하지 않습니다.
- 앱 상태에 저장할 안내는 `{ key, values }`로 표현할 수 있습니다.
- 새 문구는 모든 언어 파일에 추가하고 `frontend`에서 `npm test -- tests/frontend/i18n.test.ts`를 실행합니다.

The desktop and web clients share these independent JSON catalogs. Keep functional
keys stable when display text changes. Use named placeholders, preserve user data,
and add each new key to every language. The i18n tests check duplicate and missing
keys, empty translations, call sites, and matching placeholder names.
