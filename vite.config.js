import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/auth': 'http://localhost:8080',
      '/ask': 'http://localhost:8080',
      '/task': 'http://localhost:8080',
      '/api': 'http://localhost:8080',
      '/human_approve': 'http://localhost:8080',
      '/human_reject': 'http://localhost:8080',
      '/intents': 'http://localhost:8080',
      '/ui/pipelines': 'http://localhost:8080',
      '/ui/settings': 'http://localhost:8080',
    }
  }
})
