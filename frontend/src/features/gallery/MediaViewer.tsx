import { translateError, useLanguage } from '@renderer/shared/i18n'
import { useEffect } from 'react'
import { getApi } from '@renderer/shared/api'
import { toMediaUrl } from '@renderer/shared/utils/media'
import type { MediaFileDto, MediaResult } from '@contracts/media-v2'
import { mountMediaViewer } from '../../../../internal/webserver/web/viewer.js'
import '../../../../internal/webserver/web/viewer.css'
import '../../../../internal/webserver/web/player.css'

async function unwrap<T>(pending: Promise<MediaResult<T>>): Promise<T> {
    const result = await pending
    if (!result.ok) throw new Error(translateError(result.error))
    return result.data
}

export function MediaViewer({ mediaId, fileId, onClose }: { mediaId: number; fileId?: number; onClose: () => void }) {
    useLanguage()
    useEffect(() => {
        const api = getApi().media
        const viewer = mountMediaViewer({ mediaId, fileId,
            getSequence: (id: number) => unwrap(api.sequence({ mediaId: id })),
            getFile: (id: number) => unwrap(api.readFile({ fileId: id })),
            listMedia: ({ keyword, offset }: { keyword: string; offset: number }) => unwrap(api.list({ limit: 50, offset, filter: { keyword } })),
            getAlbum: (id: number, offset: number) => unwrap(api.get({ mediaId: id, fileOffset: offset, fileLimit: 50 })),
            contentURL: (file: MediaFileDto) => toMediaUrl(file.filepath),
            previewURL: (file: MediaFileDto) => toMediaUrl(file.previewPath ?? file.thumbnailPath),
            thumbnailURL: (file: MediaFileDto) => toMediaUrl(file.thumbnailPath), onClose,
        })
        return () => viewer.destroy()
    }, [mediaId, fileId, onClose])
    return null
}
