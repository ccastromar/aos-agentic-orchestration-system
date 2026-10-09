import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// Keep the dev proxy aligned with AOS' default HTTP port. Override with
// AOS_BACKEND_URL when the backend is started on another port/host.
const AOS_BACKEND_URL = process.env.AOS_BACKEND_URL || 'http://localhost:9090';

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/auth': AOS_BACKEND_URL,
      '/ask': AOS_BACKEND_URL,
      '/task': AOS_BACKEND_URL,
      '/api': AOS_BACKEND_URL,
      '/human_approve': AOS_BACKEND_URL,
      '/human_reject': AOS_BACKEND_URL,
      '/intents': AOS_BACKEND_URL,
      '/ui/pipelines': AOS_BACKEND_URL,
      '/ui/settings': AOS_BACKEND_URL,
    }
  }
})
