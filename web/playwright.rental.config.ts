import { defineConfig, devices } from '@playwright/test'
export default defineConfig({
 testDir:'./e2e', testMatch:'rental-workflow.spec.ts', workers:1, fullyParallel:false,
 use:{baseURL:'http://127.0.0.1:4198',screenshot:'only-on-failure'},
 projects:[{name:'desktop',use:{...devices['Desktop Chrome']}},{name:'mobile',use:{...devices['Pixel 5']}}],
 webServer:[{command:'node scripts/rental-test-server.mjs',url:'http://127.0.0.1:18080/api/v1/health',reuseExistingServer:false,timeout:120000},{command:'npm run dev -- --host 127.0.0.1 --port 4198 --strictPort',url:'http://127.0.0.1:4198',reuseExistingServer:false,env:{SKLAD_DEV_BACKEND:'http://127.0.0.1:18080'}}],
})
