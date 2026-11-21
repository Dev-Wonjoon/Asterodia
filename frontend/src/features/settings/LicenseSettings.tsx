import { SettingSection } from '@renderer/shared/components/ui/SettingSection'
import { useLicenseSettingsViewModel } from '@renderer/features/settings/useLicenseSettingsViewModel'

const groups = [
    ['bundled', '앱에 포함된 라이브러리·리소스'],
    ['external', '별도로 설치하는 외부 도구'],
    ['build', '빌드·개발·테스트 도구']
] as const

export const LicenseSettings = (): React.JSX.Element => {
    const { licenses, expanded, toggle, loading, error, retry, query, setQuery } = useLicenseSettingsViewModel()
    return (
        <SettingSection title="오픈소스 라이선스"
            description="라이선스·저작권·추가 고지 원문을 인터넷 연결 없이 열람할 수 있습니다. 외부 도구는 각 배포본의 별도 조건을 따릅니다.">
            <div className="p-4 space-y-3">
                <input type="search" aria-label="라이선스 검색" placeholder="구성 요소 또는 라이선스 검색"
                    value={query} onChange={event => setQuery(event.target.value)}
                    className="w-full rounded-lg border border-[var(--border-line)] bg-[var(--bg-app)] px-3 py-2 text-sm text-[var(--text-main)]" />
                {loading && <p role="status">고지를 불러오는 중입니다.</p>}
                {error && <div role="alert">고지를 불러오지 못했습니다. <button onClick={retry} className="underline">다시 시도</button></div>}
                {!loading && !error && licenses.length === 0 && <p>검색 결과가 없습니다.</p>}
            </div>
            {!loading && !error && groups.map(([scope, title]) => {
                const items = licenses.filter(item => item.scope === scope)
                if (!items.length) return null
                return <section key={scope} aria-label={title}>
                    <h3 className="px-4 py-2 text-sm font-semibold text-[var(--text-muted)]">{title} ({items.length})</h3>
                    {items.map(item => <div key={item.id}>
                        <button onClick={() => toggle(item.id)} aria-expanded={expanded === item.id} aria-controls={`license-${item.id}`}
                            className="w-full flex items-center justify-between gap-3 px-4 py-3 text-left text-sm text-[var(--text-main)] hover:bg-[var(--bg-hover)]">
                            <span className="min-w-0 break-words"><strong>{item.name}</strong><br /><span className="text-xs text-[var(--text-muted)]">{item.version} · {item.license}</span></span>
                            <span aria-hidden="true">{expanded === item.id ? '−' : '+'}</span>
                        </button>
                        {expanded === item.id && <div id={`license-${item.id}`} className="px-4 pb-4 space-y-3">
                            <p className="text-xs text-[var(--text-muted)] leading-relaxed">{item.notice}</p>
                            <div className="flex gap-4 text-xs text-[var(--color-primary)]">
                                <a href={item.url} target="_blank" rel="noopener noreferrer" className="underline">공식 프로젝트</a>
                                <a href={item.sourceURL} target="_blank" rel="noopener noreferrer" className="underline">소스 코드·배포 정보</a>
                            </div>
                            <pre tabIndex={0} aria-label={`${item.name} 고지 원문`} className="max-h-96 overflow-auto whitespace-pre-wrap break-words rounded-lg bg-[var(--bg-app)] p-3 text-xs text-[var(--text-main)]">{item.text}</pre>
                        </div>}
                    </div>)}
                </section>
            })}
        </SettingSection>
    )
}
