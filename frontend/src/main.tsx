/*
 * main.tsx
 * The primary entry point for the React application.
 * It mounts the root App component into the DOM under StrictMode for standard validations.
 */
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './index.css'
import App from './App.tsx'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
