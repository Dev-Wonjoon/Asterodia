import { translate, useLanguage } from '@renderer/shared/i18n'
import { GalleryPage } from '../gallery/GalleryPage'

export const FavoritesPage = (): React.JSX.Element => {
    useLanguage()
    return <GalleryPage favoritesOnly title={translate('즐겨찾기')} />
}
