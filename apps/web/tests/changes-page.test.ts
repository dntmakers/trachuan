// @vitest-environment happy-dom
import { mount, flushPromises } from '@vue/test-utils'
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { afterEach, expect, it, vi } from 'vitest'
import ChangesPage from '../app/pages/vn/changes.vue'

const counts = { document_added: 1, document_removed: 0, document_validity: 0, provision_text: 0, document_key: 0 }
afterEach(() => vi.unstubAllGlobals())
it('keeps filters in request bodies and clears expired pages', async () => {
  const requests: Record<string, unknown>[] = []
  vi.stubGlobal('definePageMeta', () => {})
  vi.stubGlobal('useState', () => ref('en'))
  vi.stubGlobal('computed', computed)
  vi.stubGlobal('ref', ref)
  vi.stubGlobal('onMounted', onMounted)
  vi.stubGlobal('nextTick', nextTick)
  vi.stubGlobal('onBeforeUnmount', onBeforeUnmount)
  vi.stubGlobal('useApi', () => ({ captureAction: () => ({ accountId: 1, csrfToken: 'csrf' }), session: { account: { id: 1 } }, forAction: () => ({ POST: async (_: string, options: { body: Record<string, unknown> }) => {
    requests.push(options.body)
    if (requests.length === 3) return { error: { error: { code: 'release_expired' } } }
    return { data: { country: 'vn', release: '20990101', previous_release: '20980101', state: 'available', counts, items: [{ kind: 'document_added', key: 'vn:vbpl:1', cause: 'membership' }], next_cursor: 'next' } }
  } }) }))
  const url = window.location.href
  const wrapper = mount(ChangesPage, { attachTo: document.body, global: { stubs: { ChangeRow: { props: ['item'], template: '<p>{{ item.key }}</p>' } } } })
  await flushPromises()
  await wrapper.get('input[value=text]').setValue()
  await wrapper.get('#changes-document').setValue('vn:vbpl:1')
  await wrapper.get('form').trigger('submit')
  await flushPromises()
  await wrapper.get('#changes-document').setValue('vn:vbpl:2')
  const more = wrapper.findAll('button').find(button => button.text() === 'Load more')!
  more.element.focus()
  await more.trigger('click')
  await flushPromises()
  expect(requests[1]).toEqual({ country: 'vn', kinds: ['provision_text'], document: 'vn:vbpl:1' })
  expect(requests[2]).toEqual({ ...requests[1], cursor: 'next', release: '20990101' })
  expect(wrapper.text()).not.toContain('vn:vbpl:1')
  expect(wrapper.text()).toContain('This comparison has expired.')
  expect(document.activeElement).toBe(wrapper.findAll('button').find(button => button.text() === 'Load active comparison')!.element)
  expect(window.location.href).toBe(url)
  wrapper.unmount()
})
