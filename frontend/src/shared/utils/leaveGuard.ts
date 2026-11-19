import type { Confirm } from '../hooks/confirmContext'
export function confirmDiscard(dirty: boolean, confirm: Confirm): boolean | Promise<boolean> {
 return !dirty || confirm({ title: '편집 내용 버리기', message: '작성 중인 제목·태그를 버리고 이동하거나 상세를 닫을까요?', confirmLabel: '버리고 계속', danger: true })
}
