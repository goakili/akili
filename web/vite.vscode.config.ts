// Copyright (c) 2026 Jonas Kaninda.
// SPDX-License-Identifier: MIT

// Fixed file names: the extension loads them into its webview under a strict CSP.
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath } from 'node:url'

export default defineConfig({
  plugins: [vue()],
  base: './',
  build: {
    outDir: fileURLToPath(new URL('../vscode/media/webview', import.meta.url)),
    emptyOutDir: true,
    sourcemap: false,
    chunkSizeWarningLimit: 1500,
    rollupOptions: {
      input: fileURLToPath(new URL('./vscode.html', import.meta.url)),
      output: {
        entryFileNames: 'webview.js',
        assetFileNames: (a) => (a.names?.[0]?.endsWith('.css') ? 'webview.css' : 'assets/[name][extname]'),
        codeSplitting: false,
      },
    },
  },
})
