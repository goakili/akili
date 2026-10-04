// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

// The VS Code sidebar bundle (src/vscode): one script and one stylesheet with fixed names, which the
// extension loads into its webview under a strict CSP.
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
