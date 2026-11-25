import { afterEach, describe, expect, it, vi } from 'vitest'
import { homeSample, storedSample } from '../server/utils/home-sample'
import { validSample } from '../server/utils/sample-validation'
import { publicStatus } from '../server/utils/public-status'

vi.mock('../server/utils/public-status', () => ({ publicStatus: vi.fn() }))

const fallback = storedSample()!
const live = structuredClone(fallback.result)
const response = (body: unknown, date = '2026-11-16', status = 200) => new Response(JSON.stringify(body), { status, headers: { 'content-type': 'application/json', 'x-sample-status-date': date } })
afterEach(() => { vi.unstubAllGlobals(); vi.useRealTimers(); vi.clearAllMocks() })

describe('home sample validation', () => {
  it('projects the stored first hit and generation date', () => {
    expect(fallback.result.hits).toHaveLength(1)
    expect(fallback.statusDate).toBe('2026-11-16')
    expect(fallback.result.hits[0]).not.toHaveProperty('path')
  })
  it('rejects additional hits and fields at every level', () => {
    for (const change of [
      (x: typeof live) => x.hits.push(x.hits[0]!),
      (x: typeof live) => { Object.assign(x.hits[0]!, { text: 'forbidden' }) },
      (x: typeof live) => { Object.assign(x.documents, { other: x.documents['vn:vbpl:179252'] }) },
      (x: typeof live) => { Object.assign(x.documents['vn:vbpl:179252'].status, { extra: true }) },
    ]) {
      const x = structuredClone(live)
      change(x)
      expect(validSample(x)).toBe(false)
    }
  })
  it('uses the frozen live date only when the release matches', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(response(live, '2026-11-15')))
    vi.mocked(publicStatus).mockResolvedValueOnce({ release: live.release } as Awaited<ReturnType<typeof publicStatus>>)
    expect(await homeSample('http://example.invalid')).toMatchObject({ stored: false, statusDate: '2026-11-15' })
  })
  it('keeps the live header date when Vietnam midnight passes', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-11-15T16:59:59Z'))
    vi.stubGlobal('fetch', vi.fn().mockImplementation(async () => {
      vi.setSystemTime(new Date('2026-11-15T17:00:01Z'))
      return response(live, '2026-11-15')
    }))
    vi.mocked(publicStatus).mockResolvedValueOnce({ release: live.release } as Awaited<ReturnType<typeof publicStatus>>)
    expect(await homeSample('http://example.invalid')).toMatchObject({ stored: false, statusDate: '2026-11-15' })
  })
  it('stops reading an oversized unknown-length decoded stream', async () => {
    let cancelled = false
    const body = new ReadableStream<Uint8Array>({
      pull(controller) { controller.enqueue(new Uint8Array(9000)) },
      cancel() { cancelled = true },
    })
    const reply = new Response(body, { headers: { 'content-type': 'application/json', 'x-sample-status-date': '2026-11-15' } })
    expect(reply.headers.get('content-length')).toBeNull()
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(reply))
    expect((await homeSample('http://example.invalid'))?.stored).toBe(true)
    expect(cancelled).toBe(true)
  })
  it('falls back after a release mismatch and refresh', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(response(live)))
    vi.mocked(publicStatus).mockResolvedValue({ release: '20990101' } as Awaited<ReturnType<typeof publicStatus>>)
    expect((await homeSample('http://example.invalid'))?.stored).toBe(true)
  })
  it('returns the stored sample without another upstream request when the slot is busy', async () => {
    let finish!: (value: Response) => void
    const fetcher = vi.fn(() => new Promise<Response>(resolve => { finish = resolve }))
    vi.stubGlobal('fetch', fetcher)
    const first = homeSample('http://example.invalid')
    const overflow = await Promise.race([
      homeSample('http://example.invalid'),
      new Promise<'waiting'>(resolve => setTimeout(() => resolve('waiting'), 50)),
    ])
    expect(overflow).toMatchObject({ stored: true })
    expect(fetcher).toHaveBeenCalledTimes(1)
    finish(new Response(null, { status: 503 }))
    expect((await first)?.stored).toBe(true)
    const next = homeSample('http://example.invalid')
    expect(fetcher).toHaveBeenCalledTimes(2)
    finish(new Response(null, { status: 503 }))
    expect((await next)?.stored).toBe(true)
  })
  it('falls back on malformed responses, missing dates, oversize, and errors', async () => {
    for (const value of [response({ ...live, extra: true }), response(live, ''), response({ ...live, extra: 'x'.repeat(17000) }), response({}, '2026-11-16', 503)]) {
      vi.stubGlobal('fetch', vi.fn().mockResolvedValue(value))
      expect((await homeSample('http://example.invalid'))?.stored).toBe(true)
    }
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('offline')))
    expect((await homeSample('http://example.invalid'))?.stored).toBe(true)
  })
})
