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
