import { useEffect, useRef, useState } from 'react'
import { getApi } from '@renderer/shared/api'
import { toMediaUrl } from '@renderer/shared/utils/media'
import { mountMediaEditor } from '../../../../internal/webserver/web/editor.js'
import '../../../../internal/webserver/web/editor.css'

export function MediaEditLauncher({ file, disabled = false }: { file?: { id: number; filepath: string; title: string | null; fileKey: string; duration: number | null; kind?: string; detectedFormat?: string | null }; disabled?: boolean }) {
    const [open, setOpen] = useState(false)
    const host = useRef<HTMLDivElement>(null)
    useEffect(() => {
        if (!open || !host.current) return
        const api = getApi()
        const editor = mountMediaEditor(host.current, {
            api: api.mediaEdit,
            file: file ? { id: file.id, title: file.title || file.fileKey, duration: file.duration,kind:file.kind,detectedFormat:file.detectedFormat,nativeGIF:file.kind==='image', src: toMediaUrl(file.filepath) } : undefined,
            onClose: () => setOpen(false),
            reveal: async (id: string) => {
                const result = await api.mediaEdit.resultPath({ id })
                if (!result.ok) throw new Error(result.error.message)
                await api.showItemInFolder(result.data)
            },
        })
        return () => editor.destroy()
    }, [open, file?.id])
    return <><button type="button" disabled={disabled} onClick={() => setOpen(true)} className="rounded-lg border border-[var(--border-line)] px-3 py-2 text-sm disabled:opacity-40">{file ? (file.kind==='image'?'GIF 편집':'GIF 만들기·영상 자르기') : '미디어 편집 작업'}</button><div ref={host} /></>
}
