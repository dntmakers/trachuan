// @vitest-environment happy-dom
import { mount, flushPromises } from '@vue/test-utils'
import { computed, ref } from 'vue'
import { afterEach, expect, it, vi } from 'vitest'
import ReaderRail from '../app/components/ReaderRail.vue'
import TimelinePanel from '../app/components/TimelinePanel.vue'

afterEach(() => vi.unstubAllGlobals())
it('loads the collapsed timeline at the reader release and appends pages', async () => {
  const calls: { body: { country: string, release: string, items: unknown[] } }[] = []
  vi.stubGlobal('useState', () => ref('en'))
  vi.stubGlobal('computed', computed)
  vi.stubGlobal('useApi', () => ({ captureAction: () => ({ accountId: 1, csrfToken: 'csrf' }), session: { account: { id: 1 } }, forAction: () => ({ POST: async (_path: string, options: { body: { country: string, release: string, items: unknown[] } }) => { calls.push(options); return { data: { items: [{ next: calls.length === 1 ? 'more' : undefined, units: [{ kind: 'timeline', event: calls.length === 1 ? { kind: 'issued', date: '2020-01-01' } : { kind: 'repeals', actor: { key: 'vn:vbpl:2', in_release: false }, target: { scope: 'document' } } }] }] } } } }) }))
  const wrapper = mount(ReaderRail, { props: { document: { status: { current: true }, text_source: 'none', text_complete: false, related: [], related_counts: {} }, release: '20990101_1', documentKey: 'vn:vbpl:1', gaps: [] } })
  expect(calls).toHaveLength(0)
  const details = wrapper.findAll('details').find(item => item.text().includes('Timeline'))!
  expect(details.element.open).toBe(false)
  details.element.open = true
  await details.trigger('toggle')
  await flushPromises()
  expect(calls[0].body).toEqual({ country: 'vn', release: '20990101_1', items: [{ handle: 'vn:vbpl:1', timeline: true }] })
  expect(wrapper.text()).toContain('2020')
  await details.get('button').trigger('click')
  await flushPromises()
  expect(calls[1].body.items).toEqual([{ next: 'more' }])
  expect(details.text()).toContain('Document-level')
  expect(details.text()).toContain('Date unknown')
  expect(details.find('button').exists()).toBe(false)
})

it.each(['vi', 'en'] as const)('renders recorded targets and actor links in %s', async language => {
  vi.stubGlobal('useState', () => ref(language))
  vi.stubGlobal('computed', computed)
  vi.stubGlobal('useApi', () => ({ captureAction: () => ({ accountId: 1 }), session: { account: { id: 1 } }, forAction: () => ({ POST: async () => ({ data: { items: [{ units: [
    { kind: 'timeline', event: { kind: 'amends_supplements', actor: { key: 'vn:vbpl:2', number: 'Actor', issued: '2020-01-01', effective_from: '2020-02-01', effective_to: '2021-01-01', in_release: true }, target: { scope: 'provision', anchor: 'dieu-1', citation: 'Article 1' } } },
    { kind: 'timeline', event: { kind: 'repeals', actor: { key: 'vn:vbpl:3', in_release: false }, target: { scope: 'unresolved_provision', named: 'dieu-2' } } },
  ] }] } }) }) }))
  const wrapper = mount(TimelinePanel, { props: { documentKey: 'vn:vbpl:1', release: '20990101_1' }, global: { stubs: { NuxtLink: { props: ['to'], template: '<a :data-target="JSON.stringify(to)"><slot /></a>' } } } })
  const details = wrapper.get('details')
  details.element.open = true
  await details.trigger('toggle')
  await flushPromises()
  expect(wrapper.text()).toContain(language === 'vi' ? 'Chưa xác định được điều khoản' : 'Provision not resolved')
  expect(wrapper.text()).toContain('dieu-2')
  expect(wrapper.text()).toContain(language === 'vi' ? 'Không rõ ngày' : 'Date unknown')
  expect(wrapper.findAll('a')).toHaveLength(2)
  expect(wrapper.findAll('a')[0]!.attributes('data-target')).toContain('20990101_1')
  expect(wrapper.findAll('a')[1]!.attributes('data-target')).toContain('#dieu-1')
})

it('shows the fixed empty state after an empty result', async () => {
  vi.stubGlobal('useState', () => ref('en'))
  vi.stubGlobal('computed', computed)
  vi.stubGlobal('useApi', () => ({ captureAction: () => ({ accountId: 1 }), session: { account: { id: 1 } }, forAction: () => ({ POST: async () => ({ data: { items: [{ complete: true, units: [] }] } }) }) }))
  const wrapper = mount(TimelinePanel, { props: { documentKey: 'vn:vbpl:1', release: '20990101_1' } })
  const details = wrapper.get('details')
  details.element.open = true
  await details.trigger('toggle')
  await flushPromises()
  expect(wrapper.text()).toContain('No events recorded.')
})

it('offers the current reader after release expiry', async () => {
  vi.stubGlobal('useState', () => ref('en'))
  vi.stubGlobal('computed', computed)
  vi.stubGlobal('useApi', () => ({ captureAction: () => ({ accountId: 1 }), session: { account: { id: 1 } }, forAction: () => ({ POST: async () => ({ data: { items: [{ error: 'release_expired' }] } }) }) }))
  const wrapper = mount(TimelinePanel, { props: { documentKey: 'vn:vbpl:1', release: '20990101_1' } })
  const details = wrapper.get('details')
  details.element.open = true
  await details.trigger('toggle')
  await flushPromises()
  expect(wrapper.get('[role=alert]').text()).toContain('Timeline unavailable')
  expect(wrapper.get('a').attributes('href')).toBe('/vn/documents/vn:vbpl:1')
})
