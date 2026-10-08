<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { getAdminSummary, getSettings, isHostOnline } from '@santaizi/api'
import type { CollectorRecord, ConnectionPath, IncidentRecord, ServerRecord } from '@santaizi/api'
import { AppDrawer, AppEmpty } from '@santaizi/ui'
import { listAllServersPaged, listCollectors, listConnectionPaths, telemetryList } from '@/api/adminApi'
import TopologyGlobe from '@/components/TopologyGlobe.vue'
import logoUrl from '@/assets/logo.svg?url'
import { formatAdminValue, formatLatencyMs } from '@/composables/format'
import { notifyAPIError } from '@/composables/notify'
import { missedObserverText, type ObserverSight } from '@/domain/incidents'
import { buildTopology, primaryLatencyRows, type NodeLatencyRow, type TopologyGraph, type TopologyMarker } from '@/domain/topology'

const { t, te, locale } = useI18n()
const router = useRouter()
const loading = ref(false)
const summary = ref<Record<string, unknown>>({})
const servers = ref<ServerRecord[]>([])
const collectors = ref<CollectorRecord[]>([])
const paths = ref<ConnectionPath[]>([])
const primaryLocation = ref('')
const siteTitle = ref('')
const highlightId = ref('')
const incidentsOpen = ref(false)
const incidentsLoading = ref(false)
const incidentRows = ref<IncidentRecord[]>([])

const cards = computed(() => [
  ['ri-server-line', 'totalServers', Number(summary.value.total_servers || 0), 'blue'],
  ['ri-pulse-line', 'onlineServers', servers.value.filter(isHostOnline).length, 'green'],
  ['ri-alarm-warning-line', 'activeIncidents', Number(summary.value.active_incidents || 0), 'amber'],
  ['ri-radar-line', 'activeCollectors', Number(summary.value.active_collectors || 0), 'violet'],
])

const graph = computed<TopologyGraph>(() => buildTopology({
  servers: servers.value,
  collectors: collectors.value,
  paths: paths.value,
  primaryLocation: primaryLocation.value,
  siteTitle: siteTitle.value || t('appName'),
}))

const latencyRows = computed(() => primaryLatencyRows(servers.value, paths.value))

const unlocatedLabel = computed(() => graph.value.unlocated.slice(0, 8).map(item => item.name).join(' · '))

function latencyText(ms?: number) {
  return formatLatencyMs(ms, locale.value)
}

function incidentText(value: unknown, key: string) {
  return formatAdminValue(value, key, locale.value, t as never, te)
}

function observerName(item: ObserverSight) {
  if (item.observer_kind === 'primary') return t('observerKindPrimary')
  return item.observer_name || item.observer_id || ''
}

function missedText(row: IncidentRecord) {
  return missedObserverText(row.observer_evidence, observerName) || '—'
}

async function openIncidents() {
  incidentsOpen.value = true
  incidentsLoading.value = true
  incidentRows.value = []
  try {
    const result = await telemetryList('incidents', { page: 1, page_size: 100, open: 1 })
    incidentRows.value = result.data as IncidentRecord[]
  } catch (error) {
    incidentRows.value = []
    notifyAPIError(error, t as never, te)
  } finally {
    incidentsLoading.value = false
  }
}

function selectMarker(marker: TopologyMarker) {
  highlightId.value = marker.id
  if (marker.href) void router.push(marker.href)
}

function clearHighlight() {
  highlightId.value = ''
}

function selectLatency(name: string) {
  const marker = graph.value.nodes.find(item => item.names.includes(name))
  if (!marker) return
  highlightId.value = highlightId.value === marker.id ? '' : marker.id
}

function isLatencyActive(row: NodeLatencyRow) {
  if (!highlightId.value) return false
  return graph.value.nodes.some(item => item.id === highlightId.value && item.names.includes(row.name))
}

async function load() {
  loading.value = true
  try {
    const [summaryData, serverRows, collectorRows, pathRows, settings] = await Promise.all([
      getAdminSummary(),
      listAllServersPaged(),
      listCollectors(),
      listConnectionPaths(),
      getSettings(),
    ])
    summary.value = summaryData
    servers.value = serverRows
    collectors.value = collectorRows.data
    paths.value = pathRows.data
    primaryLocation.value = String(settings.primary_location || '')
    siteTitle.value = String(settings.site_title || '')
  } catch (error) {
    notifyAPIError(error, t as never, te)
  } finally {
    loading.value = false
  }
}
onMounted(load)
</script>

<template>
  <div class="overview-page">
    <div class="page-head">
      <h1>{{ t('overview') }}</h1>
      <el-button @click="load"><i class="ri-refresh-line"></i>{{ t('refresh') }}</el-button>
    </div>
    <div v-loading="loading" class="overview-body">
      <div class="overview-top">
        <div class="surface metric-strip">
          <component
            :is="card[1] === 'activeIncidents' ? 'button' : 'article'"
            v-for="card in cards"
            :key="String(card[1])"
            class="metric-card"
            :class="{ 'metric-card--button': card[1] === 'activeIncidents' }"
            :type="card[1] === 'activeIncidents' ? 'button' : undefined"
            @click="card[1] === 'activeIncidents' ? openIncidents() : undefined"
          >
            <span class="metric-icon" :class="String(card[3])"><i :class="String(card[0])"></i></span>
            <div><p>{{ t(String(card[1])) }}</p><strong>{{ card[2] }}</strong></div>
          </component>
        </div>
        <div class="surface quick-panel">
          <div class="quick-grid">
            <RouterLink to="/servers?create=1"><i class="ri-add-circle-line"></i><span>{{ t('createServer') }}</span></RouterLink>
            <RouterLink to="/telemetry?create=1"><i class="ri-radar-line"></i><span>{{ t('createCollector') }}</span></RouterLink>
            <RouterLink to="/services?create=1"><i class="ri-heart-pulse-line"></i><span>{{ t('createMonitor') }}</span></RouterLink>
            <RouterLink to="/api-tokens"><i class="ri-key-2-line"></i><span>{{ t('issueToken') }}</span></RouterLink>
          </div>
        </div>
      </div>
      <div class="overview-stage">
        <section class="surface dashboard-panel topology-panel">
          <div class="section-title">
            <div><h2>{{ t('globalLinks') }}</h2></div>
          </div>
          <div class="topology-body">
            <TopologyGlobe :graph="graph" :highlight-id="highlightId" @select="selectMarker" @clear="clearHighlight">
              <template #legend>
                <div class="topology-legend">
                  <div class="topology-legend__group">
                    <span><img class="topology-legend__primary" :src="logoUrl" alt="">{{ t('observerKindPrimary') }}</span>
                    <span>
                      <i class="ri-base-station-fill topology-legend__collector" aria-hidden="true"></i>{{ t('observerKindCollector') }}
                    </span>
                    <span><i class="topology-legend__node"></i>{{ t('nodes') }}</span>
                  </div>
                  <div class="topology-legend__group">
                    <span><i class="status-dot online"></i>{{ t('online') }}</span>
                    <span><i class="status-dot offline"></i>{{ t('offline') }}</span>
                    <span><i class="status-dot degraded"></i>{{ t('mixed') }}</span>
                  </div>
                </div>
              </template>
              <template #note>
                <el-tooltip v-if="graph.unlocated.length" :content="unlocatedLabel" :disabled="!unlocatedLabel" placement="top">
                  <span class="topology-unlocated">{{ t('unlocatedNodes', { n: graph.unlocated.length }) }}</span>
                </el-tooltip>
              </template>
            </TopologyGlobe>
          </div>
        </section>
        <aside class="surface dashboard-panel latency-panel">
          <div class="section-title">
            <div><h2>{{ t('nodeLatency') }}</h2></div>
            <div class="latency-panel__actions">
              <button v-if="highlightId" type="button" class="latency-reset" @click="clearHighlight">
                <i class="ri-close-line"></i>{{ t('reset') }}
              </button>
              <RouterLink to="/connections">{{ t('details') }} <i class="ri-arrow-right-line"></i></RouterLink>
            </div>
          </div>
          <AppEmpty v-if="!latencyRows.length" icon="ri-timer-line" :description="t('noData')" />
          <div v-else class="latency-list">
            <button
              v-for="row in latencyRows"
              :key="row.id"
              type="button"
              class="latency-row"
              :class="{ 'is-offline': !row.online, 'is-active': isLatencyActive(row) }"
              @click="selectLatency(row.name)"
            >
              <span>{{ row.name }}</span>
              <strong>{{ row.online ? latencyText(row.rttMs) : t('offline') }}</strong>
            </button>
          </div>
        </aside>
      </div>
    </div>
    <AppDrawer v-model="incidentsOpen" :title="t('activeIncidents')" mode="view" size="min(760px,96vw)">
      <AppEmpty v-if="!incidentsLoading && !incidentRows.length" icon="ri-alarm-warning-line" :description="t('noActiveIncidents')" />
      <el-table v-else v-loading="incidentsLoading" :data="incidentRows">
        <el-table-column :label="t('host')" min-width="160">
          <template #default="{ row }">{{ row.server_name || '—' }}</template>
        </el-table-column>
        <el-table-column :label="t('currentClassification')" min-width="120">
          <template #default="{ row }">{{ incidentText(row.current_classification, 'current_classification') }}</template>
        </el-table-column>
        <el-table-column :label="t('startedAt')" min-width="180">
          <template #default="{ row }">{{ incidentText(row.started_at, 'started_at') }}</template>
        </el-table-column>
        <el-table-column :label="t('missedObservers')" min-width="160">
          <template #default="{ row }">{{ missedText(row) }}</template>
        </el-table-column>
      </el-table>
    </AppDrawer>
  </div>
</template>
