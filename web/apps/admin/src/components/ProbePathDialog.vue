<script setup lang="ts">
import { computed, onUnmounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ProbeMTRHop, ProbePath, ProbeRouteHistory, ProbeRouteHop, ProbeRouteRecord, ProbeSampleBucket, ProbeTrace } from '@santaizi/api'
import { AppDialog, AppEmpty } from '@santaizi/ui'
import ProbeLatencyChart from '@/components/ProbeLatencyChart.vue'
import { createProbeRoute, getProbeRoute, getProbeTrace, listProbeSamples } from '@/api/adminApi'
import { formatAdminValue } from '@/composables/format'
import { notifyAPIError } from '@/composables/notify'
import {
  defaultProbeRouteProtocol,
  formatProbeLoss,
  hopGeoText,
  probeBestTCP,
  probeICMPMetric,
  probeLossPercent,
  probeMTRMetric,
  probeTCPMetric,
  probeTargetText,
  type ProbeMetricTone,
  type ProbeRouteProtocol,
} from '@/domain/probePath'

const props = defineProps<{
  modelValue: boolean
  path?: ProbePath
}>()
const emit = defineEmits<{ 'update:modelValue': [boolean] }>()
const { t, te, locale } = useI18n()
const loading = ref(false)
const tracing = ref(false)
const tab = ref('latency')
const routeProtocol = ref<ProbeRouteProtocol>('icmp')
const nexttraceProtocol = ref<ProbeRouteProtocol>('icmp')
const selectedRouteId = ref<number | undefined>()
const samples = ref<ProbeSampleBucket[]>([])
const icmpBuckets = ref<ProbeSampleBucket[]>([])
const tcpBuckets = ref<ProbeSampleBucket[]>([])
const trace = ref<ProbeTrace | null>(null)
const routeHistory = ref<ProbeRouteHistory | null>(null)
const meta = reactive({ page: 1, page_size: 20, total: 0 })
let pollTimer: ReturnType<typeof setTimeout> | undefined

const icmpMetric = computed(() => props.path ? probeICMPMetric(props.path, locale.value, t('probeTimeout')) : { text: '—', tone: '' as const })
const tcpMetric = computed(() => props.path ? probeTCPMetric(props.path, locale.value, t('probeTimeout')) : { text: '—', tone: '' as const })
const activeHops = computed<ProbeMTRHop[]>(() => {
  if (routeProtocol.value === 'tcp') return trace.value?.tcp?.hops || []
  return trace.value?.icmp?.hops || trace.value?.hops || []
})
const mtrMetric = computed(() => {
  const hops = activeHops.value
  if (hops.length) {
    const loss = hops[hops.length - 1]?.loss ?? 0
    return { text: formatProbeLoss(loss, locale.value), tone: (loss > 0 ? 'is-fail' : 'is-ok') as ProbeMetricTone }
  }
  return props.path ? probeMTRMetric(props.path, locale.value) : { text: '—', tone: '' as const }
})
const tcpLabel = computed(() => tcpMetric.value.port != null ? `${t('tcp')} :${tcpMetric.value.port}` : t('tcp'))
const tcpChart = computed(() => {
  if (!props.path) return tcpBuckets.value
  const port = probeBestTCP(props.path)?.port ?? props.path.tcp?.[0]?.port
  if (!port) return tcpBuckets.value
  const matched = tcpBuckets.value.filter(row => row.port === port)
  return matched.length ? matched : tcpBuckets.value
})
const icmpRouteDisabled = computed(() => !trace.value?.icmp?.hops?.length)
const tcpRouteDisabled = computed(() => !trace.value?.tcp?.hops?.length)
const routeOptions = computed(() => [
  { label: t('icmp'), value: 'icmp', disabled: icmpRouteDisabled.value },
  { label: tcpRouteLabel.value, value: 'tcp', disabled: tcpRouteDisabled.value },
])
const tcpRouteLabel = computed(() => {
  const port = trace.value?.tcp?.port || trace.value?.port
  return port ? `${t('tcp')} :${port}` : t('tcp')
})
const icmpNexttraceOn = computed(() => props.path?.enable_icmp !== false && routeHistory.value?.enable_icmp !== false)
const tcpNexttraceOn = computed(() => props.path?.enable_tcp !== false && routeHistory.value?.enable_tcp !== false)
const nexttraceOptions = computed(() => [
  { label: t('icmp'), value: 'icmp', disabled: !icmpNexttraceOn.value },
  { label: t('tcp'), value: 'tcp', disabled: !tcpNexttraceOn.value },
])
const routeRecords = computed<ProbeRouteRecord[]>(() => {
  if (!routeHistory.value) return []
  return nexttraceProtocol.value === 'tcp' ? routeHistory.value.tcp || [] : routeHistory.value.icmp || []
})
const selectedRoute = computed(() => routeRecords.value.find(row => row.id === selectedRouteId.value) || routeRecords.value[0])
const nexttraceHops = computed<ProbeRouteHop[]>(() => selectedRoute.value?.hops || [])
const nexttracePending = computed(() => routeHistory.value?.job?.status === 'pending' && routeHistory.value.job.protocol === nexttraceProtocol.value)
const canTrace = computed(() => nexttraceProtocol.value === 'tcp' ? tcpNexttraceOn.value : icmpNexttraceOn.value)

function pretty(value: unknown, key = '') {
  return formatAdminValue(value, key, locale.value, t as never, te)
}

function hopLabel(hop: ProbeMTRHop | ProbeRouteHop) {
  return hopGeoText(hop, t('hopPrivate'))
}

function stopPoll() {
  if (pollTimer) {
    clearTimeout(pollTimer)
    pollTimer = undefined
  }
}

async function loadRoute() {
  const path = props.path
  if (!path) return
  routeHistory.value = await getProbeRoute({ collector_id: path.collector_id, server_id: path.server_id })
  if (!routeRecords.value.some(row => row.id === selectedRouteId.value)) {
    selectedRouteId.value = routeRecords.value[0]?.id
  }
}

async function load() {
  const path = props.path
  if (!path || !props.modelValue) return
  loading.value = true
  try {
    const [icmpList, tcpList, sampleList, nextTrace] = await Promise.all([
      listProbeSamples({ collector_id: path.collector_id, server_id: path.server_id, kind: 'icmp', page_size: 60 }),
      listProbeSamples({ collector_id: path.collector_id, server_id: path.server_id, kind: 'tcp', page_size: 60 }),
      listProbeSamples({
        collector_id: path.collector_id, server_id: path.server_id,
        page: meta.page, page_size: meta.page_size,
      }),
      getProbeTrace({ collector_id: path.collector_id, server_id: path.server_id }),
    ])
    icmpBuckets.value = [...icmpList.data].reverse()
    tcpBuckets.value = [...tcpList.data].reverse()
    samples.value = sampleList.data
    meta.total = sampleList.meta.total || sampleList.data.length
    trace.value = nextTrace
    routeProtocol.value = defaultProbeRouteProtocol(nextTrace)
    await loadRoute()
    if (!icmpNexttraceOn.value && tcpNexttraceOn.value) nexttraceProtocol.value = 'tcp'
  } catch (error) {
    notifyAPIError(error, t as never, te)
  } finally {
    loading.value = false
  }
}

async function runTrace() {
  const path = props.path
  if (!path || !canTrace.value) return
  tracing.value = true
  try {
    await createProbeRoute({ collector_id: path.collector_id, server_id: path.server_id, protocol: nexttraceProtocol.value })
    await pollRoute(0)
  } catch (error) {
    notifyAPIError(error, t as never, te)
    tracing.value = false
  }
}

async function pollRoute(attempt: number) {
  if (!props.modelValue) {
    tracing.value = false
    return
  }
  try {
    await loadRoute()
  } catch (error) {
    notifyAPIError(error, t as never, te)
    tracing.value = false
    return
  }
  if (!nexttracePending.value || attempt >= 30) {
    tracing.value = false
    return
  }
  pollTimer = setTimeout(() => { void pollRoute(attempt + 1) }, 2000)
}

watch(() => [props.modelValue, props.path?.collector_id, props.path?.server_id], () => {
  stopPoll()
  tracing.value = false
  if (!props.modelValue || !props.path) return
  meta.page = 1
  tab.value = 'latency'
  routeProtocol.value = 'icmp'
  nexttraceProtocol.value = 'icmp'
  selectedRouteId.value = undefined
  trace.value = null
  routeHistory.value = null
  icmpBuckets.value = []
  tcpBuckets.value = []
  void load()
})

watch(nexttraceProtocol, () => {
  selectedRouteId.value = routeRecords.value[0]?.id
})

onUnmounted(stopPoll)
</script>

<template>
  <AppDialog :model-value="modelValue" :title="path?.server_name || t('probeObservation')" mode="view" width="min(920px, 96vw)" @update:model-value="emit('update:modelValue', $event)">
    <div v-if="path" class="probe-dialog" v-loading="loading">
      <p class="probe-dialog-meta">
        <span>{{ path.collector_name }}</span>
        <span>{{ probeTargetText(path) }}</span>
        <span>{{ pretty(path.sampled_at, 'sampled_at') }}</span>
      </p>
      <el-alert v-if="path.last_error" type="error" :closable="false" :title="path.last_error" />
      <div class="probe-kpi">
        <div class="probe-metric" :class="icmpMetric.tone">
          <span class="probe-metric__label">{{ t('icmp') }}</span>
          <span class="probe-metric__value">{{ icmpMetric.text }}</span>
        </div>
        <div class="probe-metric" :class="tcpMetric.tone">
          <span class="probe-metric__label">{{ tcpLabel }}</span>
          <span class="probe-metric__value">{{ tcpMetric.text }}</span>
        </div>
        <div class="probe-metric" :class="mtrMetric.tone">
          <span class="probe-metric__label">{{ t('probeTrace') }}</span>
          <span class="probe-metric__value">{{ mtrMetric.text }}</span>
        </div>
      </div>
      <div v-if="path.tcp && path.tcp.length > 1" class="probe-tcp-chips">
        <span v-for="row in path.tcp" :key="row.port" class="probe-tcp-chip" :class="row.ok ? 'is-ok' : 'is-fail'">
          :{{ row.port }} {{ row.ok ? pretty(row.rtt_ms, 'rtt_ms') : t('probeTimeout') }}
        </span>
      </div>
      <el-tabs v-model="tab">
        <el-tab-pane :label="t('latency')" name="latency">
          <ProbeLatencyChart v-if="icmpBuckets.length" :title="t('icmp')" :points="icmpBuckets" />
          <ProbeLatencyChart v-if="tcpChart.length" :title="tcpLabel" :points="tcpChart" />
          <AppEmpty v-if="!icmpBuckets.length && !tcpChart.length" icon="ri-timer-line" :description="t('noData')" />
        </el-tab-pane>
        <el-tab-pane :label="t('probeRoute')" name="route">
          <el-segmented v-if="trace?.icmp || trace?.tcp" v-model="routeProtocol" :options="routeOptions" class="probe-route-switch" />
          <ol v-if="activeHops.length" class="probe-hops">
            <li v-for="(hop, index) in activeHops" :key="`${hop.ttl}-${hop.address || ''}-${index}`" class="probe-hop">
              <span class="probe-hop__ttl">{{ hop.ttl }}</span>
              <span class="probe-hop__addr" :title="[hop.address, hopLabel(hop)].filter(Boolean).join(' · ')">
                <span class="probe-hop__ip">{{ hop.address || '—' }}</span>
                <span v-if="hopLabel(hop)" class="probe-hop__geo">{{ hopLabel(hop) }}</span>
              </span>
              <span class="probe-hop__rtt">{{ pretty(hop.avg_ms, 'avg_ms') }}</span>
              <span class="probe-hop__track" aria-hidden="true"><span class="probe-hop__fill" :style="{ width: `${probeLossPercent(hop.loss)}%` }"></span></span>
              <span class="probe-hop__loss">{{ formatProbeLoss(hop.loss, locale) }}</span>
            </li>
          </ol>
          <AppEmpty v-else icon="ri-route-line" :description="t('noData')" />
        </el-tab-pane>
        <el-tab-pane :label="t('probeRouteTrace')" name="nexttrace">
          <div class="probe-route-toolbar">
            <el-segmented v-model="nexttraceProtocol" :options="nexttraceOptions" />
            <el-button type="primary" :disabled="!canTrace || tracing" :loading="tracing" @click="runTrace">
              <i class="ri-route-line"></i>{{ t('probeRouteRun') }}
            </el-button>
          </div>
          <div v-if="routeRecords.length" class="probe-route-runs">
            <el-button
              v-for="row in routeRecords"
              :key="row.id"
              size="small"
              :class="{ 'is-current': selectedRoute?.id === row.id }"
              class="probe-route-run"
              @click="selectedRouteId = row.id"
            >{{ pretty(row.sampled_at, 'sampled_at') }}</el-button>
          </div>
          <p v-if="selectedRoute?.error" class="probe-route-error">{{ selectedRoute.error }}</p>
          <ol v-if="nexttraceHops.length" class="probe-hops">
            <li v-for="(hop, index) in nexttraceHops" :key="`${hop.ttl}-${hop.address || ''}-${index}`" class="probe-hop">
              <span class="probe-hop__ttl">{{ hop.ttl }}</span>
              <span class="probe-hop__addr" :title="[hop.address, hop.hostname, hopLabel(hop)].filter(Boolean).join(' · ')">
                <span class="probe-hop__ip">{{ hop.address || '—' }}</span>
                <span v-if="hop.hostname" class="probe-hop__geo">{{ hop.hostname }}</span>
                <span v-if="hopLabel(hop)" class="probe-hop__geo">{{ hopLabel(hop) }}</span>
              </span>
              <span class="probe-hop__rtt">{{ pretty(hop.rtt_ms, 'rtt_ms') }}</span>
              <span class="probe-hop__track" aria-hidden="true"><span class="probe-hop__fill" :style="{ width: `${probeLossPercent(hop.loss)}%` }"></span></span>
              <span class="probe-hop__loss">{{ formatProbeLoss(hop.loss, locale) }}</span>
            </li>
          </ol>
          <AppEmpty v-else-if="!selectedRoute?.error" icon="ri-route-line" :description="t('probeRouteEmpty')" />
        </el-tab-pane>
        <el-tab-pane :label="t('probeRecords')" name="records">
          <el-table :data="samples" class="dataset-table">
            <el-table-column :label="t('bucketStart')" min-width="180"><template #default="{row}">{{ pretty(row.bucket_start, 'bucket_start') }}</template></el-table-column>
            <el-table-column :label="t('type')" width="80"><template #default="{row}">{{ pretty(row.kind, 'kind') }}</template></el-table-column>
            <el-table-column :label="t('tcp')" width="80"><template #default="{row}">{{ row.port || '—' }}</template></el-table-column>
            <el-table-column :label="t('minMs')" width="90"><template #default="{row}">{{ pretty(row.min_ms, 'min_ms') }}</template></el-table-column>
            <el-table-column :label="t('avgMs')" width="90"><template #default="{row}">{{ pretty(row.avg_ms, 'avg_ms') }}</template></el-table-column>
            <el-table-column :label="t('maxMs')" width="90"><template #default="{row}">{{ pretty(row.max_ms, 'max_ms') }}</template></el-table-column>
            <el-table-column :label="t('loss')" width="80"><template #default="{row}">{{ formatProbeLoss(row.loss, locale) }}</template></el-table-column>
            <template #empty><AppEmpty icon="ri-timer-line" :description="t('noData')" /></template>
          </el-table>
          <div v-if="meta.total" class="pagination">
            <el-pagination v-model:current-page="meta.page" v-model:page-size="meta.page_size" layout="total, prev, pager, next" :total="meta.total" @change="load"/>
          </div>
        </el-tab-pane>
      </el-tabs>
    </div>
  </AppDialog>
</template>
