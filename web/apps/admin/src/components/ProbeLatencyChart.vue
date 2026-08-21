<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import * as echarts from 'echarts/core'
import { LineChart } from 'echarts/charts'
import { GridComponent, MarkPointComponent, TooltipComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
import type { ProbeSampleBucket } from '@santaizi/api'
import { AppEmpty } from '@santaizi/ui'
import { formatLatencyMs } from '@/composables/format'
import { probeLatencyStats } from '@/domain/probePath'

echarts.use([LineChart, GridComponent, MarkPointComponent, TooltipComponent, CanvasRenderer])

const props = defineProps<{
  title: string
  points: ProbeSampleBucket[]
}>()

const { t, locale } = useI18n()
const node = ref<HTMLElement>()
let chart: echarts.ECharts | undefined
let resizeObserver: ResizeObserver | undefined
let themeObserver: MutationObserver | undefined

const validPoints = computed(() => props.points.filter((point) => {
  return Number(point.success_count) > 0 && Number.isFinite(Number(point.avg_ms))
}))
const hasData = computed(() => validPoints.value.length > 0)
const stats = computed(() => probeLatencyStats(props.points))

function format(value?: number) {
  return formatLatencyMs(value, locale.value)
}

function formatMarkValue(value: unknown) {
  const raw = Array.isArray(value) ? value[value.length - 1] : value
  return format(raw == null ? undefined : Number(raw))
}

function color(name: string, fallback: string) {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim() || fallback
}

function chartData() {
  return props.points.map((point) => {
    const avg = Number(point.avg_ms)
    const value = Number(point.success_count) > 0 && Number.isFinite(avg) ? avg : null
    return [point.bucket_start, value]
  })
}

function draw() {
  if (!node.value || !hasData.value || !node.value.clientWidth || !node.value.clientHeight) return

  const primary = color('--sz-primary', '#2563eb')
  const muted = color('--sz-text-muted', '#667085')
  const border = color('--sz-border', '#e4e9f2')
  const narrow = node.value.clientWidth < 520

  if (!chart) chart = echarts.init(node.value)
  chart.setOption({
    backgroundColor: 'transparent',
    animation: false,
    tooltip: {
      trigger: 'axis',
      valueFormatter: (value: unknown) => format(value == null ? undefined : Number(value)),
    },
    grid: {
      left: narrow ? 42 : 52,
      right: narrow ? 16 : 24,
      top: 32,
      bottom: 30,
      containLabel: true,
    },
    xAxis: {
      type: 'time',
      axisLabel: { color: muted, hideOverlap: true },
      axisLine: { lineStyle: { color: border } },
      splitLine: { show: false },
    },
    yAxis: {
      type: 'value',
      name: 'ms',
      nameTextStyle: { color: muted },
      axisLabel: { color: muted },
      axisLine: { show: false },
      splitLine: { lineStyle: { color: border } },
    },
    series: [{
      name: props.title,
      type: 'line',
      smooth: true,
      showSymbol: false,
      connectNulls: false,
      data: chartData(),
      lineStyle: { color: primary, width: 2 },
      itemStyle: { color: primary },
      markPoint: {
        symbol: 'circle',
        symbolSize: 8,
        itemStyle: { color: primary },
        label: {
          color: muted,
          fontSize: 12,
          formatter: (params: { value?: unknown }) => formatMarkValue(params.value),
        },
        data: [
          { type: 'max', name: t('maxMs') },
          { type: 'min', name: t('minMs') },
        ],
      },
    }],
  }, true)
}

function onResize() {
  if (chart) chart.resize()
  else draw()
}

function observeChart() {
  resizeObserver?.disconnect()
  if (!node.value || typeof ResizeObserver === 'undefined') return
  resizeObserver = new ResizeObserver(() => onResize())
  resizeObserver.observe(node.value)
}

watch(() => props.points, async () => {
  await nextTick()
  if (!hasData.value) {
    chart?.clear()
    return
  }
  draw()
  observeChart()
}, { deep: true })

onMounted(async () => {
  window.addEventListener('resize', onResize)
  await nextTick()
  draw()
  observeChart()
  if (typeof MutationObserver !== 'undefined') {
    themeObserver = new MutationObserver(() => draw())
    themeObserver.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
  }
})

onBeforeUnmount(() => {
  window.removeEventListener('resize', onResize)
  resizeObserver?.disconnect()
  themeObserver?.disconnect()
  chart?.dispose()
  chart = undefined
})
</script>

<template>
  <section class="probe-latency-chart">
    <header class="probe-latency-chart__head">
      <h3>{{ title }}</h3>
      <dl v-if="hasData" class="probe-latency-chart__stats">
        <div>
          <dt>{{ t('maxMs') }}</dt>
          <dd>{{ format(stats.max) }}</dd>
        </div>
        <div>
          <dt>{{ t('minMs') }}</dt>
          <dd>{{ format(stats.min) }}</dd>
        </div>
        <div>
          <dt>{{ t('avgMs') }}</dt>
          <dd>{{ format(stats.avg) }}</dd>
        </div>
      </dl>
    </header>
    <div v-if="hasData" ref="node" class="probe-latency-chart__canvas" />
    <AppEmpty v-else icon="ri-line-chart-line" :description="t('noData')" />
  </section>
</template>
