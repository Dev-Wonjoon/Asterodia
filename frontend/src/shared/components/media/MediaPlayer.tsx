import { useState } from 'react'
import { toMediaUrl } from '@renderer/shared/utils/media'

export function MediaPlayer({ path, kind, title, poster }: { path: string; kind: 'video' | 'audio'; title: string; poster: string | null }) {
    const [failed, setFailed] = useState(false)
    const src = toMediaUrl(path)!
    return <div className="space-y-2">
        {kind === 'video'
            ? <video aria-label={title} className="max-h-[60vh] w-full rounded bg-black" src={src} poster={toMediaUrl(poster) ?? undefined} controls preload="none" onError={() => setFailed(true)} />
            : <audio aria-label={title} className="w-full" src={src} controls preload="none" onError={() => setFailed(true)} />}
        {failed && <p role="alert" className="text-xs text-amber-500">이 파일을 현재 재생 환경에서 열 수 없습니다. 파일이 이동했거나 지원하지 않는 코덱일 수 있습니다. 파일 위치를 열어 원본을 확인하세요.</p>}
    </div>
}
