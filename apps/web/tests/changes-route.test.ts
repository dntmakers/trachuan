import { execFileSync } from 'node:child_process'
import { expect, it } from 'vitest'

it('redirects before querying changes and marks the route noindex', () => {
  const script = `
    import { Server } from 'node:http';
    Server.prototype.listen = function () { return this };
    process.env.NITRO_SHUTDOWN_DISABLED = '1';
    let changes = 0;
    globalThis.fetch = async request => {
      if (new URL(request.url).pathname === '/api/v1/changes') changes++;
      return new Response(JSON.stringify({ error: { code: 'unauthenticated' } }), { status: 401, headers: { 'content-type': 'application/json' } });
    };
    const { nitroApp } = await import('./tests/nitro-app.mjs');
    const response = await nitroApp.localFetch('/vn/changes?document=secret');
    console.log(JSON.stringify({ status: response.status, location: response.headers.get('location'), robots: response.headers.get('x-robots-tag'), cache: response.headers.get('cache-control'), changes }));
  `
  const output = execFileSync(process.execPath, ['--input-type=module', '-e', script], { encoding: 'utf8' })
  expect(JSON.parse(output)).toEqual({ status: 302, location: '/login?next=%2Fvn%2Fchanges', robots: 'noindex', cache: 'no-store', changes: 0 })
})
