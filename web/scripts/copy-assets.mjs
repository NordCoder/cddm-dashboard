import { copyFile, mkdir } from 'node:fs/promises'

const projectRoot = new URL('../', import.meta.url)
const distRoot = new URL('dist/', projectRoot)
const vendorRoot = new URL('vendor/', distRoot)

await mkdir(vendorRoot, { recursive: true })
await copyFile(new URL('src/index.html', projectRoot), new URL('index.html', distRoot))
await copyFile(new URL('src/styles.css', projectRoot), new URL('assets/styles.css', distRoot))
await copyFile(
  new URL('node_modules/react/umd/react.production.min.js', projectRoot),
  new URL('react.production.min.js', vendorRoot),
)
await copyFile(
  new URL('node_modules/react-dom/umd/react-dom.production.min.js', projectRoot),
  new URL('react-dom.production.min.js', vendorRoot),
)
