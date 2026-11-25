<script setup lang="ts">
import type { components } from '@trachuan/schema/gen/openapi'
import { formatNumber } from '../content'
import { messagesFor, type Language } from '../../i18n'
import { searchMessages } from '../../i18n/search'
import SearchNodePicker from './SearchNodePicker.vue'

defineProps<{ inForce: boolean, asOf: string, issuedFrom: string, issuedTo: string, issuedError: boolean, nodes: string[], docTypes: string[], issuers: string[], facets: components['schemas']['CorpusStatus'] | null, failed: boolean }>()
const emit = defineEmits<{ 'update:inForce': [value: boolean], 'update:asOf': [value: string], 'update:issuedFrom': [value: string], 'update:issuedTo': [value: string], 'update:nodes': [value: string[]], 'update:docTypes': [value: string[]], 'update:issuers': [value: string[]], retry: [], labels: [value: Record<string, string>] }>()
const asOfInput = ref<HTMLInputElement | null>(null)
const fromInput = ref<HTMLInputElement | null>(null)
const toInput = ref<HTMLInputElement | null>(null)
defineExpose({
  invalidDates: () => ({ asOf: !!asOfInput.value?.validity.badInput, from: !!fromInput.value?.validity.badInput, to: !!toInput.value?.validity.badInput }),
  clearDates: () => { for (const input of [asOfInput.value, fromInput.value, toInput.value]) if (input) input.value = '' },
})
function updateDate(bound: 'asOf' | 'from' | 'to', event: Event) {
  const input = event.target as HTMLInputElement
  if (bound === 'asOf') emit('update:asOf', input.value)
  else if (bound === 'from') emit('update:issuedFrom', input.value)
  else emit('update:issuedTo', input.value)
}
const language = useState<Language>('language', () => 'vi')
const messages = computed(() => messagesFor(language.value))
const labels = computed(() => searchMessages(language.value))
const allTypes = ref(false)
const allIssuers = ref(false)
function toggle(list: string[], value: string, group: 'docTypes' | 'issuers') {
  const next = list.includes(value) ? list.filter(item => item !== value) : list.length < 10 ? [...list, value] : list
  if (group === 'docTypes') emit('update:docTypes', next)
  else emit('update:issuers', next)
}
</script>

<template>
  <div class="search-filters">
    <h2>{{ labels.filters }}</h2>
    <fieldset><legend>{{ labels.validity }}</legend>
      <label><input type="checkbox" :checked="inForce" @change="emit('update:inForce', ($event.target as HTMLInputElement).checked)"> {{ messages.searchInForce }}</label>
      <label>{{ messages.searchDate }} <span>{{ labels.dateHint }}</span> <input ref="asOfInput" class="input" type="date" :value="asOf" :aria-invalid="issuedError" @input="updateDate('asOf', $event)"></label>
      <label>{{ labels.issuedFrom }} <input ref="fromInput" class="input" type="date" :value="issuedFrom" :aria-invalid="issuedError" @input="updateDate('from', $event)"></label>
      <label>{{ labels.issuedTo }} <input ref="toInput" class="input" type="date" :value="issuedTo" :aria-invalid="issuedError" @input="updateDate('to', $event)"></label>
      <p v-if="issuedError" role="alert">{{ labels.issuedInvalid }}</p>
    </fieldset>
    <p v-if="failed" class="filter-error" role="status">{{ messages.filtersUnavailable }} <button type="button" @click="emit('retry')">{{ messages.retry }}</button></p>
    <fieldset v-for="group in ['docTypes', 'issuers'] as const" :key="group">
      <legend>{{ group === 'docTypes' ? messages.searchTypes : messages.searchIssuers }}</legend>
      <label v-for="facet in (group === 'docTypes' ? facets?.doc_types : facets?.issuers)?.slice(0, (group === 'docTypes' ? allTypes : allIssuers) ? undefined : 6)" :key="facet.value" class="facet-option">
        <input type="checkbox" :checked="(group === 'docTypes' ? docTypes : issuers).includes(facet.value || '')" :disabled="!(group === 'docTypes' ? docTypes : issuers).includes(facet.value || '') && (group === 'docTypes' ? docTypes : issuers).length >= 10" @change="toggle(group === 'docTypes' ? docTypes : issuers, facet.value || '', group)">
        <span>{{ facet.value }}</span><span class="facet-count">{{ formatNumber(facet.count, language) }}</span>
      </label>
      <button v-if="(group === 'docTypes' ? facets?.doc_types : facets?.issuers)?.length && (group === 'docTypes' ? facets!.doc_types!.length : facets!.issuers!.length) > 6" type="button" class="filter-more" @click="group === 'docTypes' ? allTypes = !allTypes : allIssuers = !allIssuers">{{ (group === 'docTypes' ? allTypes : allIssuers) ? labels.showLess : labels.showAll }}</button>
    </fieldset>
    <SearchNodePicker :model-value="nodes" @update:model-value="emit('update:nodes', $event)" @labels="emit('labels', $event)" />
  </div>
</template>
