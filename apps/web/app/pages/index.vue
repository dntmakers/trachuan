<script setup lang="ts">
import { homeGraph } from '../utils/structured-data'
import { getCoverage } from '../api/coverage'
import { publicStatus } from '../../server/utils/public-status'
import { loginUrl } from '../api/next'
import { useSessionStore } from '../stores/session'
import { messagesFor, type Language } from '../../i18n'
import { homeMessages } from '../../i18n/home'
import { documentCitation, formatDate } from '../content'
useHead({ htmlAttrs: { 'data-page': 'home' } })
const session = useSessionStore()
const mounted = ref(false)
onMounted(() => { mounted.value = true })
const searchLink = computed(() => mounted.value && session.status === 'signed-in' ? '/vn/search' : loginUrl('/vn/search'))
const { data: sample } = await useAsyncData('home-sample', () => $fetch('/home-sample.json'), { server: true })
const hit = computed(() => sample.value?.result.hits[0])
const doc = computed(() => sample.value?.result.documents['vn:vbpl:179252'])
const citation = computed(() => doc.value ? documentCitation(hit.value?.handle.split('#', 2)[1], doc.value) : null)
const hasSource = computed(() => !!doc.value?.source_url)
const language = useState<Language>('language', () => 'vi')
const origin = useRuntimeConfig().public.siteOrigin.replace(/\/$/, '')
useStructuredData(() => homeGraph(origin, language.value))
const messages = computed(() => messagesFor(language.value))
const labels = computed(() => homeMessages(language.value))
const route = useRoute()
const deleted = ref(false)
onMounted(() => { deleted.value = route.query.account === 'deleted' })
const { data: status } = await useAsyncData('coverage-home', () => import.meta.server ? getCoverage(() => publicStatus(useRuntimeConfig().apiOrigin).then(data => ({ data }))).then(status => ({ status })) : Promise.resolve({ status: null }), { server: true, default: () => ({ status: null }) })
</script>
<template>
  <div class="home-page">
    <p v-if="deleted" class="container notice" role="status">{{ messages.deletedNotice }}</p>
    <HomeHero />
    <div v-if="sample && hit" class="container home-sample"><span v-if="citation" class="home-citation">{{ citation }}</span><ResultCard :hit="hit" :documents="sample.result.documents" :status-date="sample.statusDate" current-release home /></div>
    <section v-if="sample" class="home-explanation" aria-labelledby="home-sample-title"><div class="container">
      <h2 id="home-sample-title">{{ (sample.stored ? labels.storedSampleTitle : labels.sampleTitle).replace('{date}', formatDate(sample.releaseDate, language)) }}</h2><p>{{ labels.sampleText }}</p><ul><li>{{ labels.check1 }}</li><li v-if="citation">{{ labels.check2.replace('{citation}', citation) }}</li><li v-if="hasSource">{{ labels.check3 }}</li></ul>
    </div></section>
    <div class="container home-coverage"><CoverageFacts :status="status.status" /><p>{{ labels.scope }} <NuxtLink to="/sources">{{ labels.scopeLink }}</NuxtLink></p></div>
    <HomeConnect />
    <section class="container home-maker"><div class="home-maker-panel"><img src="/maker-avatar-320.webp" srcset="/maker-avatar-320.webp 320w, /maker-avatar-640.webp 640w" sizes="(min-width: 64rem) 232px, (min-width: 48rem) 176px, 168px" :alt="labels.makerAlt"><div><h2>{{ labels.makerTitle }}</h2><p>{{ labels.makerText }}</p><div class="home-maker-actions"><NuxtLink class="btn btn-primary" :to="searchLink">{{ labels.makerSearch }}</NuxtLink><a class="btn home-outline" href="https://aboutme.vn/danny">{{ labels.makerResume }}</a><a class="btn home-outline" href="mailto:support@trachuan.vn">{{ labels.makerFeedback }}</a></div></div></div></section>
  </div>
</template>
