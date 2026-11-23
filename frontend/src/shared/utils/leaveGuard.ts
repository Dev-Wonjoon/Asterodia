import { translate } from '@renderer/shared/i18n'
import type { Confirm } from '../hooks/confirmContext'
export function confirmDiscard(dirty: boolean, confirm: Confirm): boolean | Promise<boolean> {
 return !dirty || confirm({ title: translate("편집 내용 버리기"), message: translate("작성 중인 제목·태그를 버리고 이동하거나 상세를 닫을까요?"), confirmLabel: translate("버리고 계속"), danger: true })
}
