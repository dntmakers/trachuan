import raw from '../../app/data/sample-result.json'
import meta from '../../app/data/sample-result.meta.json'
import { publicStatus } from './public-status'
import { validDate, validSample, withinCap } from './sample-validation'

const key = 'vn:vbpl:179252'
const select = (value: Record<string, unknown>, keys: string[]) => Object.fromEntries(keys.filter(k => k in value).map(k => [k, value[k]]))
export function storedSample() {
  try {
    const hit = raw.hits[0]
    if (!hit) return null
    const doc = raw.documents[key]
    const result = {
      release: raw.release, abstain: raw.abstain,
      hits: [select(hit, ['handle', 'doc', 'citation', 'snippet', 'complete'])],
      documents: { [key]: { ...select(doc, ['number', 'text_source', 'text_complete', 'title', 'type', 'issuer', 'issued', 'effective_from', 'effective_to', 'source_url']), status: select(doc.status, ['current', 'class', 'basis', 'status_basis']) } },
    }
    const releaseDate = /^(\d{4})(\d{2})(\d{2})(?:_\d+)?$/.exec(meta.release)
    const date = releaseDate && `${releaseDate[1]}-${releaseDate[2]}-${releaseDate[3]}`
    const generated = new Date(meta.generated_at)
    const statusDate = Number.isNaN(generated.valueOf()) ? '' : new Intl.DateTimeFormat('en-CA', { timeZone: 'Asia/Ho_Chi_Minh', year: 'numeric', month: '2-digit', day: '2-digit' }).format(generated)
    if (!validSample(result) || !withinCap(result) || !validDate(date) || !validDate(statusDate)) return null
    return { result, releaseDate: date, statusDate, stored: true }
  } catch { return null }
}

let sampleInFlight = false

export async function homeSample(origin: string) {
  if (sampleInFlight) return storedSample()
  sampleInFlight = true
  const started = Date.now()
  try {
    const response = await fetch(`${origin}/api/v1/public/sample`, { credentials: 'omit', signal: AbortSignal.timeout(5000) })
    const date = response.headers.get('x-sample-status-date')
    if (response.status !== 200 || !/^application\/json(?:\s*;|\s*$)/i.test(response.headers.get('content-type') || '') || !validDate(date) || !response.body) return storedSample()
    const reader = response.body.getReader()
    const chunks: Uint8Array[] = []
    let size = 0
    while (true) {
      const part = await reader.read()
      if (part.done) break
      size += part.value.length
      if (size > 16 * 1024) { await reader.cancel(); return storedSample() }
      chunks.push(part.value)
    }
    const bytes = new Uint8Array(size)
    let offset = 0
    for (const chunk of chunks) { bytes.set(chunk, offset); offset += chunk.length }
    const result: unknown = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(bytes))
    if (!validSample(result)) return storedSample()
    let status = await publicStatus(origin)
    if (status?.release !== result.release) status = await publicStatus(origin, started)
    const release = /^(\d{4})(\d{2})(\d{2})(?:_\d+)?$/.exec(result.release)
    const releaseDate = release && `${release[1]}-${release[2]}-${release[3]}`
    if (!status || status.release !== result.release || !validDate(releaseDate)) return storedSample()
    return { result, releaseDate, statusDate: date, stored: false }
  } catch { return storedSample() } finally { sampleInFlight = false }
}
