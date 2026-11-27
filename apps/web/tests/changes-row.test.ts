// @vitest-environment happy-dom
import { mount } from '@vue/test-utils'
import { computed, ref } from 'vue'
import { afterEach, expect, it, vi } from 'vitest'
import ChangeRow from '../app/components/ChangeRow.vue'
import { changesMessages } from '../i18n/changes'

afterEach(() => vi.unstubAllGlobals())
it.each(['vi', 'en'] as const)('provides labels for every served kind in %s', language => {
  const labels = changesMessages(language)
  for (const kind of ['document_added', 'document_removed', 'document_validity', 'provision_text', 'document_key'] as const) expect(labels[kind]).toBeTruthy()
})
it.each(['vi', 'en'] as const)('shows validity and source context in %s', language => {
  vi.stubGlobal('useState', () => ref(language))
  vi.stubGlobal('computed', computed)
  const wrapper = mount(ChangeRow, { props: { release: '20990101', item: { kind: 'document_validity', key: 'vn:vbpl:2', handle: 'vn:vbpl:2', eff_from: { before: null, after: '2020-01-01' }, source_change: { before_key: 'vn:vbpl:1', text_complete: { before: false, after: true } } } }, global: { stubs: { NuxtLink: { props: ['to'], template: '<a :href="to"><slot /></a>' } } } })
  expect(wrapper.text()).toContain(language === 'vi' ? 'Hiệu lực ghi nhận trong kho văn bản' : 'Validity values recorded in the corpus')
  expect(wrapper.text()).toContain(language === 'vi' ? 'Thay đổi nguồn' : 'Source change')
  expect(wrapper.text()).toContain('vn:vbpl:1')
  expect(wrapper.text()).toContain('2020-01-01')
  expect(wrapper.get('a').attributes('href')).toBe('/vn/documents/vn:vbpl:2?release=20990101')
})
it.each(['toString', 'constructor', '__proto__'])('shows the raw status %s and a missing value as not recorded', status => {
  vi.stubGlobal('useState', () => ref('vi'))
  vi.stubGlobal('computed', computed)
  const wrapper = mount(ChangeRow, { props: { release: '20990101', item: { kind: 'document_validity', key: 'vn:vbpl:1', status_class: { before: status, after: null } } as never } })
  expect(wrapper.text()).toContain(`Trước: ${status}`)
  expect(wrapper.text()).toContain('Sau: Chưa ghi nhận')
})
it.each(['membership', 'coverage', 'source'] as const)('labels removal cause %s and never links its text', cause => {
  vi.stubGlobal('useState', () => ref('en'))
  vi.stubGlobal('computed', computed)
  const wrapper = mount(ChangeRow, { props: { release: '20990101', item: { kind: 'document_removed', key: 'vn:vbpl:1', cause } } })
  expect(wrapper.text()).toContain(({ membership: 'Membership', coverage: 'Coverage change', source: 'Source change' })[cause])
  expect(wrapper.text()).toContain('Text unavailable in this release')
  expect(wrapper.find('a').exists()).toBe(false)
})
