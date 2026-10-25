import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    // `pnpm dev` talks to a server started with `make run`.
    proxy: { '/api': 'http://localhost:7171' },
  },
  build: { outDir: 'dist', emptyOutDir: false },
  test: { environment: 'jsdom' },
})
