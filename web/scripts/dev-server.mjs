import { createReadStream } from 'node:fs'
import { stat } from 'node:fs/promises'
import { createServer, request as httpRequest } from 'node:http'
import { request as httpsRequest } from 'node:https'
import { extname, resolve, sep } from 'node:path'
import { fileURLToPath } from 'node:url'

const port = Number.parseInt(process.env.WEB_DEV_PORT ?? '5173', 10)
const apiTarget = new URL(process.env.API_PROXY_TARGET ?? 'http://localhost:8080')
const distRoot = fileURLToPath(new URL('../dist/', import.meta.url))
const contentTypes = new Map([
  ['.css', 'text/css; charset=utf-8'],
  ['.html', 'text/html; charset=utf-8'],
  ['.js', 'text/javascript; charset=utf-8'],
  ['.map', 'application/json; charset=utf-8'],
])

function proxyApi(request, response) {
  const target = new URL(request.url ?? '/', apiTarget)
  const transport = target.protocol === 'https:' ? httpsRequest : httpRequest
  const proxy = transport(
    target,
    {
      method: request.method,
      headers: { ...request.headers, host: target.host },
    },
    (upstream) => {
      response.writeHead(upstream.statusCode ?? 502, upstream.headers)
      upstream.pipe(response)
    },
  )

  proxy.on('error', (error) => {
    response.writeHead(502, { 'content-type': 'application/json; charset=utf-8' })
    response.end(JSON.stringify({ error: `Backend proxy failed: ${error.message}` }))
  })
  request.pipe(proxy)
}

async function serveStatic(request, response) {
  const url = new URL(request.url ?? '/', 'http://localhost')
  const pathname = decodeURIComponent(url.pathname)
  const candidate = pathname === '/' ? 'index.html' : pathname.replace(/^\/+/, '')
  let filePath = resolve(distRoot, candidate)

  if (filePath !== distRoot && !filePath.startsWith(`${distRoot}${sep}`)) {
    response.writeHead(400)
    response.end('Bad request')
    return
  }

  try {
    if (!(await stat(filePath)).isFile()) {
      throw new Error('not a file')
    }
  } catch {
    filePath = resolve(distRoot, 'index.html')
  }

  response.writeHead(200, {
    'content-type': contentTypes.get(extname(filePath)) ?? 'application/octet-stream',
    'cache-control': 'no-store',
  })
  createReadStream(filePath).pipe(response)
}

const server = createServer((request, response) => {
  if ((request.url ?? '').startsWith('/api/')) {
    proxyApi(request, response)
    return
  }

  void serveStatic(request, response).catch((error) => {
    response.writeHead(500)
    response.end(error instanceof Error ? error.message : 'Internal server error')
  })
})

server.listen(port, '0.0.0.0', () => {
  console.log(`Web development server listening on http://localhost:${port}`)
  console.log(`Proxying /api to ${apiTarget.origin}`)
})
