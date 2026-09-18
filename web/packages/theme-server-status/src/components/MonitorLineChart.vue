<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import * as echarts from 'echarts/core'
import { LineChart } from 'echarts/charts'
import { DataZoomComponent, GridComponent, LegendComponent, TooltipComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
import { useInView } from '../composables/useInView'
import type { NetworkSeries } from '../composables/useNetworkMonitor'

echarts.use([LineChart, DataZoomComponent, GridComponent, LegendComponent, TooltipComponent, CanvasRenderer])

const props = defineProps<{
  series: NetworkSeries[]
  compact?: boolean
  lazy?: boolean
}>()

const root = ref<HTMLElement>()
const node = ref<HTMLElement>()
const { load, keep } = useInView(root)
const chartReady = computed(() => !props.lazy || (load.value && keep.value))
let chart: echarts.ECharts | undefined
let resizeObserver: ResizeObserver | undefined
let themeObserver: MutationObserver | undefined

function onResize() {
  chart?.resize()
}

function token(name: string, fallback: string) {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim() || fallback
}

function palette() {
  const isDark = document.documentElement.classList.contains('dark') || document.documentElement.dataset.theme === 'dark'
  return {
    muted: token('--svc-muted', token('--sz-text-muted', isDark ? '#a8b2c3' : '#667085')),
    border: token('--svc-border', token('--sz-border', isDark ? '#263449' : '#e4e9f2')),
    text: token('--svc-text', token('--sz-text', isDark ? '#e8eef7' : '#1d2939')),
    surface: token('--svc-surface', token('--sz-surface', isDark ? '#121826' : '#ffffff')),
  }
}

function ready() {
  return chartReady.value
}

function draw() {
  if (!ready() || !node.value || !props.series.length) return
  if (!node.value.clientWidth || !node.value.clientHeight) return
  const colors = palette()
  const narrow = (node.value.clientWidth || 0) < 520
  if (!chart) chart = echarts.init(node.value)
  chart.setOption({
    backgroundColor: 'transparent',
    tooltip: {
      trigger: 'axis',
      valueFormatter: (value: unknown) => `${Number(value || 0).toFixed(2)} ms`,
      backgroundColor: colors.surface,
      borderColor: colors.border,
      textStyle: { color: colors.text, fontSize: 12 },
    },
    legend: props.compact
      ? { show: false }
      : { top: 4, textStyle: { color: colors.muted, fontSize: 12 } },
    grid: props.compact
      ? { left: 44, right: 12, top: 16, bottom: 40 }
      : { left: narrow ? 36 : 48, right: narrow ? 12 : 16, top: 40, bottom: 48 },
    dataZoom: [
      { type: 'inside' },
      { type: 'slider', height: 18, bottom: 8, borderColor: colors.border, textStyle: { color: colors.muted } },
    ],
    xAxis: {
      type: 'time',
      axisLabel: { color: colors.muted, fontSize: 12 },
      axisLine: { lineStyle: { color: colors.border } },
    },
    yAxis: {
      type: 'value',
      name: 'ms',
      nameTextStyle: { color: colors.muted },
      axisLabel: { color: colors.muted, fontSize: 12 },
      splitLine: { lineStyle: { color: colors.border } },
      axisLine: { lineStyle: { color: colors.border } },
    },
    series: props.series.map((item) => ({
      name: item.name,
      type: 'line',
      smooth: true,
      showSymbol: false,
      data: item.points,
    })),
  }, true)
}

function dispose() {
  resizeObserver?.disconnect()
  resizeObserver = undefined
  chart?.dispose()
  chart = undefined
}

function observe() {
  resizeObserver?.disconnect()
  if (node.value && typeof ResizeObserver !== 'undefined') {
    resizeObserver = new ResizeObserver(() => {
      if (!chart) draw()
      else chart.resize()
    })
    resizeObserver.observe(node.value)
  }
}

watch(() => props.series, async () => {
  await nextTick()
  if (!props.series.length || !ready()) {
    dispose()
    return
  }
  draw()
  observe()
}, { deep: true })

watch([load, keep], async () => {
  if (!ready()) {
    dispose()
    return
  }
  await nextTick()
  draw()
  observe()
})

onMounted(async () => {
  window.addEventListener('resize', onResize)
  await nextTick()
  if (ready()) {
    draw()
    observe()
  }
  if (typeof MutationObserver !== 'undefined') {
    themeObserver = new MutationObserver(() => requestAnimationFrame(draw))
    themeObserver.observe(document.documentElement, { attributes: true, attributeFilter: ['class', 'data-theme'] })
  }
})

onBeforeUnmount(() => {
  window.removeEventListener('resize', onResize)
  themeObserver?.disconnect()
  dispose()
})
</script>

<template>
  <div ref="root" class="ss-monitor-chart" :class="{ 'is-compact': compact }">
    <div v-show="chartReady" ref="node" class="ss-monitor-chart__canvas" />
  </div>
</template>
