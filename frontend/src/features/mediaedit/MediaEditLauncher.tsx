import { translateError, translate, useLanguage } from '@renderer/shared/i18n'
import { useEffect, useImperativeHandle, useRef, useState, type Ref } from 'react'
import { getApi } from '@renderer/shared/api'
import { toMediaUrl } from '@renderer/shared/utils/media'
import { mountMediaEditor } from '../../../../internal/webserver/web/editor.js'
import '../../../../internal/webserver/web/editor.css'

export interface MediaEditFile { id: number; filepath: string; title: string | null; fileKey: string; duration: number | null; kind?: string; detectedFormat?: string | null }
export interface MediaEditControls { open(file?: MediaEditFile): void }

export function MediaEditLauncher({ file, disabled = false, compact = false, controlRef, allowReveal = true }: { file?: MediaEditFile; disabled?: boolean; compact?: boolean; controlRef?: Ref<MediaEditControls>; allowReveal?: boolean }) {
    useLanguage()
    const [request, setRequest] = useState<{ file?: MediaEditFile } | null>(null)
    const open = request !== null, activeFile = request?.file
    const openEditor = (value = file) => { if (!disabled) setRequest({ file: value }) }
    useImperativeHandle(controlRef, () => ({ open: openEditor }))
    const host = useRef<HTMLDivElement>(null)
    useEffect(() => {
        if (!open || !host.current) return
        const api = getApi()
        const editor = mountMediaEditor(host.current, {
            api: api.mediaEdit,
            file: activeFile ? { id: activeFile.id, title: activeFile.title || activeFile.fileKey, duration: activeFile.duration,kind:activeFile.kind,detectedFormat:activeFile.detectedFormat,nativeGIF:activeFile.kind==='image', src: toMediaUrl(activeFile.filepath) } : undefined,
            onClose: () => setRequest(null),
            reveal: allowReveal ? async (id: string) => {
                const result = await api.mediaEdit.resultPath({ id })
                if (!result.ok) throw new Error(translateError(result.error))
                await api.showItemInFolder(result.data)
            } : undefined,
        })
        return () => editor.destroy()
    }, [open, activeFile?.id])
    return <>{!compact && <button type="button" disabled={disabled} onClick={() => openEditor()} className="rounded-lg border border-[var(--border-line)] px-3 py-2 text-sm disabled:opacity-40">{file ? (file.kind==='image'?translate("editor.editGif"):translate("editor.createGifOrTrimVideo")) : translate("gallery.editor.createAnimation")}</button>}<div ref={host} style={{ display: 'contents' }} /></>
}
