import { gzipSync } from 'node:zlib'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

const dist = resolve('dist')
const html = readFileSync(resolve(dist, 'index.html'), 'utf8')
const modernMatch = html.match(/<script type="module"[^>]+src="([^"]+)"/)
const modernScripts = [...html.matchAll(/<script type="module"[^>]+src="([^"]+)"/g)]
const legacyMatch = html.match(/id="vite-legacy-entry"[^>]+data-src="([^"]+)"/)
const cssMatches = [...html.matchAll(/<link rel="stylesheet"[^>]+href="([^"]+)"/g)]

if (!modernMatch || !legacyMatch || cssMatches.length === 0) {
  throw new Error('unable to resolve entry assets from dist/index.html')
}

const gzipKB = asset => gzipSync(readFileSync(resolve(dist, asset.replace(/^\//, '')))).length / 1024
const modernJS = modernScripts.reduce((total, match) => total + gzipKB(match[1]), 0)
const initialCSS = cssMatches.reduce((total, match) => total + gzipKB(match[1]), 0)
const legacyJS = gzipKB(legacyMatch[1])

const modernBaseline = 68.46 + 55.85
const modernBudget = modernBaseline * 0.8
const legacyBaseline = 128.44
const legacyBudget = legacyBaseline * 0.85
const modernEntryTotal = modernJS + initialCSS

console.log(`modern entry JS + initial CSS: ${modernEntryTotal.toFixed(2)} KiB gzip (budget ${modernBudget.toFixed(2)})`)
console.log(`legacy entry JS: ${legacyJS.toFixed(2)} KiB gzip (budget ${legacyBudget.toFixed(2)}, polyfills excluded)`)

if (modernEntryTotal > modernBudget || legacyJS > legacyBudget) {
  process.exitCode = 1
}
