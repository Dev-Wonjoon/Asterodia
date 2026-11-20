export function ProgressBar({ label, value, failed = false }: {
    label: string; value?: number; failed?: boolean
}) {
    const percent = value === undefined ? undefined : Math.max(0, Math.min(100, value))
    return <div role="progressbar" aria-label={label} aria-valuemin={0} aria-valuemax={100} aria-valuenow={percent}
        className="h-2 w-full overflow-hidden rounded-full bg-[var(--bg-active)]">
        <div className={`motion-progress h-full rounded-full ${failed ? 'bg-red-500' : 'bg-[var(--color-primary)]'} ${percent === undefined ? 'w-1/3 animate-pulse motion-reduce:animate-none' : ''}`}
            style={percent === undefined ? undefined : { width: `${percent}%` }} />
    </div>
}
