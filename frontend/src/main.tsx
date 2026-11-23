import { StrictMode } from 'react'
import { QueryClientProvider } from '@tanstack/react-query'
import { BrowserRouter } from 'react-router-dom'
import { createRoot as CreateRoot } from 'react-dom/client'
import '@fontsource-variable/inter'
import '@fontsource-variable/geist-mono'
import { App } from './App'
import { QueryClientInstance } from './auth/query'
import './styles/index.css'

CreateRoot(document.getElementById('root')!).render(
  <StrictMode>
    <QueryClientProvider client={QueryClientInstance}>
      <BrowserRouter><App /></BrowserRouter>
    </QueryClientProvider>
  </StrictMode>,
)
