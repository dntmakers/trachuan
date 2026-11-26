import type { components } from '@trachuan/schema/gen/openapi'
import { isDocumentKey } from './document-key'

export type Change = components['schemas']['ChangesResult']['items'][number]
export type ChangesResult = components['schemas']['ChangesResult']
export type ChangesView = 'default' | 'text' | 'source'

export function changesRequest(view: ChangesView, document: string, cursor?: string, release?: string): components['schemas']['ChangesInput'] {
  return { country: 'vn', ...(view === 'text' ? { kinds: ['provision_text'] } : view === 'source' ? { kinds: ['document_key'] } : {}), ...(document ? { document } : {}), ...(cursor ? { cursor } : {}), ...(release ? { release } : {}) }
}

export function changesMessage(result: ChangesResult): 'baseline' | 'unavailable' | 'empty' | 'noMatches' | null {
  if (result.state !== 'available') return result.state
  if (result.items.length) return null
  return Object.values(result.counts || {}).every(count => count === 0) ? 'empty' : 'noMatches'
}

export function changeReaderLink(item: Change, release: string): string | null {
  if (!('handle' in item) || !item.handle) return null
  const [key, anchor] = item.handle.split('#', 2)
  if (!key || !isDocumentKey(key)) return null
  return `/vn/documents/${key}?release=${encodeURIComponent(release)}${anchor ? `#${encodeURI(anchor)}` : ''}`
}
