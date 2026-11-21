import { readdir, readFile } from 'node:fs/promises'
import { extname, join, relative } from 'node:path'
import { fileURLToPath, URL } from 'node:url'
import process from 'node:process'

const root = fileURLToPath(new URL('../src/', import.meta.url))
const allowedFile = 'assets/styles/tokens.css'
const rawColor = /#[\da-f]{3,8}\b|rgba?\([^)]*\)/gi
const violations = []
const sources = []

const visit = async directory => {
  for (const entry of await readdir(directory, { withFileTypes: true })) {
    const path = join(directory, entry.name)
    if (entry.isDirectory()) {
      await visit(path)
      continue
    }
    if (!['.css', '.js', '.vue'].includes(extname(entry.name))) continue
    const filename = relative(root, path)
    const lines = (await readFile(path, 'utf8')).split('\n')
    sources.push({ filename, lines })
    if (filename === allowedFile) continue
    lines.forEach((line, index) => {
      const matches = line.match(rawColor)
      if (matches) violations.push(`${filename}:${index + 1} ${matches.join(', ')}`)
    })
  }
}

await visit(root)
const definitions = new Set(sources.flatMap(({ lines }) => [...lines.join('\n').matchAll(/(--kx-[\w-]+)\s*:/g)].map(match => match[1])))
for (const { filename, lines } of sources) {
  lines.forEach((line, index) => {
    for (const match of line.matchAll(/var\(\s*(--kx-[\w-]+)/g)) {
      if (!definitions.has(match[1])) violations.push(`${filename}:${index + 1} 未定义 ${match[1]}`)
    }
  })
}
if (violations.length) {
  process.stderr.write(`发现 ${violations.length} 处设计 Token 问题：\n`)
  process.stderr.write(`${violations.join('\n')}\n`)
  process.exit(1)
}
process.stdout.write('设计 Token 校验通过：业务源码无直接色值，所有 kx 变量引用均有定义。\n')
