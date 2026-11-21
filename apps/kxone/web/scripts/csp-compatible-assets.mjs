import { createHash } from 'node:crypto'

// Vite 8 legacy detection imports data: modules, which our production CSP blocks.
// Keep the same feature test and per-entry identity, but serve it from this origin.
export function cspCompatibleAssets() {
  const probe = 'if(!import.meta.resolve)throw Error("import.meta.resolve not supported")'
  const assetName = (name, source) => `assets/${name}-${createHash('sha256').update(source).digest('hex').slice(0, 12)}.js`
  const probeName = assetName('browser-support', probe)
  const replaceProbe = (code) => code.replace(
    /import\s*'data:text\/javascript,([^']*import\.meta\.resolve[^']*)';?/g,
    (_, source) => `import '/${probeName}?probe=${createHash('sha256').update(source).digest('hex').slice(0, 12)}';`
  )

  return {
    name: 'kxone-csp-compatible-assets',
    apply: 'build',
    enforce: 'post',
    transform: {
      order: 'pre',
      handler(code, id) {
        if (!/\/vant\/(es|lib)\/icon\/index\.css$/.test(id)) return null
        // Vant already embeds WOFF2, supported by our Safari 12+ baseline.
        // Remove its optional remote fallback so icon rendering stays self-contained.
        return code.replace(/,url\(\/\/at\.alicdn\.com\/[^)]+\)\s*format\("woff"\)/g, '')
      }
    },
    renderChunk: {
      order: 'post',
      handler(code) {
        const next = replaceProbe(code)
        return next === code ? null : { code: next, map: null }
      }
    },
    generateBundle: {
      order: 'post',
      handler(_, bundle) {
        for (const item of Object.values(bundle)) {
          if (item.type !== 'asset' || !item.fileName.endsWith('.html')) continue
          let usesProbe = false
          item.source = String(item.source).replace(/<script([^>]*)>([\s\S]*?)<\/script>/g, (tag, attributes, source) => {
            if (!source.trim() || /(?:^|\s)src=/.test(attributes)) return tag
            const next = replaceProbe(source)
            usesProbe ||= next !== source
            const fileName = assetName('browser-bootstrap', next)
            this.emitFile({ type: 'asset', fileName, source: next })
            return `<script${attributes} src="/${fileName}"></script>`
          })
          if (usesProbe) this.emitFile({ type: 'asset', fileName: probeName, source: probe })
        }
      }
    }
  }
}
