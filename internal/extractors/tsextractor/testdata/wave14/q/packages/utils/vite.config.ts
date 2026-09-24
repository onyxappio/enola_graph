import { resolve } from 'node:path'
import { defineConfig } from 'vite'
import { externalizeDeps } from 'vite-plugin-externalize-deps'

const isDev = process.env.NODE_ENV
export default defineConfig({
  plugins: [
    externalizeDeps({
      include: [/node_modules/],
    }),
  ],
  build: {
    lib: {
      entry: resolve(__dirname, './src/index.ts'),
      name: 'utils',
      fileName: 'index',
      formats: ['es', 'cjs'],
      cssFileName: 'style',
    },
    minify: !isDev,
  },
})
