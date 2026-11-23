import { translate, useLanguage } from '@renderer/shared/i18n'
import { useEffect, useRef, useState } from 'react'
import { toMediaUrl } from '@renderer/shared/utils/media'
import { mountVideoPlayer } from '../../../../../internal/webserver/web/player.js'
import '../../../../../internal/webserver/web/player.css'

export function MediaPlayer({ path, kind, title, poster }: { path: string; kind: 'video' | 'audio'; title: string; poster: string | null }) {
    useLanguage()
    const [failed, setFailed] = useState(false)
    const src = toMediaUrl(path)!
    const host = useRef<HTMLDivElement>(null)
    useEffect(() => {
        setFailed(false)
        if (kind !== 'video' || !host.current) return
        const player = mountVideoPlayer(host.current, { src, title, poster: toMediaUrl(poster), onError: () => setFailed(true) })
        return () => player.destroy()
    }, [src, kind, title, poster])
    return <div className="space-y-2">
        {kind === 'video'
            ? <div ref={host} />
            : <audio aria-label={title} className="w-full" src={src} controls preload="none" onError={() => setFailed(true)} />}
        {failed && <p role="alert" className="text-xs text-amber-500">{translate("이 파일을 현재 재생 환경에서 열 수 없습니다. 파일이 이동했거나 지원하지 않는 코덱일 수 있습니다. 파일 위치를 열어 원본을 확인하세요.")}</p>}
    </div>
}
