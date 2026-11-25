export type MediaSourceKind = 'download' | 'local' | 'legacy' | 'edited'

export type MediaStorageKind = 'app_managed' | 'external_reference' | 'unverified_legacy'

export type MediaFileKind = 'image' | 'video' | 'audio' | 'unknown'

export type MediaErrorCode =
    | 'INVALID_ARGUMENT'
    | 'NOT_FOUND'
    | 'REVISION_CONFLICT'
    | 'JOB_ACTIVE'
    | 'INVALID_COVER'
    | 'INVALID_LOCATION'
    | 'FILE_NOT_FOUND'
    | 'INVARIANT_VIOLATION'
    | 'INTERNAL_ERROR'
    | 'NOT_READY'

export type MediaResult<T> =
    | {
          ok: true
          data: T
      }
    | {
          ok: false
          error: {
              code: MediaErrorCode
              message: string
              context: Record<string, number>
          }
      }

export interface MediaFilter {
    showHidden?: boolean
    keyword?: string
    tags?: string[]
    tagMode?: 'and' | 'or'
    platforms?: string[]
    authors?: string[]
    createdFrom?: number
    createdTo?: number
}

export interface ContentFilters { hiddenPlatforms: string[]; hiddenTags: string[] }

export interface MediaSuggestionRequest {
    kind: 'tag' | 'platform' | 'author'
    text: string
    limit?: number
}

export interface SearchSuggestion { kind: 'title' | 'tag' | 'author' | 'platform'; value: string }
export interface SearchSuggestionRequest {
    text: string
    kind?: SearchSuggestion['kind']
    limit?: number
    offset?: number
    filter?: MediaFilter
    favoritesOnly?: boolean
}
export interface SearchSuggestionPage {
    data: SearchSuggestion[]
    offset: number
    hasNextPage: boolean
    revision: number
    indexed: boolean
}

export interface MediaListRequest {
    limit?: number
    offset?: number
    favoritesOnly?: boolean
    filter?: MediaFilter
    gallery?: boolean
    albumCards?: boolean
    galleryView?: GalleryView
    showAlbumFiles?: boolean
    expandedMediaIds?: number[]
    anchorMediaId?: number
}

export type GalleryView = 'all' | 'albums' | 'files'

export interface MediaGetRequest {
    mediaId: number
    fileLimit?: number
    fileOffset?: number
}

export interface MediaEditRequest {
    mediaId: number
    expectedRevision: number
    customTitle?: string | null
    cover?: { mode: 'auto' } | { mode: 'manual'; fileId: number }
    tagIds?: number[]
    tagChange?: {
        addNames?: string[]
        removeIds?: number[]
    }
    isFavorite?: boolean
    isPinned?: boolean
    pinFileId?: number
}

export interface MediaUnregisterRequest {
    mediaId: number
    expectedRevision: number
}

export interface MediaUnregisterFilesRequest extends MediaUnregisterRequest {
    fileIds: number[]
}

export interface MediaSummaryDto {
	extension?: string | null
	parentId?: number | null
	childAlbumCount?: number
    managedFileCount?: number
    isPinned?: boolean
    isUserAlbum?: boolean
    galleryFile?: MediaFileDto
    galleryFileOffset?: number
    galleryCanCollapse?: boolean
    galleryPinned?: boolean
    id: number
    sourceKind: MediaSourceKind
    requestUrl: string | null
    sourceTitle: string | null
    customTitle: string | null
    title: string
    revision: number
    coverMode: 'auto' | 'manual'
    cover: {
        fileId: number
        path: string
        kind: MediaFileKind
    } | null
    fileCount: number
    isFavorite: boolean
    createdAt: number
    updatedAt: number | null
}

export interface AlbumRequest {
	parentId?: number
    albumId?: number
    expectedRevision?: number
    title?: string
    addFileIds?: number[]
    addMediaIds?: number[]
    removeFileIds?: number[]
    moveFileId?: number
    beforeFileId?: number
    delete?: boolean
}
export interface MediaSequence { ids: number[]; title: string; revision: number }

export type AlbumExplorerSort = 'name' | 'date' | 'size' | 'kind' | 'duration'
export interface AlbumExploreRequest {
    mediaId: number; limit?: number; offset?: number; filter?: MediaFilter
    recursive?: boolean; sort?: AlbumExplorerSort; descending?: boolean
}
export type AlbumEntry = { kind: 'album'; album: MediaSummaryDto } | { kind: 'file'; file: MediaFileDto; albumId: number; title: string }
export interface AlbumExplorePage extends MediaPage<AlbumEntry> {
    revisionToken?: string
    album: MediaSummaryDto
    breadcrumbs: { id: number; title: string }[]
}

export interface MediaFileDto {
    extension?: string | null
    id: number
    mediaId: number
    fileKey: string
    fileUuid: string
    filenameToken: string
    directoryId: number | null
    storageKind: MediaStorageKind
    sourceUrl: string | null
    title: string | null
    filepath: string
    relativePath: string | null
    kind: MediaFileKind
    position: number
    filesize: number | null
    duration: number | null
    detectedFormat: string | null
    width: number | null
    height: number | null
    orientation: number | null
    frameCount: number | null
    pageCount: number | null
    primaryImageIndex: number | null
    thumbnailPath: string | null
    thumbnailOrigin: 'none' | 'app_generated' | 'unverified_legacy'
    previewPath: string | null
    createdAt: number
}

export interface MediaDirectoryDto {
    id: number
    directoryPath: string
    keyVersion: 0 | 1
    importMediaId: number | null
    fileCount: number
    mediaCount: number
}
export interface MediaDirectoryListRequest {
    limit?: number
    offset?: number
    keyword?: string
}

export interface MediaPage<T> {
    data: T[]
    total: number
    hasNextPage: boolean
    offset?: number
}

export interface MediaDetailDto extends MediaSummaryDto {
    files: MediaPage<MediaFileDto>
    tags: {
        id: number
        name: string
    }[]
    favorite: {
        id: number
        createdAt: number
    } | null
    localFolder: {
        id: number
        sourceTitle: string
        revision: number
    } | null
    locations: {
        id: number
        directoryPath: string
        state: 'active' | 'reserved' | 'history'
    }[]
}

export interface MediaUnregisterResult {
    mediaId: number
    parentRemoved: boolean
    revision: number | null
    cleanupTaskId: string | null
}
