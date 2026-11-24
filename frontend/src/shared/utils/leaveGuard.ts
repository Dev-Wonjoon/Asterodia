import { translate } from '@renderer/shared/i18n'
import type { Confirm } from '../hooks/confirmContext'
export function confirmDiscard(dirty: boolean, confirm: Confirm): boolean | Promise<boolean> {
 return !dirty || confirm({ title: translate("common.discardEdits"), message: translate("common.discardUnsavedTitleAndTagEditsAndNavigateAway"), confirmLabel: translate("common.discardAndContinue"), danger: true })
}
