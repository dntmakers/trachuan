// @vitest-environment happy-dom
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, expect, it, vi } from 'vitest'
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import { createApi } from '../app/api/client'
import Search from '../app/pages/vn/search.vue'

const json = (value: unknown) => new Response(JSON.stringify(value), { headers: { 'content-type': 'application/json' } })
afterEach(() => vi.unstubAllGlobals())

it.each(['en', 'vi'])('submits issue bounds and validates ranges in %s', async (language) => {
  const bodies: Record<string, unknown>[] = []
  const replace = vi.fn()
  const session = { account: { id: 1, email: 'test@example.invalid', created_at: '2026-01-01T00:00:00Z' }, csrfToken: 'token', status: 'signed-in' as const }
  const fetcher: typeof fetch = async input => {
    const request = input as Request
    if (new URL(request.url).pathname === '/api/v1/search') {
      bodies.push(await request.json())
      return json({ release: '20990101', abstain: false, hits: [], next_cursor: 'cursor' })
    }
    return json({ nodes: [], doc_types: [], issuers: [] })
  }
  vi.stubGlobal('definePageMeta', () => {})
  vi.stubGlobal('useState', () => ref(language))
  vi.stubGlobal('computed', computed)
  vi.stubGlobal('ref', ref)
  vi.stubGlobal('nextTick', nextTick)
  vi.stubGlobal('onMounted', onMounted)
  vi.stubGlobal('onBeforeUnmount', onBeforeUnmount)
  vi.stubGlobal('useRoute', () => ({ query: {}, hash: '', path: '/vn/search' }))
  vi.stubGlobal('useRouter', () => ({ replace }))
  vi.stubGlobal('useApi', () => createApi({ origin: 'http://localhost', session, fetch: fetcher }))
  setActivePinia(createPinia())
  const wrapper = mount(Search, { global: { stubs: { NuxtLayout: { template: '<div><slot name="header" /><slot /></div>' }, SearchNodePicker: true, SearchResults: true } } })
  await flushPromises()
  const fromLabel = language === 'en' ? 'Issued from' : 'Ban hành từ ngày'
  const toLabel = language === 'en' ? 'Issued to' : 'Ban hành đến ngày'
  const from = wrapper.findAll('label').find(label => label.text().includes(fromLabel))?.find('input')
  const to = wrapper.findAll('label').find(label => label.text().includes(toLabel))?.find('input')
  expect(from?.exists()).toBe(true)
  expect(to?.exists()).toBe(true)
  await wrapper.get('textarea').setValue('query')
  await wrapper.get('form').trigger('submit'); await flushPromises()
  expect(bodies[0]).not.toHaveProperty('issued_from')
  expect(bodies[0]).not.toHaveProperty('issued_to')
  await from!.setValue('2024-01-01')
  await wrapper.get('form').trigger('submit'); await flushPromises()
  expect(bodies[1]).toMatchObject({ issued_from: '2024-01-01' })
  expect(bodies[1]).not.toHaveProperty('issued_to')
  await to!.setValue('2024-12-31')
  await wrapper.get('form').trigger('submit'); await flushPromises()
  expect(bodies[2]).toMatchObject({ issued_from: '2024-01-01', issued_to: '2024-12-31' })
  expect(bodies[2]).not.toHaveProperty('cursor')
  await wrapper.findAll('button').find(button => button.text() === (language === 'en' ? 'Show more results' : 'Xem thêm kết quả'))!.trigger('click'); await flushPromises()
  expect(bodies[3]).toMatchObject({ issued_from: '2024-01-01', issued_to: '2024-12-31', cursor: 'cursor' })
  await from!.setValue('')
  await wrapper.get('form').trigger('submit'); await flushPromises()
  expect(bodies[4]).toMatchObject({ issued_to: '2024-12-31' })
  expect(bodies[4]).not.toHaveProperty('issued_from')
  expect(bodies[4]).not.toHaveProperty('cursor')
  await from!.setValue('2025-01-01')
  await wrapper.get('form').trigger('submit'); await flushPromises()
  expect(bodies).toHaveLength(5)
  expect(wrapper.find('[role="alert"]').exists()).toBe(true)
  await from!.setValue('')
  Object.defineProperty(from!.element, 'validity', { configurable: true, value: { badInput: true } })
  await from!.trigger('input')
  await wrapper.get('form').trigger('submit'); await flushPromises()
  expect(bodies).toHaveLength(5)
  expect(wrapper.find('[role="alert"]').exists()).toBe(true)
  expect(replace).not.toHaveBeenCalled()
  wrapper.unmount()
})
