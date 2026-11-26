import { describe, expect, it } from 'vitest'
import { changesMessage, changesRequest, changeReaderLink } from '../app/utils/changes'

const counts = { document_added: 0, document_removed: 0, document_validity: 0, provision_validity: 0, provision_text: 0, document_key: 0 }

describe('release changes', () => {
  it('distinguishes baseline, unavailable, zero counts and no matches', () => {
    expect(changesMessage({ country: 'vn', release: '20990101', state: 'baseline', items: [] })).toBe('baseline')
    expect(changesMessage({ country: 'vn', release: '20990101', state: 'unavailable', items: [] })).toBe('unavailable')
    expect(changesMessage({ country: 'vn', release: '20990101', state: 'available', counts, items: [] })).toBe('empty')
    expect(changesMessage({ country: 'vn', release: '20990101', state: 'available', counts: { ...counts, provision_text: 1 }, items: [] })).toBe('noMatches')
  })
  it('keeps submitted filters and release with the cursor', () => {
    expect(changesRequest('text', 'vn:vbpl:1', 'cursor', '20990101')).toEqual({ country: 'vn', kinds: ['provision_text'], document: 'vn:vbpl:1', cursor: 'cursor', release: '20990101' })
  })
  it('links only a returned handle and pins the release', () => {
    expect(changeReaderLink({ kind: 'document_removed', key: 'vn:vbpl:1', cause: 'source' }, '20990101')).toBeNull()
    expect(changeReaderLink({ kind: 'provision_text', key: 'vn:vbpl:1', anchor: 'dieu-1', change: 'changed', provenance: false, ambiguous: false, handle: 'vn:vbpl:1#dieu-1' }, '20990101')).toBe('/vn/documents/vn:vbpl:1?release=20990101#dieu-1')
  })
})
