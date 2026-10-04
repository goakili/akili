// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

import { defineConfig, type Plugin } from 'vite'
import vue from '@vitejs/plugin-vue'
import { writeFileSync, mkdirSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

const outDir = fileURLToPath(new URL('../server/internal/web/dist', import.meta.url))

// Go embeds server/internal/web/dist; emptyOutDir wipes it, so put the committed .gitkeep back.
function keepGitkeep(): Plugin {
  return {
    name: 'akili-keep-gitkeep',
    apply: 'build',
    closeBundle() {
      mkdirSync(outDir, { recursive: true })
      writeFileSync(`${outDir}/.gitkeep`, '')
    },
  }
}

const backend = process.env.AKILI_API ?? 'http://localhost:9000'

export default defineConfig({
  plugins: [vue(), keepGitkeep()],
  build: {
    outDir,
    emptyOutDir: true,
    sourcemap: false,
    chunkSizeWarningLimit: 800,
  },
  server: {
    port: 5173,
    proxy: {
      // ws: the agent terminal is a WebSocket under /api.
      '/api': { target: backend, changeOrigin: false, ws: true },
      '/install-agent.sh': { target: backend },
      '/docs': { target: backend },
    },
  },
})
