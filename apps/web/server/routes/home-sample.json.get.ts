import { homeSample } from '../utils/home-sample'

export default defineEventHandler(() => homeSample(useRuntimeConfig().apiOrigin))
