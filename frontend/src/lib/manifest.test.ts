import { describe, expect, it } from 'bun:test'
import { join } from 'node:path'

// The installable app: public/manifest.webmanifest and the tags in index.html
// that point at it. Read from disk, as the browser gets them.
const root = join(import.meta.dir, '..', '..')
const publicDir = join(root, 'public')

/** Width and height from a PNG's IHDR chunk. */
async function pngSize(path: string): Promise<[number, number]> {
  const bytes = new DataView(await Bun.file(path).arrayBuffer())
  expect(bytes.getUint32(0)).toBe(0x89504e47) // \x89PNG
  return [bytes.getUint32(16), bytes.getUint32(20)]
}

describe('web app manifest', () => {
  it('is valid JSON with the fields an install needs', async () => {
    const manifest = JSON.parse(await Bun.file(join(publicDir, 'manifest.webmanifest')).text())
    // Launchers cut longer names under the icon. Checked first: bun's
    // toMatchObject writes the asymmetric matchers into the object it checks.
    expect(typeof manifest.short_name === 'string' && manifest.short_name.length <= 12).toBe(true)
    expect(manifest).toMatchObject({
      name: 'YTMDL',
      short_name: expect.any(String),
      display: 'standalone',
      start_url: '/listen',
      scope: '/',
      background_color: expect.stringMatching(/^#[0-9a-f]{6}$/),
      theme_color: expect.stringMatching(/^#[0-9a-f]{6}$/),
    })
  })

  it('names icons that exist, at the sizes they claim, with a maskable one', async () => {
    const manifest = JSON.parse(await Bun.file(join(publicDir, 'manifest.webmanifest')).text()) as {
      icons: { src: string; sizes: string; type: string; purpose?: string }[]
    }
    const sizes = new Set<string>()
    for (const icon of manifest.icons) {
      const file = join(publicDir, icon.src.replace(/^\//, ''))
      expect({ src: icon.src, exists: await Bun.file(file).exists() }).toEqual({ src: icon.src, exists: true })
      expect(icon.type).toBe('image/png')
      const [width, height] = await pngSize(file)
      expect(`${width}x${height}`).toBe(icon.sizes)
      sizes.add(`${icon.sizes} ${icon.purpose ?? 'any'}`)
    }
    expect([...sizes].sort()).toEqual(['192x192 any', '512x512 any', '512x512 maskable'])
  })
})

describe('index.html', () => {
  it('links the manifest and lets the page reach the safe areas', async () => {
    const html = await Bun.file(join(root, 'index.html')).text()
    expect(html).toContain('<link rel="manifest" href="/manifest.webmanifest" />')
    expect(html).toMatch(/<meta name="viewport" content="[^"]*viewport-fit=cover/)
    expect(html).toMatch(/<meta name="apple-mobile-web-app-capable" content="yes"/)
    expect(html).toMatch(/<meta name="apple-mobile-web-app-status-bar-style" content="[^"]+"/)

    const touchIcon = html.match(/<link rel="apple-touch-icon"[^>]*href="([^"]+)"/)?.[1]
    expect(touchIcon).toBeDefined()
    expect(await pngSize(join(publicDir, touchIcon!.replace(/^\//, '')))).toEqual([180, 180])
  })

  it('uses the manifest theme colour, which is the page background', async () => {
    const html = await Bun.file(join(root, 'index.html')).text()
    const manifest = JSON.parse(await Bun.file(join(publicDir, 'manifest.webmanifest')).text())
    expect(html).toContain(`<meta name="theme-color" content="${manifest.theme_color}" />`)
    expect(manifest.background_color).toBe(manifest.theme_color)
  })
})
