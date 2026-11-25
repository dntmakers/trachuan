<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue'
import type { components } from '@trachuan/schema/gen/openapi'
import { ApiError, type Action } from '../api/client'
import { formatDate, relationLabel } from '../content'
import { isDocumentKey } from '../utils/document-key'
import type { Language } from '../../i18n'

type Event = components['schemas']['TimelineEvent']
const props = defineProps<{ documentKey: string, release: string }>()
const language = useState<Language>('language', () => 'vi')
const labels = computed(() => language.value === 'vi' ? { title: 'Dòng thời gian', recorded: 'Quan hệ được ghi nhận trong nguồn, không phải hiệu lực pháp lý theo ngày.', issued: 'Ngày ban hành', effective_from: 'Ngày có hiệu lực', effective_to: 'Ngày hết hiệu lực', unknown: 'Không rõ ngày', document: 'Cấp văn bản', unresolved: 'Chưa xác định được điều khoản', empty: 'Không có sự kiện được ghi nhận.', unavailable: 'Chưa tải được dòng thời gian.', more: 'Xem thêm', retry: 'Thử lại', reload: 'Tải lại văn bản', loading: 'Đang tải' } : { title: 'Timeline', recorded: 'Recorded source relations, not legal effect on a date.', issued: 'Issue date', effective_from: 'Effective from', effective_to: 'Effective to', unknown: 'Date unknown', document: 'Document-level', unresolved: 'Provision not resolved', empty: 'No events recorded.', unavailable: 'Timeline unavailable.', more: 'Show more', retry: 'Retry', reload: 'Reload reader', loading: 'Loading' })
const api = useApi({ localUnavailable: true })
const controller = new AbortController()
const events = ref<Event[]>([])
const loaded = ref(false)
const loading = ref(false)
const next = ref('')
const error = ref('')
let action: Action | undefined
async function load() {
  if (loading.value || controller.signal.aborted || (loaded.value && !next.value)) return
  action ||= api.captureAction()
  loading.value = true
  error.value = ''
  try {
    const response = await api.forAction(action).POST('/api/v1/read', { body: { country: 'vn', release: props.release, items: [next.value ? { next: next.value } : { handle: props.documentKey, timeline: true }] }, signal: controller.signal, params: { header: { 'X-CSRF-Token': action.csrfToken || '' } } })
    if (controller.signal.aborted) return
    if (api.session.account?.id !== action.accountId) { error.value = 'account_changed'; return }
    const item = response.data?.items?.[0]
    if (!item || item.error) { error.value = item?.error || response.error?.error?.code || 'unavailable'; return }
    events.value.push(...(item.units || []).flatMap(unit => unit.kind === 'timeline' && unit.event ? [unit.event] : []))
    next.value = item.next || ''
    loaded.value = true
  } catch (cause) {
    if (!controller.signal.aborted) error.value = cause instanceof ApiError ? cause.code : 'unavailable'
  } finally {
    if (!controller.signal.aborted) loading.value = false
  }
}
function expand(event: globalThis.Event) { if ((event.target as HTMLDetailsElement).open && !loaded.value && !error.value) void load() }
function actorPath(actor: NonNullable<Event['actor']>) { return actor.in_release && isDocumentKey(actor.key) ? { path: `/vn/documents/${actor.key}`, query: { release: props.release } } : undefined }
onBeforeUnmount(() => controller.abort())
</script>
<template>
  <details class="card reader-card" @toggle="expand">
    <summary>{{ labels.title }}</summary>
    <p>{{ labels.recorded }}</p>
    <p v-if="loading" role="status">{{ labels.loading }}</p>
    <p v-if="loaded && !events.length && !next && !error">{{ labels.empty }}</p>
    <ul v-if="events.length">
      <li v-for="(event, index) in events" :key="index">
        <template v-if="event.actor">
          <strong>{{ relationLabel(event.kind, 'in', language) }}</strong>
          <NuxtLink v-if="actorPath(event.actor)" :to="actorPath(event.actor)!">{{ event.actor.number || event.actor.key }}</NuxtLink><span v-else>{{ event.actor.number || event.actor.key }}</span>
          <p>{{ labels.issued }}: {{ event.actor.issued ? formatDate(event.actor.issued, language) : labels.unknown }}</p>
          <p v-if="event.actor.effective_from">{{ labels.effective_from }}: {{ formatDate(event.actor.effective_from, language) }}</p>
          <p v-if="event.actor.effective_to">{{ labels.effective_to }}: {{ formatDate(event.actor.effective_to, language) }}</p>
          <p v-if="event.target?.scope === 'document'">{{ labels.document }}</p>
          <p v-else-if="event.target?.scope === 'provision'"><NuxtLink :to="{ path: `/vn/documents/${documentKey}`, query: { release }, hash: `#${event.target.anchor}` }">{{ event.target.citation }}</NuxtLink></p>
          <p v-else-if="event.target?.scope === 'unresolved_provision'">{{ labels.unresolved }}<template v-if="event.target.named">: {{ event.target.named }}</template></p>
        </template>
        <template v-else-if="event.date && (event.kind === 'issued' || event.kind === 'effective_from' || event.kind === 'effective_to')">{{ labels[event.kind] }}: {{ formatDate(event.date, language) }}</template>
      </li>
    </ul>
    <p v-if="error" role="alert">{{ labels.unavailable }} <a v-if="error === 'release_expired'" :href="`/vn/documents/${documentKey}`">{{ labels.reload }}</a><button v-else type="button" @click="load">{{ labels.retry }}</button></p>
    <button v-if="next && !loading && !error" class="btn btn-secondary" type="button" @click="load">{{ labels.more }}</button>
  </details>
</template>
