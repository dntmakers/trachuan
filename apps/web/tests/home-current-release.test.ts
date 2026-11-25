// @vitest-environment happy-dom
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import { computed, defineComponent, h, onMounted, onUnmounted, reactive, ref, Suspense } from 'vue'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import Home from '../app/pages/index.vue'
import ResultCard from '../app/components/ResultCard.vue'
import { storedSample } from '../server/utils/home-sample'

const session = reactive<{ status: 'unknown' | 'signed-in' | 'signed-out' }>({ status: 'unknown' })
const get = vi.fn()
let sample = storedSample()!
beforeEach(() => {
  sample = storedSample()!
  setActivePinia(createPinia())
  session.status = 'unknown'
  get.mockReset()
  vi.stubGlobal('useState', () => ref('vi'))
  vi.stubGlobal('computed', computed)
  vi.stubGlobal('ref', ref)
  vi.stubGlobal('onMounted', onMounted)
  vi.stubGlobal('onUnmounted', onUnmounted)
  vi.stubGlobal('useHead', () => {})
  vi.stubGlobal('useRuntimeConfig', () => ({ public: { siteOrigin: 'https://trachuan.vn' } }))
  vi.stubGlobal('useStructuredData', () => {})
  vi.stubGlobal('useRoute', () => ({ query: {} }))
  vi.stubGlobal('useApi', () => ({ session, client: { GET: get } }))
  vi.stubGlobal('useAsyncData', async (key: string) => ({ data: ref(key === 'home-sample' ? sample : { status: null }) }))
})
afterEach(() => { vi.unstubAllGlobals(); vi.useRealTimers() })
async function render() {
  const wrapper = mount(defineComponent({ render: () => h(Suspense, null, { default: () => h(Home) }) }), { attachTo: document.body, global: { components: { ResultCard }, stubs: { HomeHero: true, HomeConnect: true, CoverageFacts: true, NuxtLink: { props: ['to'], template: '<a :href="to"><slot /></a>' } } } })
  await flushPromises()
  return wrapper
}

it('dates the sample status from its generation date', async () => {
  const wrapper = await render()
  expect(wrapper.get('.result-card-head').text()).toContain('Trạng thái tính đến 16/11/2026, theo vbpl.vn')
  wrapper.unmount()
})

it('shows the live status header date on both sides of Vietnam midnight', async () => {
  sample = { ...sample, stored: false, statusDate: '2026-11-15' }
  for (const time of ['2026-11-15T16:59:59Z', '2026-11-15T17:00:01Z']) {
    vi.setSystemTime(new Date(time))
    const wrapper = await render()
    expect(wrapper.get('.result-card-head').text()).toContain('Trạng thái tính đến 15/11/2026, theo vbpl.vn')
    wrapper.unmount()
  }
})

it('keeps the reader link without checking the current release for signed-in visitors', async () => {
  session.status = 'signed-in'
  const wrapper = await render()
  expect(get).not.toHaveBeenCalled()
  expect(wrapper.get('.result-card-footer .result-actions > a').isVisible()).toBe(true)
  expect(wrapper.get('.result-card-footer .result-actions > a').attributes('href')).not.toContain('release=')
  wrapper.unmount()
})
