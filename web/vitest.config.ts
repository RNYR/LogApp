import { configDefaults, defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'

/**
 * Vitest configuration.
 *
 * Unit tests live next to the code under `src/`; the Playwright end-to-end specs
 * under `e2e/` are explicitly excluded so `npm test` never picks them up.
 */
export default defineConfig({
  plugins: [react()],
  test: {
    environment: 'node',
    include: ['src/**/*.{test,spec}.{ts,tsx}'],
    exclude: [...configDefaults.exclude, 'e2e/**', 'dist/**'],
    passWithNoTests: true,
  },
})
