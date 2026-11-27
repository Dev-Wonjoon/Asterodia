import type {
	AlbumExploreRequest,
	AlbumExplorePage,
    AlbumRequest,
    MediaSequence,
    MediaFileDto,
    MediaDetailDto,
    MediaEditRequest,
    MediaGetRequest,
    MediaListRequest,
    MediaPage,
    MediaResult,
    MediaSummaryDto,
    MediaUnregisterFilesRequest,
    MediaUnregisterRequest,
    MediaUnregisterResult,
    MediaSuggestionRequest,
    MediaDirectoryDto,
    MediaDirectoryListRequest,
    SearchSuggestionRequest,
    SearchSuggestionPage
} from './media-v2'
import type { MoveSelection } from './file-move-api'

export interface MediaApi {
    bulkEdit?(request: MediaBulkEditRequest): Promise<MediaResult<MediaBulkEditResult>>
    metadata?(request: { mediaIds: number[]; fileIds: number[]; changedMediaIds?: number[] }): Promise<MediaResult<{ media: MediaSummaryDto[]; files: MediaFileDto[] }>>
	exploreAlbum(request: AlbumExploreRequest): Promise<MediaResult<AlbumExplorePage>>
    mergeAlbum(request: { title: string; selection: MoveSelection[] }): Promise<MediaResult<MediaSummaryDto>>
    albums(request?: MediaDirectoryListRequest): Promise<MediaResult<MediaPage<MediaSummaryDto>>>
    saveAlbum(request: AlbumRequest): Promise<MediaResult<MediaSummaryDto>>
    sequence(request: { mediaId: number }): Promise<MediaResult<MediaSequence>>
    readFile(request: { fileId: number }): Promise<MediaResult<MediaFileDto>>
    searchSuggest(request: SearchSuggestionRequest): Promise<MediaResult<SearchSuggestionPage>>
    directories(request?: MediaDirectoryListRequest): Promise<MediaResult<MediaPage<MediaDirectoryDto>>>
    list(request?: MediaListRequest): Promise<MediaResult<MediaPage<MediaSummaryDto>>>

    get(request: MediaGetRequest): Promise<MediaResult<MediaDetailDto>>

    edit(request: MediaEditRequest): Promise<MediaResult<MediaSummaryDto>>

    unregisterFiles(
        request: MediaUnregisterFilesRequest
    ): Promise<MediaResult<MediaUnregisterResult>>

    unregisterMedia(request: MediaUnregisterRequest): Promise<MediaResult<MediaUnregisterResult>>

    suggest(request: MediaSuggestionRequest): Promise<MediaResult<string[]>>
}

export interface MediaBulkEditRequest {
    selection: { mediaId: number; revision: number }[]
    addTags?: string[]
    removeTags?: string[]
    isFavorite?: boolean
}
export interface MediaBulkEditResult {
    updated: { mediaId: number; revision: number }[]
    failed: { mediaId: number; error: { code: import('./media-v2').MediaErrorCode; message: string; context: Record<string, number> } }[]
}
