import { defineConfig as DefineConfig } from '@playwright/test'

export default DefineConfig({
  testDir: './e2e',
  timeout: 120_000,
  workers: 1,
  reporter: 'list',
  outputDir: './test-results',
  use: {
    baseURL: process.env.APP_ORIGIN || 'http://127.0.0.1:5173',
    browserName: 'chromium',
    channel: 'chromium',
    headless: true,
    actionTimeout: 10_000,
    navigationTimeout: 15_000,
    trace: 'off',
    screenshot: 'off',
    video: 'off',
    viewport: { width: 1366, height: 900 },
  },
})
