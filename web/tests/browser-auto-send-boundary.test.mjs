import test from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'

const read = (path) => readFile(new URL(`../${path}`, import.meta.url), 'utf8')

test('automatic delivery boundary loads before the sender', async () => {
  const index = await read('src/index.html')
  const boundary = index.indexOf('/assets/browser-auto-send-boundary.js')
  const sender = index.indexOf('/assets/browser-auto-send.js')
  assert.ok(boundary >= 0 && sender > boundary)
})

test('manual confirmation and automatic delivery are mutually exclusive', async () => {
  const [boundary, styles] = await Promise.all([
    read('src/browser-auto-send-boundary.ts'),
    read('src/auto-send.css'),
  ])
  assert.match(boundary, /manualReviewOpen/)
  assert.match(boundary, /stopImmediatePropagation/)
  assert.match(boundary, /delivery-auto-send-enabled/)
  assert.match(styles, /body\.delivery-auto-send-enabled[\s\S]*delivery-section:nth-of-type\(2\)/)
})
