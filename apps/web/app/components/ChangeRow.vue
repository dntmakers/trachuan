<script setup lang="ts">
import type { Change } from '../utils/changes'
import { changeReaderLink } from '../utils/changes'
import { changesMessages } from '../../i18n/changes'
import type { Language } from '../../i18n'

const props = defineProps<{ item: Change, release: string }>()
const language = useState<Language>('language', () => 'vi')
const labels = computed(() => changesMessages(language.value))
const link = computed(() => changeReaderLink(props.item, props.release))
const fields = ['status_class', 'eff_from', 'eff_to', 'is_current', 'validity_conflict'] as const
const sourceFields = ['text_source', 'text_complete', 'baseline'] as const
const validityPairs = computed(() => props.item.kind === 'document_validity' ? fields.flatMap(field => { const value = props.item.kind === 'document_validity' ? props.item[field] : undefined; return value ? [{ field, value }] : [] }) : [])
const sourcePairs = computed(() => {
  const source = (props.item.kind === 'document_validity' || props.item.kind === 'provision_validity') ? props.item.source_change : undefined
  return source ? sourceFields.flatMap(field => { const value = source[field]; return value ? [{ field, value }] : [] }) : []
})
function fieldLabel(field: string): string {
  if (field === 'baseline') return labels.value.baselineField
  return labels.value[field as keyof typeof labels.value] || field
}
function valueLabel(value: unknown, field = ''): string {
  if (value === null || value === undefined) return labels.value.unknown
  if (typeof value === 'boolean') return value ? labels.value.yes : labels.value.no
  const status = { in_force: labels.value.in_force, partial: labels.value.partial, expired: labels.value.expiredStatus, not_yet: labels.value.not_yet, suspended: labels.value.suspended, unknown: labels.value.unknownStatus }
  const source = { source_tree: labels.value.source_tree, html_transcription: labels.value.html_transcription, pdf_text: labels.value.pdf_text, ocr: labels.value.ocr, none: labels.value.none }
  const table: Record<string, string> = field === 'status_class' ? status : field === 'text_source' ? source : {}
  const key = String(value)
  return Object.hasOwn(table, key) ? table[key]! : key
}
function pair(value: { before: unknown, after: unknown }, field: string): string {
  return `${labels.value.before}: ${valueLabel(value.before, field)} · ${labels.value.after}: ${valueLabel(value.after, field)}`
}
</script>
<template>
  <article class="notice page-section change-row">
    <h3>{{ labels[item.kind] }}</h3>
    <p><NuxtLink v-if="link" :to="link">{{ item.key }}</NuxtLink><span v-else>{{ item.key }}</span><span v-if="'anchor' in item"> · {{ item.anchor }}</span></p>
    <p v-if="'cause' in item">{{ item.cause === 'source' ? labels.sourceCause : labels[item.cause] }}</p>
    <p v-if="item.kind === 'document_removed'">{{ labels.textUnavailable }}<template v-if="item.replaced_by"> · {{ labels.replacedBy }}: {{ item.replaced_by }}</template></p>
    <p v-if="item.kind === 'document_key'">{{ labels.beforeKey }}: {{ item.before_key }}</p>
    <p v-if="'change' in item">{{ labels[item.change] }}</p>
    <template v-if="item.kind === 'document_validity'">
      <h4>{{ labels.validity }}</h4>
      <p v-for="entry in validityPairs" :key="entry.field">{{ fieldLabel(entry.field) }}: {{ pair(entry.value, entry.field) }}</p>
    </template>
    <template v-if="item.kind === 'provision_validity'">
      <h4>{{ labels.validity }}</h4>
      <p v-for="(value, field) in item.validity" :key="field">{{ fieldLabel(field) }}: {{ valueLabel(value, field) }}</p>
    </template>
    <template v-if="(item.kind === 'document_validity' || item.kind === 'provision_validity') && item.source_change">
      <h4>{{ labels.sourceChange }}</h4>
      <p v-if="item.source_change.before_key">{{ labels.beforeKey }}: {{ item.source_change.before_key }}</p>
      <p v-for="entry in sourcePairs" :key="entry.field">{{ fieldLabel(entry.field) }}: {{ pair(entry.value, entry.field) }}</p>
    </template>
    <template v-if="item.kind === 'provision_text'"><p>{{ labels.provenance }}: {{ valueLabel(item.provenance) }}</p><p>{{ labels.ambiguous }}: {{ valueLabel(item.ambiguous) }}</p></template>
  </article>
</template>
<style scoped>
.change-row { overflow-wrap: anywhere; }
</style>
