import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'
import App from './App'
import { AuthProvider } from './lib/auth'
import { PreferencesProvider } from './lib/preferences'
import { ThemeProvider } from './lib/theme'
import './index.css'

const container = document.getElementById('root')
if (!container) throw new Error('root element is missing from index.html')

createRoot(container).render(
  <StrictMode>
    <BrowserRouter>
      <ThemeProvider>
        <PreferencesProvider>
          <AuthProvider>
            <App />
          </AuthProvider>
        </PreferencesProvider>
      </ThemeProvider>
    </BrowserRouter>
  </StrictMode>,
)
