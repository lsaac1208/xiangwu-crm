import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'
import legacy from '@vitejs/plugin-legacy'
import AutoImport from 'unplugin-auto-import/vite'
import Components from 'unplugin-vue-components/vite'
import { VantResolver } from '@vant/auto-import-resolver'
import { fileURLToPath, URL } from 'node:url'
import { cspCompatibleAssets } from './scripts/csp-compatible-assets.mjs'

const isVitest = Boolean(process.env.VITEST)

export default defineConfig({
  plugins: [
    vue(),
    !isVitest && AutoImport({ resolvers: [VantResolver()], dts: false }),
    !isVitest && Components({ resolvers: [VantResolver()], dts: false }),
    legacy({
      targets: ['iOS >= 12', 'Safari >= 12'],
      additionalLegacyPolyfills: ['core-js/proposals/global-this']
    }),
    cspCompatibleAssets()
  ].filter(Boolean),
  build: {
    cssTarget: 'safari12'
  },
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url))
    }
  },
  server: {
    host: '127.0.0.1',
    port: 5173,
    proxy: {
      '/api': {
        target: process.env.VITE_API_PROXY_TARGET || 'http://localhost:8080',
        changeOrigin: true
      }
    }
  },
  test: {
    environment: 'jsdom',
    include: ['src/**/*.test.js'],
    deps: {
      web: { transformAssets: true, transformCss: true }
    },
    coverage: {
      provider: 'v8',
      reporter: ['text', 'json-summary'],
      reportsDirectory: 'coverage',
      include: ['src/**/*.{js,vue}'],
      exclude: ['src/**/*.test.js'],
      reportOnFailure: true
    }
  }
})
