import { useConfirm } from '@renderer/shared/hooks/useConfirm'
import { useEffect, useImperativeHandle, useMemo, useRef, useState, type Ref } from 'react'
import type { MoveSelection, MoveStatus } from '@contracts/file-move-api'
import type { MediaDirectoryDto } from '@contracts/media-v2'
import { getApi } from '@renderer/shared/api'
import { ProgressBar } from '@renderer/shared/components/ui/ProgressBar'
import { formatBytes } from '@renderer/shared/utils/formatBytes'

export const moveRunning = (state: string) => state === 'running' || state === 'pausing'
export const moveLabel = (state: string) => ({ pending: '시작 대기', running: '이동 중', pausing: '중단 중', paused: '중단됨', interrupted: '중단됨 · 이어하기 가능', completed: '이동 완료', completed_with_errors: '일부 처리 필요', failed: '실패', copying: '복사 중', verified: '검증 완료', committed: '원본 정리 대기', cleanup_pending: '원본 정리 필요', excluded: '대상 폴더에 있음' }[state] ?? state)

export interface FileMoveControls { choose: (selection?: MoveSelection[]) => void }

export function FileMovePanel({ selection, disabled, onCreated, onChanged, compact = false, controlRef, onBusyChange }: {
 selection: MoveSelection[]; disabled: boolean; onCreated: () => void; onChanged: () => void; compact?: boolean; controlRef?: Ref<FileMoveControls>; onBusyChange?: (busy: boolean) => void
}) {
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
    const result = await api.fileMove.get({ offset })
    if (stopped) return
    if (!result.ok) { setError(result.error.message); return }
    setStatus(result.data)
    if (result.data) {
     const signature = `${result.data.jobId}:${result.data.completed}:${result.data.cleanupPending}`
     if (counts.current && counts.current !== signature) changed.current()
     counts.current = signature
     if (moveRunning(result.data.state)) timer = setTimeout(() => void poll(), 500)
    }
   } catch { if (!stopped) setError('이동 상태를 확인하지 못했습니다. 상태 새로고침을 눌러주세요.') }
  }
  void poll()
  return () => { stopped = true; mounted.current = false; clearTimeout(timer) }
 }, [api, offset, refresh])
 async function folders(append = false) {
  setBusy(true); setError('')
  try {
   const result = await api.media.directories({ limit: 100, offset: append ? directories.length : 0, keyword })
   if (!mounted.current) return
   if (!result.ok) { setError(result.error.message); return }
   setDirectories(previous => append ? [...previous, ...result.data.data] : result.data.data)
   setHasMoreDirectories(result.data.hasNextPage)
   if (!append) setDirectory('')
   setChoosing(true)
  } catch { if (mounted.current) setError('대상 폴더를 불러오지 못했습니다.') }
  finally { if (mounted.current) setBusy(false) }
 }
 async function start() {
  if (!effectiveSelection.length || !directory || busy || disabled) return
  const target = directories.find(item => item.id === Number(directory))
  if (!target || starting.current) return
  starting.current = true
  setError('')
  try {
   const verb = operation === 'copy' ? '복사' : '이동'
   if (!await confirm({ title: '한 폴더에 모으기', confirmLabel: `${verb} 시작`, message: `선택한 앨범·파일을 아래 폴더로 ${verb}할까요?\n${target.directoryPath}\n${operation === 'copy' ? '원본 파일과 앨범의 파일 위치는 유지합니다.' : '복사·검증 후 원본을 정리하고 모든 앨범의 파일 위치를 갱신합니다.'} 같은 이름은 번호를 붙여 보존합니다.` }) || !mounted.current) return
   setBusy(true)
   const created = await api.fileMove.create({ selection: effectiveSelection, directoryId: target.id, operation })
   if (!mounted.current) return
   if (!created.ok) { setError(created.error.message); return }
   setChoosing(false);setHidden('');setOffset(0);onCreated()
   const result = await api.fileMove.run({ jobId: created.data.jobId })
   if (!mounted.current) return
   if (!result.ok) setError(result.error.message)
  } catch { if (mounted.current) setError('요청 결과를 확인하지 못했습니다. 상태를 새로고침한 뒤 이어하기를 확인하세요.') }
  finally { starting.current = false; if (mounted.current) { setBusy(false); setRefresh(v => v + 1) } }
 }
 async function action(kind: 'run' | 'pause' | 'retry') {
  if (!status || busy) return
  setBusy(true);setError('')
  try {
   const result = await api.fileMove[kind]({ jobId: status.jobId })
   if (!mounted.current) return
   if (!result.ok) setError(result.error.message)
  } catch { if (mounted.current) setError('요청 결과를 확인하지 못했습니다. 상태를 새로고침하세요.') }
  finally { if (mounted.current) { setBusy(false);setRefresh(v => v + 1) } }
 }
 const running = status && moveRunning(status.state)
 useImperativeHandle(controlRef, () => ({ choose: (override) => { const value = override ?? selection; if (!busy && !disabled && !running && value.length) { setChosen(value.map(item => ({ ...item, fileIds: item.fileIds ? [...item.fileIds] : undefined }))); setOperation('copy'); void folders() } } }))
 useEffect(() => { onBusyChange?.(busy || choosing) }, [busy, choosing, onBusyChange])
 const button = 'rounded-lg border border-[var(--border-line)] px-3 py-2 text-sm disabled:opacity-40'
 if (compact && !error && !choosing && !status) return null
 return <section aria-label="파일 이동" className="space-y-3">
  {!compact && <div className="flex flex-wrap items-center gap-2">
   <span className="text-sm">게시글 {selection.length}개 선택</span>
   <button className={button} disabled={busy || disabled || !!running || !selection.length} onClick={() => { setChosen(null); setOperation('copy'); void folders() }}>한 폴더에 모으기</button>
   {selection.length > 0 && <button className={button} disabled={busy} onClick={onCreated}>선택 해제</button>}
  </div>}
  {error && <p role="alert" className="text-sm text-red-500">{error}</p>}
  {choosing && <div className="space-y-3 rounded-xl border border-[var(--border-line)] p-4">
   <p className="text-sm">가져오기로 등록한 대상 폴더를 선택하세요. 앨범 구분은 유지합니다.</p>
   <form className="flex flex-wrap gap-2" onSubmit={event => { event.preventDefault(); void folders() }}>
    <input aria-label="대상 폴더 검색" value={keyword} onChange={event => setKeyword(event.target.value)} className="min-w-0 flex-1 rounded border border-[var(--border-line)] bg-[var(--bg-sidebar)] p-2" />
    <button className={button} disabled={busy}>검색</button>
   </form>
   <select aria-label="모으기 방식" value={operation} disabled={busy} onChange={event => setOperation(event.target.value as 'copy' | 'move')} className="w-full rounded border border-[var(--border-line)] bg-[var(--bg-sidebar)] p-2"><option value="copy">복사해서 모으기 · 원본 유지</option><option value="move">이동해서 모으기 · 원본 위치 변경</option></select>
   <select aria-label="이동할 폴더" value={directory} onChange={event => setDirectory(event.target.value)} className="w-full min-w-0 rounded border border-[var(--border-line)] bg-[var(--bg-sidebar)] p-2">
    <option value="">폴더 선택</option>{directories.map(item => <option key={item.id} value={item.id}>{item.directoryPath}</option>)}
   </select>
   {!directories.length && <p className="text-sm">등록된 폴더가 없습니다. 먼저 로컬 폴더 가져오기를 사용하세요.</p>}
   {hasMoreDirectories && <button className={button} disabled={busy} onClick={() => void folders(true)}>폴더 더 불러오기</button>}
   <div className="flex gap-2"><button className={button} disabled={busy || disabled || !directory || !effectiveSelection.length} onClick={() => void start()}>{operation === 'copy' ? '복사 시작' : '이동 시작'}</button><button className={button} disabled={busy} onClick={() => { setChoosing(false); setChosen(null) }}>닫기</button></div>
  </div>}
  {status && hidden !== status.jobId && <div className="space-y-3 rounded-xl border border-[var(--border-line)] bg-[var(--bg-sidebar)] p-4">
   <div className="flex items-center justify-between gap-3"><p className="font-medium">{status.operation === 'copy' ? '복사 · ' : '이동 · '}{moveLabel(status.state)}</p>{!running && <button aria-label="이동 결과 닫기" className={button} onClick={() => setHidden(status.jobId)}>×</button>}</div>
   <p className="break-all text-xs">{status.directory}</p>
   <p className="text-sm">전체 {status.total}개 · 완료 {status.completed}개 · 실패 {status.failed}개 · 원본 정리 대기 {status.cleanupPending}개</p>
   <ProgressBar label="파일 이동 진행률" value={status.totalBytes > 0 ? status.processedBytes / status.totalBytes * 100 : status.total ? (status.completed + status.cleanupPending) / status.total * 100 : 0} />
   <p className="break-all text-xs">{formatBytes(status.processedBytes)} / {formatBytes(status.totalBytes)}{status.current && ` · ${status.current}`}</p>
   <div className="flex flex-wrap gap-2">
    {running ? <button className={button} disabled={busy || status.state === 'pausing'} onClick={() => void action('pause')}>중단</button> : status.state !== 'completed' && <button className={button} disabled={busy || disabled} onClick={() => void action(status.failed || status.cleanupPending ? 'retry' : 'run')}>{status.failed || status.cleanupPending ? '실패·정리 재시도' : '이어하기'}</button>}
    <button className={button} disabled={busy} onClick={() => { setError(''); setRefresh(v => v + 1) }}>상태 새로고침</button>
   </div>
   <details><summary className="cursor-pointer text-sm">파일별 결과</summary><ul className="mt-2 max-h-64 space-y-2 overflow-auto text-xs">{status.items.map(item => <li key={item.id} className="break-all"><span>{moveLabel(item.state)} · {item.source}</span>{item.target && <p>→ {item.target}</p>}{item.error && <p className="text-red-500">{item.error}</p>}</li>)}</ul>
    <div className="mt-2 flex gap-2"><button className={button} disabled={offset === 0} onClick={() => setOffset(v => Math.max(0, v - 100))}>이전 결과</button><button className={button} disabled={!status.hasMore} onClick={() => setOffset(v => v + 100)}>다음 결과</button></div>
   </details>
  </div>}
  {status && hidden === status.jobId && <button className={button} onClick={() => setHidden('')}>최근 이동 결과 보기</button>}
 </section>
}
