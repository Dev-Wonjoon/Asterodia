import { getApi } from '@renderer/shared/api'
import type { LicenseInfo } from '@contracts/types'
import { useCallback, useEffect, useState } from 'react'

export const useLicenseSettingsViewModel = () => {
    const [licenses, setLicenses] = useState<LicenseInfo[]>([])
    const [expanded, setExpanded] = useState<string | null>(null)
    const [loading, setLoading] = useState(true)
    const [error, setError] = useState(false)
    const [attempt, setAttempt] = useState(0)
    const [query, setQuery] = useState('')

    useEffect(() => {
        let active = true
        setLoading(true)
        setError(false)
        getApi().getEngineLicenses()
            .then(value => { if (active) setLicenses(value) })
            .catch(() => { if (active) setError(true) })
            .finally(() => { if (active) setLoading(false) })
        return () => { active = false }
    }, [attempt])

    const toggle = useCallback((id: string) => {
        setExpanded(previous => previous === id ? null : id)
    }, [])
    const retry = useCallback(() => setAttempt(value => value + 1), [])
    const search = query.trim().toLocaleLowerCase()
    const filtered = licenses.filter(item => `${item.name} ${item.license} ${item.version}`.toLocaleLowerCase().includes(search))
    return { licenses: filtered, expanded, toggle, loading, error, retry, query, setQuery }
}
