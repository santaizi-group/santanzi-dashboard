import { computed, onBeforeUnmount, ref, watch, type Ref } from 'vue'
import type { MonitorHistory } from '@santaizi/api'
import { loadNetworkHistory } from '../domain/networkHistoryCache'

export const NETWORK_RANGES = [
  { hours: 0.5, labelKey: 'nazhua.range30m' },
  { hours: 1, labelKey: 'nazhua.range1h' },
  { hours: 3, labelKey: 'nazhua.range3h' },
  { hours: 6, labelKey: 'nazhua.range6h' },
  { hours: 12, labelKey: 'nazhua.range12h' },
  { hours: 24, labelKey: 'nazhua.range24h' },
] as const

export type NetworkSeries = {
  name: string
  points: Array<[number, number]>
  average: number
}

function toMs(value: unknown) {
  if (typeof value === 'number' && Number.isFinite(value)) {
    return value < 1e12 ? value * 1000 : value
  }
  const parsed = Date.parse(String(value || ''))
  return Number.isFinite(parsed) ? parsed : NaN
}

export function percentile(values: number[], ratio: number) {
  if (!values.length) return 0
  const sorted = [...values].sort((left, right) => left - right)
  const index = Math.min(sorted.length - 1, Math.max(0, Math.ceil(sorted.length * ratio) - 1))
  return sorted[index] ?? 0
}

export function cutPeakValues(values: number[], factor = 1.35) {
  const finite = values.filter((value) => Number.isFinite(value))
  if (finite.length < 8) return values
  const cap = percentile(finite, 0.95) * factor
  if (!(cap > 0)) return values
  return values.map((value) => (Number.isFinite(value) && value > cap ? cap : value))
}

export function seriesFromHistory(rows: MonitorHistory[], hours: number, cutPeak: boolean, now = Date.now()): NetworkSeries[] {
  const from = now - hours * 3_600_000
  return rows.map((row, index) => {
    const times = (row.created_at as unknown[]) || []
    const delays = ((row.avg_delay as unknown[]) || []).map((value) => Number(value))
    const clipped = cutPeak ? cutPeakValues(delays) : delays
    const points: Array<[number, number]> = []
    let total = 0
    for (let cursor = 0; cursor < times.length; cursor += 1) {
      const time = toMs(times[cursor])
      const delay = clipped[cursor]
      if (!Number.isFinite(time) || time < from || delay === undefined || !Number.isFinite(delay)) continue
      points.push([time, delay])
      total += delay
    }
    return {
      name: String(row.monitor_name || `monitor-${index + 1}`),
      points,
      average: points.length ? total / points.length : 0,
    }
  }).filter((item) => item.points.length > 0)
}

export function useNetworkMonitor(serverId: Ref<number>) {
  const rows = ref<MonitorHistory[]>([])
  const loading = ref(false)
  const failed = ref(false)
  const aggregated = ref(false)
  const autoRefresh = ref(true)
  const cutPeak = ref(false)
  const hours = ref(24)
  let requestSeq = 0
  let timer: number | undefined

  const series = computed(() => seriesFromHistory(rows.value, hours.value, cutPeak.value))
  const empty = computed(() => !loading.value && !failed.value && series.value.length === 0)

  async function load(force = false) {
    if (!serverId.value) return
    const seq = ++requestSeq
    loading.value = true
    failed.value = false
    try {
      const data = await loadNetworkHistory(serverId.value, { force })
      if (seq !== requestSeq) return
      rows.value = data
    } catch {
      if (seq !== requestSeq) return
      failed.value = true
      rows.value = []
    } finally {
      if (seq === requestSeq) loading.value = false
    }
  }

  function clearTimer() {
    if (timer !== undefined) {
      window.clearInterval(timer)
      timer = undefined
    }
  }

  function syncTimer() {
    clearTimer()
    if (autoRefresh.value) timer = window.setInterval(() => { void load(true) }, 60_000)
  }

  watch(serverId, () => { void load() }, { immediate: true })
  watch(autoRefresh, syncTimer, { immediate: true })
  onBeforeUnmount(clearTimer)

  return {
    loading,
    failed,
    empty,
    aggregated,
    autoRefresh,
    cutPeak,
    hours,
    series,
    load,
  }
}
