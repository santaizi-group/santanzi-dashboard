<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PublicMetricPoint } from '@santaizi/api'
import { AppEmpty } from '@santaizi/ui'
import { useResourceHistory } from '../composables/useResourceHistory'
import { formatSpeed } from '../domain/transfer'
import type { ServerStatusView } from '../domain/serverStatusView'
import ResourceHistoryChart, { type HistorySeries, type HistoryUnit } from './ResourceHistoryChart.vue'

type HistoryCard = {
  key: string
  title: string
  unit: HistoryUnit
  max?: number
  summary?: string
  detail?: string
  metrics?: Array<{ key: string; label: string; value: string; color: string }>
  series: HistorySeries[]
}

const props = defineProps<{ server: ServerStatusView }>()
const { t } = useI18n()
const {
  loading,
  failed,
  empty,
  range,
  points,
  hasCharts,
  load,
  disabledHistoryDate,
} = useResourceHistory(computed(() => props.server.id))

function token(name: string, fallback: string) {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim() || fallback
}

function colors() {
  return {
    cpu: token('--svc-accent', '#2563eb'),
    memory: token('--svc-good', '#16a34a'),
    disk: token('--svc-spark-3', '#d97706'),
    process: token('--svc-warn', '#d97706'),
    netIn: token('--svc-warn', '#d97706'),
    netOut: token('--svc-accent', '#2563eb'),
    tcp: token('--svc-accent', '#2563eb'),
    udp: token('--svc-spark-2', '#16a34a'),
  }
}

function formatPercent(value: number) {
  return `${Number(value).toFixed(1)}%`
}

function seriesOf(rows: PublicMetricPoint[], name: string, color: string, pick: (row: PublicMetricPoint) => number): HistorySeries {
  return {
    name,
    color,
    data: rows.map((row) => [row.window_start || '', pick(row)]),
  }
}

const cards = computed<HistoryCard[]>(() => {
  const rows = points.value
  const current = props.server
  const palette = colors()
  const memTotal = current.memory.total
  const diskTotal = current.disk.total
  return [
    {
      key: 'cpu',
      title: t('nazhua.historyCpu'),
      unit: 'percent',
      summary: formatPercent(current.cpu.percent),
      series: [seriesOf(rows, 'CPU', palette.cpu, (row) => Number(row.cpu || 0))],
    },
    {
      key: 'memory',
      title: t('nazhua.historyMemory'),
      unit: 'bytes',
      max: memTotal > 0 ? memTotal : undefined,
      summary: formatPercent(current.memory.percent),
      detail: `${current.memory.usedLabel} / ${current.memory.totalLabel}`,
      series: [seriesOf(rows, t('memory'), palette.memory, (row) => Number(row.mem_used || 0))],
    },
    {
      key: 'disk',
      title: t('nazhua.historyDisk'),
      unit: 'bytes',
      max: diskTotal > 0 ? diskTotal : undefined,
      summary: formatPercent(current.disk.percent),
      detail: `${current.disk.usedLabel} / ${current.disk.totalLabel}`,
      series: [seriesOf(rows, t('disk'), palette.disk, (row) => Number(row.disk_used || 0))],
    },
    {
      key: 'process',
      title: t('nazhua.historyProcess'),
      unit: 'count',
      summary: String(Math.round(current.processes ?? 0)),
      series: [seriesOf(rows, t('nazhua.processCount'), palette.process, (row) => Number(row.process_count || 0))],
    },
    {
      key: 'net',
      title: t('nazhua.historyNet'),
      unit: 'speed',
      metrics: [
        { key: 'in', label: t('nazhua.download'), value: formatSpeed(current.speedIn), color: palette.netIn },
        { key: 'out', label: t('nazhua.upload'), value: formatSpeed(current.speedOut), color: palette.netOut },
      ],
      series: [
        seriesOf(rows, t('nazhua.download'), palette.netIn, (row) => Number(row.net_in_speed || 0)),
        seriesOf(rows, t('nazhua.upload'), palette.netOut, (row) => Number(row.net_out_speed || 0)),
      ],
    },
    {
      key: 'conn',
      title: t('nazhua.historyConn'),
      unit: 'count',
      metrics: [
        { key: 'tcp', label: t('nazhua.tcpConn'), value: String(Math.round(current.tcp ?? 0)), color: palette.tcp },
        { key: 'udp', label: t('nazhua.udpConn'), value: String(Math.round(current.udp ?? 0)), color: palette.udp },
      ],
      series: [
        seriesOf(rows, t('nazhua.tcpConn'), palette.tcp, (row) => Number(row.tcp_conn_count || 0)),
        seriesOf(rows, t('nazhua.udpConn'), palette.udp, (row) => Number(row.udp_conn_count || 0)),
      ],
    },
  ]
})
</script>

<template>
  <section class="ss-detail__section ss-history">
    <header class="ss-history__head">
      <h3>{{ t('nazhua.resourceHistory') }}</h3>
      <el-date-picker
        v-model="range"
        class="ss-history__range"
        type="datetimerange"
        format="YYYY-MM-DD HH:mm"
        unlink-panels
        :clearable="false"
        :disabled-date="disabledHistoryDate"
        :start-placeholder="t('startedAt')"
        :end-placeholder="t('endedAt')"
        :aria-label="t('nazhua.resourceHistory')"
        @change="load"
      />
    </header>
    <div class="ss-history__body" v-loading="loading && hasCharts">
      <div v-if="!hasCharts && (loading || failed || empty)" class="ss-latency__empty">
        <AppEmpty
          :tone="failed ? 'danger' : 'default'"
          :icon="failed ? 'ri-error-warning-line' : loading ? 'ri-loader-4-line' : 'ri-line-chart-line'"
          :title="failed ? t('loadFailed') : ''"
          :description="t(failed ? 'requestFailed' : loading ? 'loading' : 'noData')"
        />
        <button v-if="failed" type="button" @click="load">
          <i class="ri-refresh-line"></i>{{ t('refresh') }}
        </button>
      </div>
      <div v-else-if="hasCharts" class="ss-history__grid">
        <article v-for="card in cards" :key="card.key" class="ss-history__card">
          <header>
            <strong>{{ card.title }}</strong>
            <div v-if="card.metrics" class="ss-history__metrics">
              <span
                v-for="metric in card.metrics"
                :key="metric.key"
                class="ss-history__metric"
                :style="{ '--metric-color': metric.color }"
              >
                <em></em>
                <span>{{ metric.label }}</span>
                <b>{{ metric.value }}</b>
              </span>
            </div>
            <div v-else class="ss-history__summary">
              <span>{{ card.summary }}</span>
              <small v-if="card.detail">{{ card.detail }}</small>
            </div>
          </header>
          <ResourceHistoryChart :series="card.series" :unit="card.unit" :max="card.max" />
        </article>
      </div>
    </div>
  </section>
</template>
