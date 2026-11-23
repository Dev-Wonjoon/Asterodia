import { translate } from '@renderer/shared/i18n'
import { useEffect, useRef, useState } from 'react'
import type { MoveSelection } from '@contracts/file-move-api'
import { getApi } from '@renderer/shared/api'
import { useConfirm } from '@renderer/shared/hooks/useConfirm'
import type { GalleryBulkAction, GalleryBulkResult, GalleryStore } from './galleryStore'

export type BulkCommand = 'favorite' | 'unfavorite' | 'addTags' | 'removeTags' | 'openFolders' | 'unregister'

export function useGalleryBulkActions(store: GalleryStore, selection: MoveSelection[], onSuccess: (ids: number[]) => void) {
    const { confirm } = useConfirm()
    const [busy, setBusy] = useState(false)
    const [progress, setProgress] = useState<{ done: number; total: number } | null>(null)
    const [result, setResult] = useState<GalleryBulkResult | null>(null)
    const lock = useRef(false)
    const generation = useRef(0)
    const latest = useRef({ selection, onSuccess }); latest.current = { selection, onSuccess }
    useEffect(() => {
        ++generation.current; lock.current = false
        setBusy(false); setProgress(null); setResult(null)
        return () => { ++generation.current }
    }, [store])

    async function run(command: BulkCommand, names: string[] = []) {
        if (lock.current || !selection.length || store.getSnapshot().busy || store.getSnapshot().needsRefresh) return
        lock.current = true
        const own = generation.current
        const targets = selection.map(item => ({ ...item }))
        setBusy(true)
        try {
            if (command === 'unregister' || command === 'openFolders') {
                const accepted = await confirm(command === 'unregister'
                    ? { title: translate("선택한 묶음 등록 해제"), message: translate("선택한 게시글 {0}개의 등록을 해제할까요? 외부 원본과 보관 방식 미확인 원본은 삭제하지 않습니다. 앱 관리 파일의 정리는 별도로 처리합니다.", { "0": targets.length }), confirmLabel: translate("등록 해제"), danger: true }
                    : { title: translate("선택한 파일 위치 열기"), message: translate("선택한 게시글 {0}개의 파일 위치를 엽니다. 같은 폴더는 한 번만 열립니다.", { "0": targets.length }), confirmLabel: translate("열기") })
                if (!accepted || own !== generation.current || JSON.stringify(latest.current.selection) !== JSON.stringify(targets)) return
            }
            const action: GalleryBulkAction = command === 'unregister' ? { kind: 'unregister' }
                : command === 'openFolders' ? { kind: 'openFolders', open: path => getApi().showItemInFolder(path) }
                : command === 'favorite' || command === 'unfavorite' ? { kind: 'favorite', value: command === 'favorite' }
                : { kind: 'tags', names, remove: command === 'removeTags' }
            if (own !== generation.current) return
            setResult(null); setProgress({ done: 0, total: targets.length })
            const outcome = await store.bulk(targets, action, (done, total) => {
                if (own === generation.current) setProgress({ done, total })
            })
            if (own !== generation.current) return
            if (outcome) { setResult(outcome); setProgress(null); latest.current.onSuccess(outcome.succeeded) }
            else setProgress(null)
        } catch {
            if (own === generation.current) {
                setProgress(null)
                setResult({ succeeded: [], failed: targets.map(item => ({ mediaId: item.mediaId, message: translate("요청 결과를 확인하지 못했습니다. 새로고침 후 확인하세요.") })) })
            }
        } finally { if (own === generation.current) { lock.current = false; setBusy(false) } }
    }
    return { busy, progress, result, run }
}
