import { type ButtonHTMLAttributes, type ReactElement } from 'react'
import { ActionContent } from './ActionButton'

interface IconButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
    size?: 'sm' | 'md'
    active?: boolean
}

export const IconButton = ({
    size = 'md',
    active = false,
    className = '',
    children,
    title,
    ...props
}: IconButtonProps): ReactElement => {
    const sizes = {
        sm: 'min-h-8 min-w-8 px-2',
        md: 'min-h-9 min-w-9 px-2'
    }

    const state = active
        ? 'border-slate-900 bg-slate-900 text-white dark:border-slate-100 dark:bg-slate-100 dark:text-slate-950'
        : 'border-slate-200 bg-white text-slate-500 hover:border-slate-300 hover:text-slate-950 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-400 dark:hover:border-slate-600 dark:hover:text-slate-100'

    return (
        <button
            type="button"
            title={title}
            className={`motion-control inline-grid place-items-center rounded-lg border disabled:cursor-not-allowed disabled:opacity-50 ${sizes[size]} ${state} ${className}`}
            {...props}
        >
            {title ? <ActionContent icon={children} label={title} /> : children}
        </button>
    )
}
