import { useEffect, useState } from 'react'

export type Theme = 'day' | 'night'

const KEY = 'fmc-theme'

function initialTheme(): Theme {
  try {
    const saved = window.localStorage.getItem(KEY)
    if (saved === 'day' || saved === 'night') return saved
  } catch {
    // Storage blocked (private mode etc.): fall back to the OS preference
  }
  return window.matchMedia?.('(prefers-color-scheme: dark)').matches ? 'night' : 'day'
}

/** Day / Night display mode, like the vehicle's own screen. Applied as <html data-theme="…"> */
export function useTheme(): [Theme, () => void] {
  const [theme, setTheme] = useState<Theme>(initialTheme)

  useEffect(() => {
    document.documentElement.dataset.theme = theme
    try {
      window.localStorage.setItem(KEY, theme)
    } catch {
      // Non-essential: the toggle still works for this session
    }
  }, [theme])

  return [theme, () => setTheme((t) => (t === 'day' ? 'night' : 'day'))]
}
