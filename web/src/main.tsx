import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import '@fontsource-variable/inter'
import App from './App'
import { UnitsProvider } from './lib/units'
import './styles.css'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <UnitsProvider>
      <App />
    </UnitsProvider>
  </StrictMode>,
)
