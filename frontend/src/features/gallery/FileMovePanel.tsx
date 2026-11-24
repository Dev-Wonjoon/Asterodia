import { translateMessage, translateError, translate, useLanguage } from '@renderer/shared/i18n'
import { useConfirm } from '@renderer/shared/hooks/useConfirm'
import { useEffect, useImperativeHandle, useMemo, useRef, useState, type Ref } from 'react'
import type { MoveSelection, MoveStatus } from '@contracts/file-move-api'
import type { MediaDirectoryDto } from '@contracts/media-v2'
import { getApi } from '@renderer/shared/api'
import { ProgressBar } from '@renderer/shared/components/ui/ProgressBar'
import { formatBytes } from '@renderer/shared/utils/formatBytes'

export const moveRunning = (state: string) => state === 'running' || state === 'pausing'
export const moveLabel = (state: string) => ({ pending: translate("gallery.waitingToStart"), running: translate("gallery.moving"), pausing: translate("gallery.stopping"), paused: translate("download.interrupted"), interrupted: translate("gallery.stoppedCanResume"), completed: translate("gallery.moveCompleted"), completed_with_errors: translate("gallery.someItemsNeedAttention"), failed: translate("download.failed"), copying: translate("gallery.copying"), verified: translate("gallery.verified"), committed: translate("gallery.awaitingSourceCleanup"), cleanup_pending: translate("gallery.sourceCleanupRequired"), excluded: translate("gallery.alreadyInDestinationFolder") }[state] ?? state)

export interface FileMoveControls { choose: (selection?: MoveSelection[]) => void; track: (jobId: string, error?: string) => void }

export function FileMovePanel({ selection, disabled, onCreated, onChanged, compact = false, controlRef, onBusyChange }: {
 selection: MoveSelection[]; disabled: boolean; onCreated: () => void; onChanged: () => void; compact?: boolean; controlRef?: Ref<FileMoveControls>; onBusyChange?: (busy: boolean) => void
}) {
    useLanguage()
 const { confirm } = useConfirm()
 const starting = useRef(false)
 const [status, setStatus] = useState<MoveStatus | null>(null)
 const [error, setError] = useState('')
 const [busy, setBusy] = useState(false)
 const [choosing, setChoosing] = useState(false)
 const [directories, setDirectories] = useState<MediaDirectoryDto[]>([])
 const [hasMoreDirectories, setHasMoreDirectories] = useState(false)
 const [directory, setDirectory] = useState('')
 const [operation, setOperation] = useState<'copy' | 'move'>('copy')
 const [chosen, setChosen] = useState<MoveSelection[] | null>(null)
 const effectiveSelection = chosen ?? selection
 const [keyword, setKeyword] = useState('')
 const [offset, setOffset] = useState(0)
 const [hidden, setHidden] = useState('')
 const [refresh, setRefresh] = useState(0)
 const [jobId, setJobId] = useState('')
 const changed = useRef(onChanged); changed.current = onChanged
 const mounted = useRef(true)
 const counts = useRef('')
 const api = useMemo(() => getApi(), [])
 useEffect(() => {
  mounted.current = true
  let stopped = false
  let timer: ReturnType<typeof setTimeout> | undefined
  const poll = async () => {
   try {
    const result = await api.fileMove.get({ offset, ...(jobId ? { jobId } : {}) })
    if (stopped) return
    if (!result.ok) { setError(translateError(result.error)); return }
    setStatus(result.data)
    if (result.data) {
     const signature = `${result.data.jobId}:${result.data.completed}:${result.data.cleanupPending}`
     if (counts.current && counts.current !== signature) changed.current()
     counts.current = signature
     if (moveRunning(result.data.state)) timer = setTimeout(() => void poll(), 500)
    }
   } catch { if (!stopped) setError(translate("gallery.couldNotCheckMoveStatusSelectRefreshStatus")) }
  }
  void poll()
  return () => { stopped = true; mounted.current = false; clearTimeout(timer) }
 }, [api, offset, refresh, jobId])
 async function folders(append = false) {
  setBusy(true); setError('')
  try {
   const result = await api.media.directories({ limit: 100, offset: append ? directories.length : 0, keyword })
   if (!mounted.current) return
   if (!result.ok) { setError(translateError(result.error)); return }
   setDirectories(previous => append ? [...previous, ...result.data.data] : result.data.data)
   setHasMoreDirectories(result.data.hasNextPage)
   if (!append) setDirectory('')
   setChoosing(true)
  } catch { if (mounted.current) setError(translate("gallery.couldNotLoadDestinationFolders")) }
  finally { if (mounted.current) setBusy(false) }
 }
 async function start() {
  if (!effectiveSelection.length || !directory || busy || disabled) return
  const target = directories.find(item => item.id === Number(directory))
  if (!target || starting.current) return
  starting.current = true
  setError('')
  try {
   const verb = operation === 'copy' ? translate("gallery.copy") : translate("gallery.move")
   if (!await confirm({ title: translate("gallery.collectInOneFolder"), confirmLabel: translate("gallery.start", { operation: verb }), message: translate("gallery.theSelectedAlbumsAndFilesToTheFolderBelow", { operation: verb, path: target.directoryPath, operationHint: operation === 'copy' ? translate("gallery.originalFilesAndAlbumFileLocationsWillBeKept") : translate("gallery.afterCopyingAndVerificationOriginalsAreCleanedUpAnd") }) }) || !mounted.current) return
   setBusy(true)
   const created = await api.fileMove.create({ selection: effectiveSelection, directoryId: target.id, operation })
   if (!mounted.current) return
   if (!created.ok) { setError(translateError(created.error)); return }
   setChoosing(false);setHidden('');setOffset(0);setJobId(created.data.jobId);onCreated()
   const result = await api.fileMove.run({ jobId: created.data.jobId })
   if (!mounted.current) return
   if (!result.ok) setError(translateError(result.error))
  } catch { if (mounted.current) setError(translate("gallery.couldNotConfirmTheResultRefreshStatusAndCheck")) }
  finally { starting.current = false; if (mounted.current) { setBusy(false); setRefresh(v => v + 1) } }
 }
 async function action(kind: 'run' | 'pause' | 'retry') {
  if (!status || busy) return
  setBusy(true);setError('')
  try {
   const result = await api.fileMove[kind]({ jobId: status.jobId })
   if (!mounted.current) return
   if (!result.ok) setError(translateError(result.error))
  } catch { if (mounted.current) setError(translate("gallery.couldNotConfirmTheResultRefreshStatus")) }
  finally { if (mounted.current) { setBusy(false);setRefresh(v => v + 1) } }
 }
 const running = status && moveRunning(status.state)
 useImperativeHandle(controlRef, () => ({ track: (id, message = '') => { setJobId(id); setOffset(0); setHidden(''); setError(message); setRefresh(v => v + 1) }, choose: (override) => { const value = override ?? selection; if (!busy && !disabled && !running && value.length) { setChosen(value.map(item => ({ ...item, fileIds: item.fileIds ? [...item.fileIds] : undefined }))); setOperation('copy'); void folders() } } }))
 useEffect(() => { onBusyChange?.(busy || choosing) }, [busy, choosing, onBusyChange])
 const button = 'rounded-lg border border-[var(--border-line)] px-3 py-2 text-sm disabled:opacity-40'
 if (compact && !error && !choosing && !status) return null
 return <section aria-label={translate("gallery.moveFiles")} className="space-y-3">
  {!compact && <div className="flex flex-wrap items-center gap-2">
   <span className="text-sm">{translate("gallery.postsSelected", { count: selection.length })}</span>
   <button className={button} disabled={busy || disabled || !!running || !selection.length} onClick={() => { setChosen(null); setOperation('copy'); void folders() }}>{translate("gallery.collectInOneFolder")}</button>
   {selection.length > 0 && <button className={button} disabled={busy} onClick={onCreated}>{translate("gallery.selection.clear")}</button>}
  </div>}
  {error && <p role="alert" className="text-sm text-red-500">{translateMessage(error)}</p>}
  {choosing && <div className="space-y-3 rounded-xl border border-[var(--border-line)] p-4">
   <p className="text-sm">{translate("gallery.selectADestinationFolderRegisteredThroughImportAlbumGrouping")}</p>
   <form className="flex flex-wrap gap-2" onSubmit={event => { event.preventDefault(); void folders() }}>
    <input aria-label={translate("gallery.searchDestinationFolders")} value={keyword} onChange={event => setKeyword(event.target.value)} className="min-w-0 flex-1 rounded border border-[var(--border-line)] bg-[var(--bg-sidebar)] p-2" />
    <button className={button} disabled={busy}>{translate("navigation.search")}</button>
   </form>
   <select aria-label={translate("gallery.collectionMethod")} value={operation} disabled={busy} onChange={event => setOperation(event.target.value as 'copy' | 'move')} className="w-full rounded border border-[var(--border-line)] bg-[var(--bg-sidebar)] p-2"><option value="copy">{translate("gallery.copyFilesKeepOriginals")}</option><option value="move">{translate("gallery.moveFilesChangeOriginalLocations")}</option></select>
   <select aria-label={translate("gallery.destinationFolder")} value={directory} onChange={event => setDirectory(event.target.value)} className="w-full min-w-0 rounded border border-[var(--border-line)] bg-[var(--bg-sidebar)] p-2">
    <option value="">{translate("gallery.selectFolder")}</option>{directories.map(item => <option key={item.id} value={item.id}>{item.directoryPath}</option>)}
   </select>
   {!directories.length && <p className="text-sm">{translate("gallery.noRegisteredFoldersImportALocalFolderFirst")}</p>}
   {hasMoreDirectories && <button className={button} disabled={busy} onClick={() => void folders(true)}>{translate("gallery.loadMoreFolders")}</button>}
   <div className="flex gap-2"><button className={button} disabled={busy || disabled || !directory || !effectiveSelection.length} onClick={() => void start()}>{operation === 'copy' ? translate("gallery.startCopying") : translate("gallery.startMoving")}</button><button className={button} disabled={busy} onClick={() => { setChoosing(false); setChosen(null) }}>{translate("common.close")}</button></div>
  </div>}
  {status && hidden !== status.jobId && <div className="space-y-3 rounded-xl border border-[var(--border-line)] bg-[var(--bg-sidebar)] p-4">
   <div className="flex items-center justify-between gap-3"><p className="font-medium">{status.operation === 'copy' ? translate("gallery.operation.copyPrefix") : translate("gallery.operation.movePrefix")}{moveLabel(status.state)}</p>{!running && <button aria-label={translate("gallery.closeMoveResults")} className={button} onClick={() => setHidden(status.jobId)}>×</button>}</div>
   <p className="break-all text-xs">{status.directory}</p>
   {!!status.albumId && <p className="text-sm">{translate("gallery.collectionCreatedCheckFileCollectionResultsBelow")}</p>}
   <p className="text-sm">{translate("gallery.totalCompletedFailedAwaitingSourceCleanup", { total: status.total, completed: status.completed, failed: status.failed, pendingCleanup: status.cleanupPending })}</p>
   <ProgressBar label={translate("gallery.fileMoveProgress")} value={status.totalBytes > 0 ? status.processedBytes / status.totalBytes * 100 : status.total ? (status.completed + status.cleanupPending) / status.total * 100 : 0} />
   <p className="break-all text-xs">{formatBytes(status.processedBytes)} / {formatBytes(status.totalBytes)}{status.current && ` · ${status.current}`}</p>
   <div className="flex flex-wrap gap-2">
    {running ? <button className={button} disabled={busy || status.state === 'pausing'} onClick={() => void action('pause')}>{translate("gallery.stop")}</button> : status.state !== 'completed' && <button className={button} disabled={busy || disabled} onClick={() => void action(status.failed || status.cleanupPending ? 'retry' : 'run')}>{status.failed || status.cleanupPending ? translate("gallery.retryFailuresAndCleanup") : translate("gallery.resume")}</button>}
    <button className={button} disabled={busy} onClick={() => { setError(''); setRefresh(v => v + 1) }}>{translate("gallery.refreshStatus")}</button>
   </div>
   <details><summary className="cursor-pointer text-sm">{translate("gallery.perFileResults")}</summary><ul className="mt-2 max-h-64 space-y-2 overflow-auto text-xs">{status.items.map(item => <li key={item.id} className="break-all"><span>{moveLabel(item.state)} · {item.source}</span>{item.target && <p>→ {item.target}</p>}{item.error && <p className="text-red-500">{translateMessage(item.error)}</p>}</li>)}</ul>
    <div className="mt-2 flex gap-2"><button className={button} disabled={offset === 0} onClick={() => setOffset(v => Math.max(0, v - 100))}>{translate("gallery.previousResults")}</button><button className={button} disabled={!status.hasMore} onClick={() => setOffset(v => v + 100)}>{translate("gallery.nextResults")}</button></div>
   </details>
  </div>}
  {status && hidden === status.jobId && <button className={button} onClick={() => setHidden('')}>{translate("gallery.viewRecentMoveResults")}</button>}
 </section>
}
