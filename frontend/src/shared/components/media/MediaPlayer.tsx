import { translate, useLanguage } from '@renderer/shared/i18n'
import { useEffect, useRef, useState } from 'react'
import { toMediaUrl } from '@renderer/shared/utils/media'
import { mountVideoPlayer } from '../../../../../internal/webserver/web/player.js'
import '../../../../../internal/webserver/web/player.css'

export function MediaPlayer({ path, kind, title, poster, windowFullscreen }: { path: string; kind: 'video' | 'audio'; title: string; poster: string | null; windowFullscreen?: import('@contracts/api').AppApi['windowFullscreen'] }) {
    useLanguage()
    const [failed, setFailed] = useState(false)
    const src = toMediaUrl(path)!
    const host = useRef<HTMLDivElement>(null)
    useEffect(() => {
        setFailed(false)
        if (!host.current) return
        const player = mountVideoPlayer(host.current, { src, kind, title, windowFullscreen, poster: toMediaUrl(poster), onError: () => setFailed(true) })
        return () => player.destroy()
    }, [src, kind, title, poster, windowFullscreen])
    return <div className="space-y-2">
        <div ref={host} />
        {failed && <p role="alert" className="text-xs text-amber-500">{translate("common.thisFileCannotPlayInTheCurrentEnvironmentIt")}</p>}
    </div>
}
