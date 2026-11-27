import { expect, it, vi } from 'vitest'
import { computed, createSSRApp, ref } from 'vue'
import { renderToString } from '@vue/server-renderer'
import LoginPage from '../app/pages/login.vue'

it('renders the login form inside a disabled fieldset', async () => {
  vi.stubGlobal('computed', computed)
  vi.stubGlobal('useState', (key: string) => key === 'language' ? ref('en') : ref(false))
  vi.stubGlobal('useRoute', () => ({ query: { next: '/vn/changes' } }))
  vi.stubGlobal('useAuthApi', () => ({}))
  vi.stubGlobal('usePageSecrets', () => ({ signal: new AbortController().signal }))
  const html = await renderToString(createSSRApp(LoginPage), {})
  expect(html).toMatch(/<form[^>]*><fieldset disabled>[\s\S]*<button class="btn btn-primary"/)
  expect(html).toContain('next=%2Fvn%2Fchanges')
})
