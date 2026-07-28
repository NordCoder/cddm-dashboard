import test from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'

const read = (path) => readFile(new URL(`../${path}`, import.meta.url), 'utf8')

test('frontend UI is split into bounded feature modules', async () => {
  const barrel = await read('src/ui.ts')
  assert.match(barrel, /ui-shared\.js/)
  assert.match(barrel, /ui-workspace\.js/)
  assert.match(barrel, /ui-work-unit\.js/)
  assert.match(barrel, /ui-planning\.js/)
  assert.match(barrel, /ui-settings\.js/)
  assert.ok(barrel.split('\n').length < 12, 'ui.ts must remain a barrel, not regrow into a monolith')
})

test('app entrypoint remains route composition rather than page implementation', async () => {
  const app = await read('src/app.ts')
  assert.match(app, /app-routing\.js/)
  assert.match(app, /pages-workspace\.js/)
  assert.match(app, /pages-work-unit\.js/)
  assert.match(app, /pages-settings\.js/)
  assert.ok(app.split('\n').length < 90, 'app.ts must remain a bounded composition root')
  assert.doesNotMatch(app, /createProject|generatePlan|planningContext/)
})

test('workspace navigation exposes an explicit current-page state', async () => {
  const shared = await read('src/ui-shared.ts')
  assert.match(shared, /aria-current/)
  assert.match(shared, /activeSection/)
  assert.match(shared, /current: item\.section === activeSection/)
})

test('browser delivery has no inline visual theme or generic DOM fallbacks', async () => {
  const controller = await read('src/browser-delivery.ts')
  const view = await read('src/browser-delivery-view.ts')
  const styles = await read('src/styles.css')
  assert.doesNotMatch(controller, /const style\s*=/)
  assert.doesNotMatch(view, /element\.style|style\.cssText|<style/i)
  assert.doesNotMatch(view, /contenteditable|form button\[type=['"]submit/)
  assert.match(styles, /\.delivery-inspector/)
  assert.match(styles, /body\.delivery-inspector-open/)
})

test('mobile workspace is a first-class built asset with safe touch and viewport contracts', async () => {
  const index = await read('src/index.html')
  const mobile = await read('src/mobile.css')
  const assets = await read('scripts/copy-assets.mjs')

  assert.match(index, /viewport-fit=cover/)
  assert.match(index, /assets\/mobile\.css/)
  assert.match(assets, /src\/mobile\.css/)
  assert.match(assets, /assets\/mobile\.css/)

  assert.match(mobile, /@media \(max-width: 800px\)/)
  assert.match(mobile, /safe-area-inset-top/)
  assert.match(mobile, /safe-area-inset-bottom/)
  assert.match(mobile, /100dvh/)
  assert.match(mobile, /\.primary-nav[\s\S]*grid-template-columns: repeat\(2/)
  assert.match(mobile, /\.delivery-inspector[\s\S]*width: 100vw/)
  assert.match(mobile, /min-height: 44px/)
  assert.match(mobile, /font-size: 16px/)
  assert.match(mobile, /overflow-x: clip/)
})

test('production and development servers apply the strict frontend header set', async () => {
  const nginx = await read('nginx.conf')
  const devServer = await read('scripts/dev-server.mjs')
  for (const value of [
    "default-src 'self'",
    "object-src 'none'",
    "frame-ancestors 'none'",
    'Cross-Origin-Opener-Policy',
    'X-Content-Type-Options',
    'Permissions-Policy',
  ]) assert.match(nginx, new RegExp(value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'), 'i'))
  assert.match(devServer, /content-security-policy/)
  assert.match(devServer, /cross-origin-opener-policy/)
  assert.match(devServer, /x-content-type-options/)
  assert.match(devServer, /permissions-policy/)
})
