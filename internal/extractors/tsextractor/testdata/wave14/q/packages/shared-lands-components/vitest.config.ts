import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'
import { resolve } from 'node:path'

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      utils: resolve(__dirname, '../utils/src/index.ts'),
      // Landing apps provide this; unit tests only need a stub.
      '~/assets/imageMap': resolve(__dirname, 'src/utils/test/imageMap.stub.ts'),
    },
  },
  test: {
    globals: true,
    environment: 'jsdom',
    include: ['src/**/*.spec.ts'],
    exclude: ['**/*.pw.spec.ts', 'node_modules/**/*.spec.ts'],
  },
})
