import { useEffect } from 'react'
import { createPortal } from 'react-dom'

interface BaseModalProps {
    layer?: number
    onClose?: () => void
    closeOnOverlay?: boolean
    position?: 'center' | { x: number; y: number }
    containerRef?: React.RefObject<HTMLDivElement | null>
    overlayClassName?: string
    children: React.ReactNode
}

export const BaseModal = ({
    layer = 99998,
    onClose,
    closeOnOverlay = true,
    position = 'center',
    containerRef,
    overlayClassName = 'bg-black/50',
    children
}: BaseModalProps): React.JSX.Element => {
    // ESC 키로 닫기
    useEffect(() => {
        if (!onClose) return
        const handleEsc = (e: KeyboardEvent): void => {
            if (e.key === 'Escape' && !e.defaultPrevented && !e.isComposing && e.keyCode !== 229) onClose()
        }
        document.addEventListener('keydown', handleEsc)
        return () => document.removeEventListener('keydown', handleEsc)
    }, [onClose])

    const isCenter = position === 'center'

    return createPortal(
        <>
            {/* 오버레이 */}
            <div
                className={`motion-backdrop fixed inset-0 z-[99998] ${overlayClassName}`}
                style={{ zIndex: layer }}
                onClick={closeOnOverlay ? onClose : undefined}
            />
            {/* 컨테이너 */}
            {isCenter ? (
                <div style={{ zIndex: layer + 1 }} className="fixed inset-0 z-[99999] flex items-center justify-center pointer-events-none">
                    <div
                        className="motion-modal pointer-events-auto bg-[var(--bg-popup)] shadow-[0_8px_32px_rgba(0,0,0,0.4)] rounded-2xl overflow-hidden"
                        onClick={(e) => e.stopPropagation()}
                    >
                        {children}
                    </div>
                </div>
            ) : (
                <div
                    ref={containerRef}
                    className="motion-menu fixed z-[99999] bg-[var(--bg-popup)] shadow-[0_8px_32px_rgba(0,0,0,0.4)] rounded-lg"
                    style={{
                        zIndex: layer + 1,
                        left: (position as { x: number; y: number }).x,
                        top: (position as { x: number; y: number }).y
                    }}
                    onClick={(e) => e.stopPropagation()}
                >
                    {children}
                </div>
            )}
        </>,
        document.body
    )
}
