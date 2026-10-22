import { defineConfig as DefineConfig, loadEnv as LoadEnv } from 'vite'
import React from '@vitejs/plugin-react'

export default DefineConfig(({ mode: Mode }) => {
  const Env = LoadEnv(Mode, '..', 'HTTP_')
  return {
    plugins: [React()],
    server: {
      host: '127.0.0.1',
      port: 5173,
      strictPort: true,
      proxy: {
        '/api': { target: `http://${Env.HTTP_ADDR || '127.0.0.1:8080'}` },
      },
    },
    build: {
      outDir: 'dist',
    },
  }
})
