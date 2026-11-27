import mark from '@renderer/assets/asterodia.svg?url'

/** The transparent source keeps its shape; currentColor follows the active theme. */
export function BrandMark({ className = 'size-6' }: { className?: string }) {
    return <span aria-hidden="true" className={`inline-block shrink-0 bg-current ${className}`} style={{
        maskImage: `url("${mark}")`, WebkitMaskImage: `url("${mark}")`,
        maskSize: 'contain', WebkitMaskSize: 'contain',
        maskRepeat: 'no-repeat', WebkitMaskRepeat: 'no-repeat',
        maskPosition: 'center', WebkitMaskPosition: 'center',
    }} />
}
