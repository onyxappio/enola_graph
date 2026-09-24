/// <reference types="vitest" />
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import UnoCSS from 'unocss/vite'
import { externalizeDeps } from 'vite-plugin-externalize-deps'

const __filename = fileURLToPath(import.meta.url)
const __dirname = dirname(__filename)
const isDev = process.env.NODE_ENV

export default defineConfig({
  plugins: [
    vue(),
    UnoCSS(),
    externalizeDeps({
      include: [/node_modules/, '~/assets/imageMap'],
    }),
  ],
  assetsInclude: ['**/**.png'],
  build: {
    lib: {
      entry: {
        index: resolve(__dirname, 'src/index.ts'),
        utils: resolve(__dirname, 'src/index.utils.ts'),
        consts: resolve(__dirname, 'src/index.consts.ts'),
      },
      name: 'shared-lands-components',
      fileName: (fmt, name) => `${name}/index.${fmt}.js`,
      formats: ['es'],
      cssFileName: 'style',
    },
    minify: !isDev,
    cssCodeSplit: false,
    reportCompressedSize: true,
    rollupOptions: {
      output: {
        assetFileNames: (assetInfo) => {
          return assetInfo.name === 'style.css' ? 'style.css' : (assetInfo.name ?? 'assets/[name]-[hash][extname]')
        },
        chunkFileNames: (chunkInfo) => {
          const facadeModuleId = chunkInfo.facadeModuleId ? chunkInfo.facadeModuleId.split('/') : []
          const fileName = facadeModuleId[facadeModuleId.length - 2] || '[name]'

          return `js/${fileName}/[name].[hash].js`
        },
      },
    },
  },
  test: {
    includeSource: ['src/**/*.{js,ts}'],
    exclude: ['src/**/*.pw.spec.ts'],
  },
  define: {
    'import.meta.vitest': 'undefined',
  },
})
