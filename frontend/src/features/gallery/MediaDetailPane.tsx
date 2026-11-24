import { canEditMedia } from '../mediaedit/canEditMedia'
import { translateError, translate, useLanguage } from '@renderer/shared/i18n'
import { useConfirm } from '@renderer/shared/hooks/useConfirm'
import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { getApi } from '@renderer/shared/api'
import { useToast } from '@renderer/shared/hooks/useToast'
import { MediaPreview } from '@renderer/shared/components/media/MediaPreview'
import { MediaPlayer } from '@renderer/shared/components/media/MediaPlayer'
import { MEDIA_PAGE_SIZE, type GallerySnapshot, type GalleryStore } from './galleryStore'
import { MediaThumbnailStatus } from './MediaThumbnailStatus'
import { MediaEditLauncher } from '../mediaedit/MediaEditLauncher'
import { ActionButton } from '@renderer/shared/components/ui/ActionButton'
import { Play, ListPlus, FolderInput } from 'lucide-react'
import { prepareRemoval, removalConfirmation, removalCounts, removalLabel } from './galleryRemoval'

export interface MediaDetailFocusRequest {
    mediaId: number
    field: 'title' | 'tags'
    sequence: number
}

export function MediaDetailPane({
    state,
    store,
    onDirtyChange,
    focusRequest,
    onViewFile, onAddFiles, onAddMedia, onGatherFiles
}: {
    state: GallerySnapshot
    store: GalleryStore
    onDirtyChange: (dirty: boolean) => void
    onViewFile?: (fileId: number) => void
    onAddFiles?: (fileIds: number[]) => void
    onAddMedia?: (mediaId: number) => void
    onGatherFiles?: (fileIds: number[]) => void
    focusRequest?: MediaDetailFocusRequest | null
}) {
    useLanguage()
    const { showToast } = useToast()
    const { confirm } = useConfirm()
    const alive = useRef(true)
    useEffect(() => { alive.current = true; return () => { alive.current = false } }, [])
    function canContinue() {
        const current = store.getSnapshot()
        return alive.current && !current.busy && !current.needsRefresh && current.detail.data?.id === data?.id && current.detail.data?.revision === data?.revision
    }
    const detail = state.detail
    const data = detail.data

    const [draft, setDraft] = useState<{ title: string; revision: number; dirty: boolean } | null>(
        null
    )

    const [newTagName, setNewTagName] = useState('')
    const titleInput = useRef<HTMLInputElement>(null)
    const tagInput = useRef<HTMLInputElement>(null)
    const handledFocus = useRef<number | null>(null)
    useEffect(() => {
        if (
            !focusRequest ||
            focusRequest.mediaId !== data?.id ||
            detail.status !== 'ready' ||
            !draft ||
            handledFocus.current === focusRequest.sequence
        )
            return
        const input = focusRequest.field === 'title' ? titleInput.current : tagInput.current
        if (!input || input.disabled) return
        handledFocus.current = focusRequest.sequence
        input.focus({ preventScroll: true })
        input.scrollIntoView({ block: 'center' })
    }, [focusRequest, data?.id, detail.status, draft])

    const hasDraft = Boolean(draft?.dirty || newTagName.trim())

    useEffect(() => {
        if (!data) return

        setDraft((previous) =>
            previous?.dirty
                ? previous
                : {
                      title: data.customTitle ?? '',
                      revision: data.revision,
                      dirty: false
                  }
        )
    }, [data])

    useLayoutEffect(() => {
        onDirtyChange(hasDraft)
        return () => onDirtyChange(false)
    }, [hasDraft, onDirtyChange])

    const disabled = state.busy || state.needsRefresh || detail.status !== 'ready'

    const staleDraft = Boolean(data && draft && draft.revision !== data.revision)

    async function saveTitle() {
        if (!data || !draft || disabled || staleDraft) {
            return
        }

        const saved = await store.edit({
            mediaId: data.id,
            expectedRevision: draft.revision,
            customTitle: draft.title
        })

        if (saved) {
            const current = store.getSnapshot().detail.data

            setDraft(
                current
                    ? {
                          title: current.customTitle ?? '',
                          revision: current.revision,
                          dirty: false
                      }
                    : null
            )
        }
    }

    async function addTag() {
        if (!data || disabled || !newTagName.trim()) {
            return
        }

        const submitted = newTagName

        const saved = await store.edit({
            mediaId: data.id,
            expectedRevision: data.revision,
            tagChange: {
                addNames: [submitted.trim()]
            }
        })

        if (saved) {
            setNewTagName((current) => (current === submitted ? '' : current))
        }
    }

    async function resetDraft() {
        if (!data || state.busy) return

        if (
            draft?.dirty &&
            !await confirm({ title: translate("gallery.reloadSavedTitle"), message: translate("gallery.discardYourTitleDraftAndLoadTheCurrentlySaved"), confirmLabel: translate("gallery.load") })
        ) {
            return
        }

        if (!canContinue()) return
        setDraft({
            title: data.customTitle ?? '',
            revision: data.revision,
            dirty: false
        })
    }

    async function showFile(filepath: string) {
        try {
            await getApi().showItemInFolder(filepath)
        } catch {
            showToast(translate("gallery.couldNotOpenTheFileLocation"), 'error')
        }
    }

    const button =
        'rounded-lg border border-[var(--border-line)] px-3 py-2 text-sm disabled:opacity-40'

    return (
        <section aria-label={translate("gallery.collectionDetails")} className="space-y-5">
            {detail.status === 'loading' && <p role="status">{translate("gallery.loadingDetails")}</p>}

            {detail.error && <p role="alert">{translateError(detail.error)}</p>}

            {detail.status === 'idle' && <p>{translate("gallery.refreshToViewDetails")}</p>}

            {data && (
                <>
                    <div className="flex flex-col gap-4">
                        <div className="min-w-0 flex-1 space-y-2">
                            <h3 className="break-words font-medium">{data.title}</h3>

                            <p className="text-sm text-[var(--text-muted)]">
                                {data.isUserAlbum ? translate("gallery.userAlbums") : data.sourceKind === 'local'
                                    ? translate("gallery.localFolder")
                                    : data.sourceKind === 'download'
                                      ? translate("navigation.downloads")
                                      : translate("gallery.existingMedia")}
                                {' · ' + translate("gallery.files.count", { count: data.fileCount })}
                            </p>

                            {data.requestUrl && (
                                <p className="break-all text-sm">{data.requestUrl}</p>
                            )}

                            <p className="text-sm">{translate("gallery.cover", { filename: data.coverMode === 'manual' ? translate("gallery.custom") : translate("download.auto") })}</p>

                            <div className="flex flex-wrap gap-2">
                                <button
                                    type="button"
                                    className={button}
                                    disabled={disabled}
                                    aria-pressed={data.isFavorite}
                                    onClick={() =>
                                        void store.edit({
                                            mediaId: data.id,
                                            expectedRevision: data.revision,
                                            isFavorite: !data.isFavorite
                                        })
                                    }
                                >
                                    {data.isFavorite ? translate("gallery.favorite.remove") : translate("gallery.favorite.add")}
                                </button>

                                <button
                                    type="button"
                                    className={button}
                                    disabled={disabled || data.coverMode === 'auto'}
                                    onClick={() =>
                                        void store.edit({
                                            mediaId: data.id,
                                            expectedRevision: data.revision,
                                            cover: { mode: 'auto' }
                                        })
                                    }
                                >{translate("gallery.chooseCoverAutomatically")}</button>
                            </div>
                        </div>
                    </div>

                    <form
                        onSubmit={(event) => {
                            event.preventDefault()
                            void saveTitle()
                        }}
                        className="space-y-2"
                    >
                        <label className="block text-sm">{translate("gallery.customTitle")}<input
                                ref={titleInput}
                                value={draft?.title ?? ''}
                                disabled={state.busy || !draft}
                                onChange={(event) => {
                                    const title = event.target.value

                                    setDraft((previous) =>
                                        previous
                                            ? {
                                                  ...previous,
                                                  title,
                                                  dirty: true
                                              }
                                            : previous
                                    )
                                }}
                                className="mt-1 w-full rounded-lg border border-[var(--border-line)] bg-[var(--bg-app)] p-2"
                            />
                        </label>

                        {staleDraft && (
                            <p role="alert" className="text-sm">{translate("gallery.theSavedTitleChangedAfterYouStartedEditingReview")}</p>
                        )}

                        <div className="flex flex-wrap gap-2">
                            <button
                                type="submit"
                                className={button}
                                disabled={disabled || !draft?.dirty || staleDraft}
                            >{translate("gallery.saveTitle")}</button>

                            <button
                                type="button"
                                className={button}
                                disabled={state.busy}
                                onClick={resetDraft}
                            >{translate("gallery.reloadSavedTitle")}</button>

                            <button
                                type="button"
                                className={button}
                                disabled={disabled}
                                onClick={async () => {
                                    if (
                                        draft?.dirty &&
                                        !await confirm({ title: translate("gallery.restoreOriginalTitle"), message: translate("gallery.discardYourTitleDraftAndRestoreTheOriginalTitle"), confirmLabel: translate("gallery.restore") })
                                    ) {
                                        return
                                    }

                                    if (!canContinue()) return
                                    void store
                                        .edit({
                                            mediaId: data.id,
                                            expectedRevision: data.revision,
                                            customTitle: null
                                        })
                                        .then((saved) => {
                                            if (!saved || !alive.current) return

                                            const current = store.getSnapshot().detail.data

                                            setDraft(
                                                current
                                                    ? {
                                                          title: current.customTitle ?? '',
                                                          revision: current.revision,
                                                          dirty: false
                                                      }
                                                    : null
                                            )
                                        })
                                }}
                            >{translate("gallery.restoreOriginalTitle")}</button>
                        </div>
                    </form>

                    <form
                        className="space-y-2"
                        onSubmit={(event) => {
                            event.preventDefault()
                            void addTag()
                        }}
                    >
                        <h3 className="text-sm font-medium">{translate("media.tags")}</h3>

                        <div className="flex flex-wrap gap-2">
                            {data.tags.map((tag) => (
                                <span
                                    key={tag.id}
                                    className="inline-flex items-center gap-2 rounded-full border border-[var(--border-line)] px-3 py-1 text-sm"
                                >
                                    {tag.name}

                                    <button
                                        type="button"
                                        disabled={disabled}
                                        aria-label={tag.name + translate("gallery.unlinkTag")}
                                        onClick={() =>
                                            void store.edit({
                                                mediaId: data.id,
                                                expectedRevision: data.revision,
                                                tagChange: {
                                                    removeIds: [tag.id]
                                                }
                                            })
                                        }
                                    >
                                        ×
                                    </button>
                                </span>
                            ))}

                            {!data.tags.length && (
                                <span className="text-sm text-[var(--text-muted)]">{translate("gallery.noLinkedTags")}</span>
                            )}
                        </div>

                        <div className="flex gap-2">
                            <input
                                ref={tagInput}
                                aria-label={translate("gallery.tagNameToAdd")}
                                value={newTagName}
                                disabled={state.busy}
                                onChange={(event) => setNewTagName(event.target.value)}
                                placeholder={translate("gallery.tagName")}
                                className="min-w-0 flex-1 rounded-lg border border-[var(--border-line)] bg-[var(--bg-app)] p-2 text-sm"
                            />

                            <button
                                type="submit"
                                className={button}
                                disabled={disabled || !newTagName.trim()}
                            >{translate("gallery.add")}</button>
                        </div>
                    </form>

                    <div className="space-y-3">
                        <h3 className="font-medium">{translate("gallery.files.count", { count: data.files.total })}</h3>
                        {onAddMedia && <ActionButton icon={<ListPlus />} label={translate("gallery.addAll")} title={translate("gallery.addAllFilesToAnAlbum", { count: data.fileCount })} disabled={disabled || hasDraft || data.fileCount === 0} onClick={() => onAddMedia(data.id)} />}

                        {data.files.data.map((file) => (
                            <article
                                key={file.id}
                                className="flex flex-col gap-3 rounded-lg border border-[var(--border-line)] p-3"
                            >
                                <div className="flex flex-wrap gap-2">
                                    {onViewFile && <ActionButton icon={<Play />} label={translate("viewer.open")} disabled={disabled} onClick={() => onViewFile(file.id)} />}
                                    {onAddFiles && <ActionButton icon={<ListPlus />} label={translate("gallery.album.add")} disabled={disabled || hasDraft} onClick={() => onAddFiles([file.id])} />}
                                    {onGatherFiles && <ActionButton icon={<FolderInput />} label={translate("gallery.collectInOneFolder")} disabled={disabled} onClick={() => onGatherFiles([file.id])} />}
                                </div>
                                {(file.kind === 'video' || file.kind === 'audio') && <MediaPlayer key={`${file.id}:${data.revision}`} path={file.filepath} kind={file.kind} title={file.title ?? file.fileKey} poster={file.thumbnailPath} />}
                                {file.kind !== 'video' && file.kind !== 'audio' && <MediaPreview
                                    path={file.kind === 'image' ? file.filepath : file.previewPath ?? file.thumbnailPath}
                                    fallbackPath={file.kind === 'image' ? file.previewPath ?? file.thumbnailPath : null}
                                    title={file.title ?? file.fileKey}
                                    className="max-h-[60vh] w-full rounded bg-[var(--bg-active)] object-contain"
                                />}

                                {(file.kind === 'video' || file.kind === 'image') &&
                                    file.thumbnailPath === null && (
                                        <MediaThumbnailStatus
                                            key={`${data.id}:${file.id}:${data.revision}`}
                                            mediaId={file.mediaId ?? data.id}
                                            fileId={file.id}
                                            title={file.title ?? file.fileKey}
                                            disabled={disabled}
                                        />
                                    )}

                                <div className="min-w-0 flex-1 space-y-1">
                                    <p className="break-words text-sm">
                                        {file.title ?? file.fileKey}
                                    </p>

                                    <p className="break-all text-xs text-[var(--text-muted)]">
                                        {file.filepath}
                                    </p>

                                    <p className="text-xs text-[var(--text-muted)]">
                                        {file.storageKind === 'external_reference'
                                            ? translate("gallery.externalOriginalReference")
                                            : file.storageKind === 'unverified_legacy'
                                              ? translate("gallery.unknownStorageOwnership")
                                              : translate("gallery.appManagedFile")}

                                        {file.detectedFormat ? ' · ' + file.detectedFormat : ''}
                                    </p>
                                </div>

                                <div className="flex flex-wrap items-start gap-2">
                                    {canEditMedia(file) && <MediaEditLauncher file={file} disabled={disabled} />}
                                    <button
                                        type="button"
                                        className={button}
                                        disabled={state.busy}
                                        onClick={() => void showFile(file.filepath)}
                                    >{translate("gallery.fileLocation")}</button>

                                    <button
                                        type="button"
                                        className={button}
                                        disabled={disabled}
                                        onClick={() =>
                                            void store.edit({
                                                mediaId: data.id,
                                                expectedRevision: data.revision,
                                                cover: {
                                                    mode: 'manual',
                                                    fileId: file.id
                                                }
                                            })
                                        }
                                    >{translate("gallery.setAsCover")}</button>

                                    <button
                                        type="button"
                                        className={button}
                                        disabled={disabled}
                                        onClick={async () => {
                                            const warning = hasDraft ? translate("gallery.ifUnregisteringTheFileRemovesItsCollectionUnsavedTitle") : ''
                                            const counts = removalCounts(data, [file.id])
                                            if (!counts) { showToast(translate('gallery.cannotDetermineRemoval'), 'error'); return }
                                            const options = removalConfirmation(counts, 1)
                                            if (!await confirm({ ...options, message: options.message + warning }) || !canContinue()) return
                                            void store.unregisterFiles({
                                                mediaId: data.id,
                                                expectedRevision: data.revision,
                                                fileIds: [file.id]
                                            })
                                        }}
                                    >
                                        {removalLabel(removalCounts(data, [file.id]))}
                                    </button>
                                </div>
                            </article>
                        ))}

                        <div className="flex items-center gap-3">
                            <button
                                type="button"
                                className={button}
                                disabled={disabled || detail.offset === 0}
                                onClick={() =>
                                    void store.loadDetail(
                                        Math.max(0, detail.offset - MEDIA_PAGE_SIZE)
                                    )
                                }
                            >{translate("gallery.previousFiles")}</button>

                            <span className="text-sm">
                                {Math.floor(detail.offset / MEDIA_PAGE_SIZE) + 1}
                                {translate("gallery.page")}
                            </span>

                            <button
                                type="button"
                                className={button}
                                disabled={disabled || !data.files.hasNextPage}
                                onClick={() =>
                                    void store.loadDetail(detail.offset + MEDIA_PAGE_SIZE)
                                }
                            >{translate("gallery.nextFiles")}</button>
                        </div>
                    </div>

                    <button
                        type="button"
                        className={button + ' text-red-500'}
                        disabled={disabled}
                        onClick={async () => {
                            const warning = hasDraft ? translate("gallery.unsavedTitleAndTagEditsWillAlsoClose") : ''
                            let counts
                            try { counts = await prepareRemoval([{ mediaId: data.id, revision: data.revision }], [data], getApi().media) }
                            catch { showToast(translate('gallery.cannotDetermineRemoval'), 'error'); return }
                            if (!canContinue()) return
                            const options = removalConfirmation(counts, 1, true)
                            if (!await confirm({ ...options, message: options.message + warning }) || !canContinue()) return
                            void store.unregisterMedia({
                                mediaId: data.id,
                                expectedRevision: data.revision
                            })
                        }}
                    >
                        {removalLabel(removalCounts(data), true)}
                    </button>
                </>
            )}
        </section>
    )
}
