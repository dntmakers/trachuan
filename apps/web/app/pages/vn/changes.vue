<script setup lang="ts">
import { ApiError, type Action } from '../../api/client'
import { isDocumentKey } from '../../utils/document-key'
import { changesMessage, changesRequest, type ChangesResult, type ChangesView } from '../../utils/changes'
import { changesMessages } from '../../../i18n/changes'
import type { Language } from '../../../i18n'

definePageMeta({ signedIn: true })
const language = useState<Language>('language', () => 'vi')
const labels = computed(() => changesMessages(language.value))
const api = useApi({ userAction: true, localUnavailable: true })
const view = ref<ChangesView>('default')
const document = ref('')
const invalid = ref(false)
const result = ref<ChangesResult | null>(null)
const error = ref('')
const reloadButton = ref<HTMLButtonElement | null>(null)
const loading = ref(false)
const submitted = ref<{ view: ChangesView, document: string }>({ view: 'default', document: '' })
let action: Action | null = null
let controller: AbortController | null = null
onBeforeUnmount(() => controller?.abort())
const message = computed(() => result.value ? changesMessage(result.value) : null)
async function load(more = false) {
  if (loading.value) return
  if (!more) {
    const key = document.value.trim()
    invalid.value = !!key && !isDocumentKey(key)
    if (invalid.value) return
    controller?.abort()
    controller = new AbortController()
    action = api.captureAction()
    submitted.value = { view: view.value, document: key }
    result.value = null
  }
  if (!action || !controller || (more && !result.value?.next_cursor)) return
  const input = changesRequest(submitted.value.view, submitted.value.document, more ? result.value?.next_cursor : undefined, more ? result.value?.release : undefined)
  const signal = controller.signal
  loading.value = true
  error.value = ''
  try {
    const response = await api.forAction(action).POST('/api/v1/changes', { body: input, signal, params: { header: { 'X-CSRF-Token': action.csrfToken || '' } } })
    if (signal.aborted) return
    if (api.session.account?.id !== action.accountId) { result.value = null; error.value = 'account_changed'; return }
    if (response.data) {
      result.value = more && result.value ? { ...response.data, items: [...result.value.items, ...response.data.items] } : response.data
    } else {
      error.value = response.error?.error?.code || 'generic'
      if (error.value === 'release_expired') result.value = null
    }
  } catch (cause) {
    if (!signal.aborted) error.value = cause instanceof ApiError ? cause.code : 'generic'
  } finally {
    if (!signal.aborted) {
      loading.value = false
      if (error.value === 'release_expired') { await nextTick(); reloadButton.value?.focus() }
    }
  }
}
onMounted(() => load())
</script>
<template>
  <div class="container page-content">
    <header class="page-intro"><h1>{{ labels.title }}</h1><p>{{ labels.intro }}</p></header>
    <form class="page-section" @submit.prevent="load()">
      <fieldset><legend>{{ labels.title }}</legend><label v-for="option in (['default', 'text', 'source'] as const)" :key="option"><input v-model="view" type="radio" name="view" :value="option"> {{ labels[option] }} </label></fieldset>
      <label for="changes-document">{{ labels.document }}</label><input id="changes-document" v-model="document" class="input" autocomplete="off">
      <p v-if="invalid" role="alert">{{ labels.invalid }}</p><button class="btn btn-primary" type="submit" :disabled="loading">{{ labels.apply }}</button>
    </form>
    <p v-if="loading" role="status">{{ labels.loading }}</p>
    <div v-if="error" class="notice" role="alert"><p>{{ error === 'release_expired' ? labels.expired : labels.error }}</p><button ref="reloadButton" class="btn btn-secondary" type="button" :disabled="loading" @click="load()">{{ error === 'release_expired' ? labels.reload : labels.retry }}</button></div>
    <section v-if="result" aria-live="polite">
      <p>{{ labels.release }}: {{ result.release }}</p><p v-if="result.previous_release">{{ labels.previous }}: {{ result.previous_release }}</p>
      <div v-if="result.counts"><h2>{{ labels.totals }}</h2><p v-for="(count, kind) in result.counts" :key="kind">{{ labels[kind] }}: {{ count }}</p></div>
      <p v-if="message">{{ labels[message] }}</p>
      <ChangeRow v-for="(item, index) in result.items" :key="index" :item="item" :release="result.release" />
      <button v-if="result.next_cursor" class="btn btn-secondary" type="button" :disabled="loading" @click="load(true)">{{ labels.more }}</button>
    </section>
  </div>
</template>
