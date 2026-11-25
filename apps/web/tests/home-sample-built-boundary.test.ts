import { execFileSync } from 'node:child_process'
import { mkdtempSync, cpSync, readFileSync, readdirSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { expect, it } from 'vitest'
import raw from '../app/data/sample-result.json'

const marker = 'FORBIDDEN_STORED_MARKER_9F3E'
const liveMarker = 'FORBIDDEN_LIVE_MARKER_7B2D'
function files(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap(entry => entry.isDirectory() ? files(join(dir, entry.name)) : [join(dir, entry.name)])
}

it('keeps forbidden stored and hostile live fields out of client assets and hydration', () => {
  const folder = mkdtempSync(join(tmpdir(), 'sample-boundary-'))
  try {
    cpSync('.output/server', folder, { recursive: true })
    const chunk = join(folder, 'chunks/routes/home-sample.json.get.mjs')
    const code = readFileSync(chunk, 'utf8')
    expect(code).toContain('anchor: "chuong-ii",')
    writeFileSync(chunk, code.replace('anchor: "chuong-ii",', `anchor: "${marker}",`))
    const script = `
      import { Server } from 'node:http';
      import { pathToFileURL } from 'node:url';
      Server.prototype.listen = function () { return this };
      process.env.NITRO_SHUTDOWN_DISABLED = '1';
      const sample = ${JSON.stringify({ release: raw.release, abstain: false, hits: [{ ...raw.hits[0], text: liveMarker }], documents: raw.documents })};
      let sampleCalls = 0;
      globalThis.fetch = async input => {
        const path = new URL(typeof input === 'string' ? input : input.url).pathname;
        if (path.endsWith('/sample')) sampleCalls++;
        return path.endsWith('/sample')
          ? new Response(JSON.stringify(sample), { headers: { 'content-type': 'application/json', 'x-sample-status-date': '2026-11-16' } })
          : new Response(JSON.stringify({ release: sample.release, documents: 1, provisions: 1, nodes: 1, gaps: {} }), { headers: { 'content-type': 'application/json' } });
      };
      const mod = await import(pathToFileURL(process.argv[1]).href);
      const app = Object.values(mod).find(value => typeof value === 'function' && value.name === 'useNitroApp')();
      const response = await app.localFetch('/');
      console.log(JSON.stringify({ status: response.status, html: await response.text(), sampleCalls }));
    `
    const output = JSON.parse(execFileSync(process.execPath, ['--input-type=module', '-e', script, join(folder, 'chunks/nitro/nitro.mjs')], { encoding: 'utf8' }))
    expect(output.status).toBe(200)
    expect(output.sampleCalls).toBe(1)
    expect(output.html).toContain('Mẫu đã lưu')
    const hiddenHit = raw.hits[1]!.snippet!
    const payload = /id="__NUXT_CSP_DATA__"[^>]*content="([^"]+)"/.exec(output.html)
    expect(payload).not.toBeNull()
    const hydration = Buffer.from(payload![1]!, 'base64').toString('utf8')
    expect(hydration).toContain('home-sample')
    expect(hydration).toContain('2026-11-16')
    expect(hydration).not.toContain(marker)
    expect(hydration).not.toContain(liveMarker)
    expect(hydration).not.toContain(JSON.stringify(hiddenHit).slice(1, -1))
    expect(output.html).not.toContain(marker)
    expect(output.html).not.toContain(liveMarker)
    expect(output.html).not.toContain(hiddenHit)
    const assets = files('.output/public').map(path => readFileSync(path).toString('utf8')).join('\n')
    expect(assets).not.toContain(marker)
    expect(assets).not.toContain(liveMarker)
    expect(assets).not.toContain(hiddenHit)
  } finally { rmSync(folder, { recursive: true, force: true }) }
})
