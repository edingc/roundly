import { createContext, useCallback, useContext, useState, type ReactNode } from 'react'

/**
 * Display preferences for the scorecard. Client-side only (like theme), so
 * there is no account sync across devices — these are just how this browser
 * likes the grid labeled.
 */
export type StrokeIndexLabel = 'HDCP' | 'SI'

const STORAGE_KEY = 'roundly.stroke_index_label'
const DEFAULT_LABEL: StrokeIndexLabel = 'HDCP'

interface PreferencesState {
  strokeIndexLabel: StrokeIndexLabel
  setStrokeIndexLabel: (label: StrokeIndexLabel) => void
}

const PreferencesContext = createContext<PreferencesState | null>(null)

function readStored(): StrokeIndexLabel {
  try {
    const stored = localStorage.getItem(STORAGE_KEY)
    if (stored === 'HDCP' || stored === 'SI') return stored
  } catch {
    // Storage can be unavailable; fall through to the default.
  }
  return DEFAULT_LABEL
}

export function PreferencesProvider({ children }: { children: ReactNode }) {
  const [strokeIndexLabel, setStrokeIndexLabelState] = useState<StrokeIndexLabel>(readStored)

  const setStrokeIndexLabel = useCallback((next: StrokeIndexLabel) => {
    setStrokeIndexLabelState(next)
    try {
      localStorage.setItem(STORAGE_KEY, next)
    } catch {
      // Not persisting is acceptable; the choice still applies for this session.
    }
  }, [])

  return (
    <PreferencesContext.Provider value={{ strokeIndexLabel, setStrokeIndexLabel }}>
      {children}
    </PreferencesContext.Provider>
  )
}

export function usePreferences(): PreferencesState {
  const context = useContext(PreferencesContext)
  if (!context) throw new Error('usePreferences must be used inside a PreferencesProvider')
  return context
}
