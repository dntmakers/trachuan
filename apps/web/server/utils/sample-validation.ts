import type { components } from '@trachuan/schema/gen/openapi'

type Sample = components['schemas']['PublicSampleResult']
const key = 'vn:vbpl:179252'
const url = 'https://vbpl.vn/van-ban/chi-tiet/179252'
const object = (x: unknown): x is Record<string, unknown> => !!x && typeof x === 'object' && !Array.isArray(x)
const closed = (x: Record<string, unknown>, required: string[], optional: string[] = []) => required.every(k => k in x) && Object.keys(x).every(k => required.includes(k) || optional.includes(k))
export function validDate(x: unknown): x is string {
  if (typeof x !== 'string' || !/^\d{4}-\d{2}-\d{2}$/.test(x)) return false
  const d = new Date(`${x}T00:00:00Z`)
  return !Number.isNaN(d.valueOf()) && d.toISOString().slice(0, 10) === x
}
export function validSample(x: unknown): x is Sample {
  if (!object(x) || !closed(x, ['release', 'abstain', 'hits', 'documents']) || typeof x.release !== 'string' || !x.release || x.abstain !== false || !Array.isArray(x.hits) || x.hits.length !== 1 || !object(x.documents) || !closed(x.documents, [key])) return false
  const h = x.hits[0], d = x.documents[key]
  if (!object(h) || !closed(h, ['handle', 'doc', 'citation', 'snippet', 'complete']) || typeof h.handle !== 'string' || !/^vn:vbpl:179252#.+$/.test(h.handle) || h.doc !== key || typeof h.citation !== 'string' || !h.citation || typeof h.snippet !== 'string' || !h.snippet || [...h.snippet].length > 600 || h.complete !== false) return false
  if (!object(d) || !closed(d, ['number', 'status', 'text_source', 'text_complete'], ['title', 'type', 'issuer', 'issued', 'effective_from', 'effective_to', 'source_url']) || d.number !== '91/2025/QH15' || !['source_tree', 'html_transcription', 'pdf_text', 'ocr', 'none'].includes(d.text_source as string) || typeof d.text_complete !== 'boolean') return false
  if (['title', 'type', 'issuer'].some(k => k in d && typeof d[k] !== 'string') || ['issued', 'effective_from', 'effective_to'].some(k => k in d && !validDate(d[k])) || ('source_url' in d && d.source_url !== url)) return false
  const s = d.status
  return object(s) && closed(s, ['current'], ['class', 'basis', 'status_basis']) && typeof s.current === 'boolean' && ['class', 'basis', 'status_basis'].every(k => !(k in s) || typeof s[k] === 'string')
}
export function withinCap(x: unknown): boolean { return new TextEncoder().encode(JSON.stringify(x)).length <= 16 * 1024 }
