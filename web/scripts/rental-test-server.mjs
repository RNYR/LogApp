import { mkdtemp, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'
import { spawn } from 'node:child_process'
const dir = await mkdtemp(join(tmpdir(), 'sklad-rental-test-'))
const go = process.env.GO_BINARY || 'go'
const binary = join(dir, 'sklad')
const build = spawn(go, ['build', '-o', binary, './cmd/sklad'], { cwd: resolve('../server'), stdio: 'inherit' })
const code = await new Promise(resolve => build.on('exit', resolve))
if (code !== 0) { await rm(dir, { recursive: true, force: true }); process.exit(code || 1) }
const server = spawn(binary, [], { stdio: 'inherit', env: { ...process.env,
 SKLAD_HTTP_ADDR: '127.0.0.1:18080', SKLAD_DATA_DIR: dir, SKLAD_DB_PATH: join(dir, 'sklad.db'),
 SKLAD_BASE_URL: 'http://127.0.0.1:4198', SKLAD_ADMIN_USERNAME: 'admin',
 SKLAD_ADMIN_PASSWORD: 'rental-integration-pass', SKLAD_COOKIE_SECRET: 'isolated-integration-test-cookie-secret-only',
} })
for (const signal of ['SIGINT', 'SIGTERM']) process.on(signal, () => server.kill(signal))
server.on('exit', async code => { await rm(dir, { recursive: true, force: true }); process.exit(code || 0) })
