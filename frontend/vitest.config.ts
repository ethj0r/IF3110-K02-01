import { defineConfig as DefineConfig } from 'vitest/config'
import React from '@vitejs/plugin-react'

export default DefineConfig({
  plugins: [React()],
  test: {
    environment: 'jsdom',
    include: ['tests/**/*.test.tsx'],
    restoreMocks: true,
  },
})
