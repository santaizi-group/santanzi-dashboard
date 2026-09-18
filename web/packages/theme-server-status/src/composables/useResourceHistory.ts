import { computed, ref, watch, type MaybeRefOrGetter, toValue } from 'vue'
import { getPublicMetrics, type PublicMetricPoint } from '@santaizi/api'

const DEFAULT_RANGE_MS = 24 * 3600 * 1000
const MAX_RANGE_MS = 30 * 24 * 3600 * 1000

function defaultHistoryRange(now = Date.now()): [Date, Date] {
  return [new Date(now - DEFAULT_RANGE_MS), new Date(now)]
}

export function useResourceHistory(serverId: MaybeRefOrGetter<number>) {
  const loading = ref(true)
  const failed = ref(false)
  const empty = ref(false)
  const range = ref<[Date, Date]>(defaultHistoryRange())
  const points = ref<PublicMetricPoint[]>([])
  const hasCharts = computed(() => points.value.length > 0)
  let requestSeq = 0

  function disabledHistoryDate(date: Date) {
    const now = Date.now()
    const time = date.getTime()
    return time > now || time < now - MAX_RANGE_MS
  }

  function rangeQuery(): { start: string; end: string } | null {
    const value = range.value
    if (!value?.[0] || !value[1]) return null
    const start = value[0].getTime()
    const end = value[1].getTime()
    if (!(end > start) || end - start > MAX_RANGE_MS) return null
    return { start: value[0].toISOString(), end: value[1].toISOString() }
  }

  async function load() {
    if (!toValue(serverId)) return
    const query = rangeQuery()
    if (!query) return
    const seq = ++requestSeq
    loading.value = true
    if (!points.value.length) {
      failed.value = false
      empty.value = false
    }
    try {
      const result = await getPublicMetrics(toValue(serverId), { resolution: '1m', ...query })
      if (seq !== requestSeq) return
      points.value = result.data || []
      empty.value = !points.value.length
      failed.value = false
    } catch {
      if (seq !== requestSeq) return
      failed.value = true
      if (!points.value.length) empty.value = false
    } finally {
      if (seq === requestSeq) loading.value = false
    }
  }

  watch(() => toValue(serverId), () => {
    range.value = defaultHistoryRange()
    points.value = []
    void load()
  }, { immediate: true })

  return {
    loading,
    failed,
    empty,
    range,
    points,
    hasCharts,
    load,
    disabledHistoryDate,
  }
}
