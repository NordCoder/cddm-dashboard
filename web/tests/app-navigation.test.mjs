import test from 'node:test'
import assert from 'node:assert/strict'
import React from 'react'
import TestRenderer from 'react-test-renderer'

const { act, create } = TestRenderer

function installGlobal(name, value) {
  Object.defineProperty(globalThis, name, {
    configurable: true,
    writable: true,
    value,
  })
}

function restoreGlobal(name, descriptor) {
  if (descriptor === undefined) {
    delete globalThis[name]
    return
  }
  Object.defineProperty(globalThis, name, descriptor)
}

test('mounted App preserves hook order across Workspace, Project, Work Unit, Plans and Settings routes', async () => {
  let pathname = '/'
  const popstateListeners = new Set()
  const names = ['React', 'location', 'history', 'scrollTo', 'addEventListener', 'removeEventListener', 'fetch']
  const originalDescriptors = new Map(names.map((name) => [name, Object.getOwnPropertyDescriptor(globalThis, name)]))

  installGlobal('React', React)
  installGlobal('location', {
    get pathname() {
      return pathname
    },
  })
  installGlobal('history', {
    pushState(_state, _title, path) {
      pathname = String(path)
    },
  })
  installGlobal('scrollTo', () => {})
  installGlobal('addEventListener', (type, listener) => {
    if (type === 'popstate') popstateListeners.add(listener)
  })
  installGlobal('removeEventListener', (type, listener) => {
    if (type === 'popstate') popstateListeners.delete(listener)
  })
  installGlobal('fetch', (_input, init = {}) => new Promise((_resolve, reject) => {
    const signal = init.signal
    const abort = () => reject(new DOMException('Aborted', 'AbortError'))
    if (signal?.aborted) {
      abort()
      return
    }
    signal?.addEventListener('abort', abort, { once: true })
  }))

  let renderer
  try {
    const { App } = await import('../dist/assets/app.js')

    await act(async () => {
      renderer = create(React.createElement(App))
      await Promise.resolve()
    })

    const assertCurrentView = (label) => {
      assert.ok(JSON.stringify(renderer.toJSON()).includes(label), `expected mounted view to include ${label}`)
    }

    const transition = async (path, label) => {
      pathname = path
      await act(async () => {
        for (const listener of [...popstateListeners]) listener()
        await Promise.resolve()
      })
      assertCurrentView(label)
    }

    assertCurrentView('Workspace')
    await transition('/projects/1', 'Project 1')
    await transition('/projects/1/work-units/14', 'Issue #14')
    await transition('/projects/1/work-units/14/plans', 'Plans')
    await transition('/settings', 'Settings / Health')
  } finally {
    if (renderer) {
      await act(async () => {
        renderer.unmount()
      })
    }
    for (const [name, descriptor] of originalDescriptors) {
      restoreGlobal(name, descriptor)
    }
  }
})
