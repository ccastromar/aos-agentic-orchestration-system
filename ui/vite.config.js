import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/ask': 'http://localhost:8088',
      '/task': 'http://localhost:8088',
      '/ui/task': 'http://localhost:8088',
      '/human_approve': 'http://localhost:8088',
      '/human_reject': 'http://localhost:8088',
      '/intents': 'http://localhost:8088',
    }
  }
})
