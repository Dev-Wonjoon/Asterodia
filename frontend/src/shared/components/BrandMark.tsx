import mark from '@renderer/assets/asterodia.svg?url'

/** Preserve the supplied colors and transparency in either theme. */
export function BrandMark({ className = 'size-6' }: { className?: string }) {
    return <img src={mark} alt="" aria-hidden="true" draggable={false}
        width={24} height={24} className={`inline-block shrink-0 object-contain ${className}`} />
}
