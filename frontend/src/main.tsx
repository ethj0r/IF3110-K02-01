import { StrictMode } from 'react'
import { createRoot as CreateRoot } from 'react-dom/client'
import { App } from './App'
import './style.css'

CreateRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
