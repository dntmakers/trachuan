// @vitest-environment happy-dom
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import { computed, defineComponent, h, onMounted, ref, Suspense } from 'vue'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import Home from '../app/pages/index.vue'
import { storedSample } from '../server/utils/home-sample'

let sample = storedSample()!

beforeEach(() => {
  setActivePinia(createPinia())
  vi.stubGlobal('useState', () => ref('vi'))
  vi.stubGlobal('computed', computed)
  vi.stubGlobal('ref', ref)
  vi.stubGlobal('onMounted', onMounted)
  vi.stubGlobal('useHead', () => {})
  vi.stubGlobal('useRuntimeConfig', () => ({ public: { siteOrigin: 'https://trachuan.vn' } }))
  vi.stubGlobal('useStructuredData', () => {})
  vi.stubGlobal('useRoute', () => ({ query: {} }))
  vi.stubGlobal('useApi', () => ({}))
  sample = storedSample()!
  vi.stubGlobal('useAsyncData', async (key: string) => ({ data: ref(key === 'home-sample' ? sample : { status: null }) }))
})
afterEach(() => vi.unstubAllGlobals())

async function render() {
  const wrapper = mount(defineComponent({ render: () => h(Suspense, null, { default: () => h(Home) }) }), { global: { stubs: { HomeHero: true, HomeConnect: true, CoverageFacts: true, ResultCard: true, NuxtLink: true } } })
  await flushPromises()
  return wrapper
}

it('describes the actual sample citation and its source', async () => {
  const wrapper = await render()
  expect(wrapper.get('.home-explanation ul').text()).toContain('Điều 19 Luật 91/2025/QH15')
  expect(wrapper.get('.home-citation').text()).toBe('Điều 19 Luật 91/2025/QH15')
  expect(wrapper.get('.home-explanation ul').text()).toContain('Liên kết về bản gốc, khi nguồn có công bố.')
})

it('describes the source only when the projected sample has a URL', async () => {
  delete sample.result.documents['vn:vbpl:179252'].source_url
  expect((await render()).get('.home-explanation ul').text()).not.toContain('Liên kết về bản gốc, khi nguồn có công bố.')
})
