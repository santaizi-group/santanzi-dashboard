<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import type {
  AgentReliabilityRecord, AgentSink, IncidentRecord, IncidentRevisionRecord,
  ObserverAssignmentRecord, ObserverEvidenceItem, TelemetryAlertRecord, TelemetryDataLossRecord,
} from '@santaizi/api'
import { deleteCollector, getCollectorToken, listCollectors, revokeCollector, rotateCollectorToken, telemetryList, type CollectorRecord } from '@/api/adminApi'
import { AppDialog, AppDrawer, AppEmpty } from '@santaizi/ui'
import { formatAdminValue, formatProductVersion } from '@/composables/format'
import { notifyAPIError } from '@/composables/notify'
import { readStoredPageSize, writeStoredPageSize } from '@/composables/pageSize'
import CollectorEditorDialog from '@/components/editors/CollectorEditorDialog.vue'
import InstallCollectorDialog from '@/components/InstallCollectorDialog.vue'
import CopyableId from '@/components/CopyableId.vue'
import { collectorAccessHost, collectorAccessPort, collectorListenPort } from '@/domain/collectorAddress'
import { isProbeCollector } from '@/domain/collectorKind'

type DatasetKey = 'assignments' | 'agents' | 'incidents' | 'revisions' | 'loss' | 'alerts'
type DatasetRow = ObserverAssignmentRecord | AgentReliabilityRecord | IncidentRecord | IncidentRevisionRecord | TelemetryDataLossRecord | TelemetryAlertRecord

const { t, te, locale } = useI18n()
const route = useRoute()
const active = ref('collectors'), loading = ref(false), editor = ref(false)
const actionBusy = ref('')
const collectors = ref<CollectorRecord[]>([]), records = ref<DatasetRow[]>([])
const total = ref(0)
const query = reactive({ page: 1, page_size: readStoredPageSize(route.path) })
const editing = ref<CollectorRecord>()
const token = ref(''), tokenDialog = ref(false)
const installDialog = ref(false)
const installCollector = ref<CollectorRecord>()
const installToken = ref('')
const drawer = ref(false)
const activeRow = ref<DatasetRow>()
const datasets: Record<DatasetKey, string> = {
  assignments: 'assignments', agents: 'agents', incidents: 'incidents',
  revisions: 'incident-revisions', loss: 'data-loss', alerts: 'alerts',
}

const assignmentRow = computed(() => active.value === 'assignments' ? activeRow.value as ObserverAssignmentRecord | undefined : undefined)
const agentRow = computed(() => active.value === 'agents' ? activeRow.value as AgentReliabilityRecord | undefined : undefined)
const incidentRow = computed(() => active.value === 'incidents' ? activeRow.value as IncidentRecord | undefined : undefined)
const revisionRow = computed(() => active.value === 'revisions' ? activeRow.value as IncidentRevisionRecord | undefined : undefined)
const lossRow = computed(() => active.value === 'loss' ? activeRow.value as TelemetryDataLossRecord | undefined : undefined)
const alertRow = computed(() => active.value === 'alerts' ? activeRow.value as TelemetryAlertRecord | undefined : undefined)

async function load() {
  writeStoredPageSize(route.path, query.page_size)
  loading.value = true
  try {
    if (active.value === 'collectors') collectors.value = (await listCollectors()).data
    else {
      const result = await telemetryList(datasets[active.value as DatasetKey] || active.value, query)
      records.value = result.data as DatasetRow[]
      total.value = result.meta.total || result.data.length
    }
  } catch (e) {
    notifyAPIError(e, t as never, te)
  } finally {
    loading.value = false
  }
}

function onTabChange() {
  query.page = 1
  load()
}

function open(item?: CollectorRecord) {
  editing.value = item
  editor.value = true
}

function showInstall(item: CollectorRecord, registrationToken = '') {
  installCollector.value = item
  installToken.value = registrationToken
  installDialog.value = true
}

async function editorSaved(registrationToken: string, created?: CollectorRecord) {
  await load()
  if (registrationToken && created) showInstall(created, registrationToken)
  else if (registrationToken) {
    token.value = registrationToken
    tokenDialog.value = true
  }
}

async function act(item: CollectorRecord, action: string) {
  if (action === 'install') {
    showInstall(item)
    return
  }
  if (action !== 'view-token') await ElMessageBox.confirm(t('confirmAction'), t('confirm'), { type: action === 'delete' || action === 'revoke' ? 'warning' : 'info' })
  actionBusy.value = item.id
  try {
    if (action === 'delete') await deleteCollector(item.id)
    else if (action === 'revoke') await revokeCollector(item.id)
    else {
      const result = action === 'rotate-token' ? await rotateCollectorToken(item.id) : await getCollectorToken(item.id)
      if (action === 'rotate-token') {
        await load()
        showInstall(item, result.registration_token)
      } else {
        token.value = result.registration_token
        tokenDialog.value = true
      }
    }
    if (action === 'delete' || action === 'revoke') await load()
  } catch (e) {
    notifyAPIError(e, t as never, te)
  } finally {
    actionBusy.value = ''
  }
}

async function copyToken() {
  await navigator.clipboard.writeText(token.value)
  ElMessage.success(t('copied'))
}

function pretty(value: unknown, key = '') {
  return formatAdminValue(value, key, locale.value, t as never, te)
}

function collectorVersionText(row: CollectorRecord) {
  return formatProductVersion(row.software_version) || '—'
}

function collectorKindLabel(row: CollectorRecord) {
  return t(isProbeCollector(row) ? 'collectorKindProbe' : 'collectorKindObserver')
}

function formatEnd(value: unknown, key: string) {
  if (value === null || value === undefined || value === '') return t('ongoing')
  return pretty(value, key)
}

function observerLabel(row: unknown) {
  const item = (row || {}) as { observer_kind?: string; observer_name?: string; observer_id?: string; endpoint_id?: string }
  if (item.observer_kind === 'primary') return t('observerKindPrimary')
  return item.observer_name || item.observer_id || item.endpoint_id || '—'
}

function hostName(row: unknown) {
  const name = (row as { server_name?: unknown } | undefined)?.server_name
  return typeof name === 'string' ? name : ''
}

function seenBy(items?: ObserverEvidenceItem[]) {
  if (!items?.length) return '—'
  const names = items.filter(item => item.seen).map(item => observerLabel(item))
  return names.length ? names.join(', ') : '—'
}

function openRow(row: DatasetRow) {
  activeRow.value = row
  drawer.value = true
}

function drawerTitle() {
  const row = activeRow.value as { server_name?: string; fact_id?: string } | undefined
  if (row?.server_name) return row.server_name
  if (lossRow.value?.fact_id) return t('dataLoss')
  if (revisionRow.value) return t('incidentRevisions')
  return t('details')
}

onMounted(async () => {
  await load()
  if (route.query.create === '1') open()
})
</script>

<template>
  <div class="page-head">
    <div><h1>{{ t('telemetry') }}</h1></div>
    <el-button v-if="active==='collectors'" type="primary" @click="open()"><i class="ri-add-line"></i>{{ t('createCollector') }}</el-button>
  </div>
  <section class="surface telemetry-shell">
    <el-tabs v-model="active" @tab-change="onTabChange">
      <el-tab-pane :label="t('collectors')" name="collectors" />
      <el-tab-pane :label="t('observerAssignments')" name="assignments" />
      <el-tab-pane :label="t('agentDelivery')" name="agents" />
      <el-tab-pane :label="t('incidents')" name="incidents" />
      <el-tab-pane :label="t('incidentRevisions')" name="revisions" />
      <el-tab-pane :label="t('dataLoss')" name="loss" />
      <el-tab-pane :label="t('alerts')" name="alerts" />
    </el-tabs>
    <template v-if="active==='collectors'">
      <el-table class="desktop-only" v-loading="loading" :data="collectors" row-key="id">
        <el-table-column class-name="col-status" label-class-name="col-status" width="44" align="center">
          <template #default="{row}">
            <span class="status-dot" :class="row.revoked?'offline':row.status||'unknown'"></span>
          </template>
        </el-table-column>
        <el-table-column prop="name" :label="t('name')" min-width="180">
          <template #default="{row}">
            <div class="server-name">
              <strong>{{ row.name }}</strong>
              <CopyableId :value="row.id" />
            </div>
          </template>
        </el-table-column>
        <el-table-column :label="t('collectorKind')" min-width="120">
          <template #default="{row}"><el-tag effect="plain">{{ collectorKindLabel(row) }}</el-tag></template>
        </el-table-column>
        <el-table-column :label="t('address')" min-width="180" show-overflow-tooltip>
          <template #default="{row}">{{ collectorAccessHost(row) || '—' }}</template>
        </el-table-column>
        <el-table-column :label="t('accessPort')" width="110">
          <template #default="{row}">{{ collectorAccessPort(row) ?? '—' }}</template>
        </el-table-column>
        <el-table-column :label="t('listenPort')" width="110">
          <template #default="{row}">{{ collectorListenPort(row) ?? '—' }}</template>
        </el-table-column>
        <el-table-column prop="generation" :label="t('generation')" width="100" />
        <el-table-column prop="config_version" :label="t('configVersion')" width="120" />
        <el-table-column :label="t('collectorVersion')" width="120">
          <template #default="{row}">{{ collectorVersionText(row) }}</template>
        </el-table-column>
        <el-table-column prop="last_seen" :label="t('lastSeen')" width="190">
          <template #default="{row}">{{ pretty(row.last_seen, 'last_seen') }}</template>
        </el-table-column>
        <el-table-column :label="t('connectedAgents')" width="120">
          <template #default="{row}">{{ isProbeCollector(row) ? '—' : pretty(row.connected_agents, 'connected_agents') }}</template>
        </el-table-column>
        <el-table-column :label="t('pendingRecords')" width="120">
          <template #default="{row}">{{ isProbeCollector(row) ? '—' : pretty(row.pending_records, 'pending_records') }}</template>
        </el-table-column>
        <el-table-column :label="t('spoolSize')" width="120">
          <template #default="{row}">{{ isProbeCollector(row) ? '—' : pretty(row.spool_size, 'spool_size') }}</template>
        </el-table-column>
        <el-table-column :label="t('actions')" width="72" fixed="right">
          <template #default="{row}">
            <el-dropdown trigger="click" :disabled="actionBusy===row.id">
              <el-button text class="actions-more" :loading="actionBusy===row.id" :aria-label="t('actions')"><i class="ri-more-fill"></i></el-button>
              <template #dropdown>
                <el-dropdown-menu>
                  <el-dropdown-item :disabled="actionBusy===row.id" @click="open(row)"><i class="ri-edit-line"></i>{{ t('edit') }}</el-dropdown-item>
                  <el-dropdown-item @click="act(row,'install')"><i class="ri-download-cloud-2-line"></i>{{ t('installCollector') }}</el-dropdown-item>
                  <el-dropdown-item @click="act(row,'view-token')"><i class="ri-key-2-line"></i>{{ t('viewToken') }}</el-dropdown-item>
                  <el-dropdown-item @click="act(row,'rotate-token')"><i class="ri-loop-left-line"></i>{{ t('rotateToken') }}</el-dropdown-item>
                  <el-dropdown-item @click="act(row,'revoke')"><i class="ri-forbid-line"></i>{{ t('revoke') }}</el-dropdown-item>
                  <el-dropdown-item divided @click="act(row,'delete')"><i class="ri-delete-bin-line"></i>{{ t('delete') }}</el-dropdown-item>
                </el-dropdown-menu>
              </template>
            </el-dropdown>
          </template>
        </el-table-column>
        <template #empty><AppEmpty class="empty-state" icon="ri-radar-line" :description="t('noData')" /></template>
      </el-table>
      <div class="mobile-only" v-loading="loading">
        <AppEmpty v-if="!collectors.length && !loading" class="empty-state" icon="ri-radar-line" :description="t('noData')" />
        <div v-else class="mobile-card-list">
          <article v-for="row in collectors" :key="row.id" class="mobile-card">
            <div class="mobile-card-head">
              <span class="mobile-card-status"><span class="status-dot" :class="row.revoked?'offline':row.status||'unknown'"></span></span>
              <div class="mobile-card-title">
                <strong>{{ row.name }}</strong>
                <CopyableId :value="row.id" />
              </div>
              <div class="mobile-card-actions">
                <el-dropdown trigger="click" :disabled="actionBusy===row.id">
                  <el-button text class="actions-more" :loading="actionBusy===row.id" :aria-label="t('actions')"><i class="ri-more-fill"></i></el-button>
                  <template #dropdown>
                    <el-dropdown-menu>
                      <el-dropdown-item :disabled="actionBusy===row.id" @click="open(row)"><i class="ri-edit-line"></i>{{ t('edit') }}</el-dropdown-item>
                      <el-dropdown-item @click="act(row,'install')"><i class="ri-download-cloud-2-line"></i>{{ t('installCollector') }}</el-dropdown-item>
                      <el-dropdown-item @click="act(row,'view-token')"><i class="ri-key-2-line"></i>{{ t('viewToken') }}</el-dropdown-item>
                      <el-dropdown-item @click="act(row,'rotate-token')"><i class="ri-loop-left-line"></i>{{ t('rotateToken') }}</el-dropdown-item>
                      <el-dropdown-item @click="act(row,'revoke')"><i class="ri-forbid-line"></i>{{ t('revoke') }}</el-dropdown-item>
                      <el-dropdown-item divided @click="act(row,'delete')"><i class="ri-delete-bin-line"></i>{{ t('delete') }}</el-dropdown-item>
                    </el-dropdown-menu>
                  </template>
                </el-dropdown>
              </div>
            </div>
            <dl class="mobile-card-meta">
              <div><dt>{{ t('collectorKind') }}</dt><dd><el-tag effect="plain">{{ collectorKindLabel(row) }}</el-tag></dd></div>
              <div><dt>{{ t('address') }}</dt><dd>{{ collectorAccessHost(row) || '—' }}</dd></div>
              <div><dt>{{ t('accessPort') }}</dt><dd>{{ collectorAccessPort(row) ?? '—' }}</dd></div>
              <div><dt>{{ t('listenPort') }}</dt><dd>{{ collectorListenPort(row) ?? '—' }}</dd></div>
              <div><dt>{{ t('generation') }}</dt><dd>{{ row.generation }}</dd></div>
              <div><dt>{{ t('configVersion') }}</dt><dd>{{ row.config_version }}</dd></div>
              <div><dt>{{ t('collectorVersion') }}</dt><dd>{{ collectorVersionText(row) }}</dd></div>
              <div><dt>{{ t('lastSeen') }}</dt><dd>{{ pretty(row.last_seen, 'last_seen') }}</dd></div>
              <div><dt>{{ t('connectedAgents') }}</dt><dd>{{ isProbeCollector(row) ? '—' : pretty(row.connected_agents, 'connected_agents') }}</dd></div>
              <div><dt>{{ t('pendingRecords') }}</dt><dd>{{ isProbeCollector(row) ? '—' : pretty(row.pending_records, 'pending_records') }}</dd></div>
              <div><dt>{{ t('spoolSize') }}</dt><dd>{{ isProbeCollector(row) ? '—' : pretty(row.spool_size, 'spool_size') }}</dd></div>
            </dl>
          </article>
        </div>
      </div>
    </template>
    <template v-else>
      <el-table class="desktop-only dataset-table" v-loading="loading" :data="records" @row-click="openRow">
        <template v-if="active==='assignments'">
          <el-table-column :label="t('host')" min-width="160">
            <template #default="{row}">
              <span v-if="hostName(row)" class="cell-ellipsis">{{ hostName(row) }}</span>
              <CopyableId v-else :value="(row as ObserverAssignmentRecord).node_uuid" />
            </template>
          </el-table-column>
          <el-table-column :label="t('observer')" min-width="140">
            <template #default="{row}"><span class="cell-ellipsis">{{ observerLabel(row) }}</span></template>
          </el-table-column>
          <el-table-column :label="t('validFrom')" min-width="180">
            <template #default="{row}">{{ pretty(row.valid_from, 'valid_from') }}</template>
          </el-table-column>
          <el-table-column :label="t('validTo')" min-width="180">
            <template #default="{row}">{{ formatEnd(row.valid_to, 'valid_to') }}</template>
          </el-table-column>
          <el-table-column :label="t('generation')" width="100">
            <template #default="{row}">{{ pretty(row.generation, 'generation') }}</template>
          </el-table-column>
          <el-table-column :label="t('configVersion')" width="120">
            <template #default="{row}">{{ pretty(row.config_version, 'config_version') }}</template>
          </el-table-column>
        </template>
        <template v-else-if="active==='agents'">
          <el-table-column :label="t('host')" min-width="160">
            <template #default="{row}">
              <span v-if="hostName(row)" class="cell-ellipsis">{{ hostName(row) }}</span>
              <CopyableId v-else :value="(row as AgentReliabilityRecord).node_uuid" />
            </template>
          </el-table-column>
          <el-table-column :label="t('walPressure')" min-width="120">
            <template #default="{row}"><span class="cell-ellipsis">{{ pretty(row.wal_pressure, 'wal_pressure') }}</span></template>
          </el-table-column>
          <el-table-column :label="t('walBytes')" width="120">
            <template #default="{row}">{{ pretty(row.wal_bytes, 'wal_bytes') }}</template>
          </el-table-column>
          <el-table-column :label="t('pendingEvents')" width="120">
            <template #default="{row}">{{ pretty(row.pending_events, 'pending_events') }}</template>
          </el-table-column>
          <el-table-column :label="t('oldestPending')" min-width="180">
            <template #default="{row}">{{ pretty(row.oldest_pending, 'oldest_pending') }}</template>
          </el-table-column>
          <el-table-column :label="t('clock')" min-width="120">
            <template #default="{row}">{{ t(row.clock_untrusted ? 'clockUntrusted' : 'clockTrusted') }}</template>
          </el-table-column>
          <el-table-column :label="t('protocolVersion')" width="120">
            <template #default="{row}"><span class="cell-ellipsis">{{ pretty(row.protocol_version, 'protocol_version') }}</span></template>
          </el-table-column>
          <el-table-column :label="t('modifiedAt')" min-width="180">
            <template #default="{row}">{{ pretty(row.updated_at, 'updated_at') }}</template>
          </el-table-column>
        </template>
        <template v-else-if="active==='incidents'">
          <el-table-column :label="t('startedAt')" min-width="180">
            <template #default="{row}">{{ pretty(row.started_at, 'started_at') }}</template>
          </el-table-column>
          <el-table-column :label="t('endedAt')" min-width="180">
            <template #default="{row}">{{ formatEnd(row.ended_at, 'ended_at') }}</template>
          </el-table-column>
          <el-table-column :label="t('currentClassification')" min-width="140">
            <template #default="{row}"><span class="cell-ellipsis">{{ pretty(row.current_classification, 'current_classification') }}</span></template>
          </el-table-column>
          <el-table-column :label="t('host')" min-width="160">
            <template #default="{row}">
              <span v-if="hostName(row)" class="cell-ellipsis">{{ hostName(row) }}</span>
              <CopyableId v-else :value="(row as IncidentRecord).node_uuid" />
            </template>
          </el-table-column>
          <el-table-column :label="t('seenBy')" min-width="140">
            <template #default="{row}"><span class="cell-ellipsis">{{ seenBy((row as IncidentRecord).observer_evidence) }}</span></template>
          </el-table-column>
          <el-table-column :label="t('revision')" width="100">
            <template #default="{row}">{{ pretty(row.revision, 'revision') }}</template>
          </el-table-column>
        </template>
        <template v-else-if="active==='revisions'">
          <el-table-column :label="t('createdAt')" min-width="180">
            <template #default="{row}">{{ pretty(row.created_at, 'created_at') }}</template>
          </el-table-column>
          <el-table-column :label="t('incidentID')" width="120">
            <template #default="{row}">{{ pretty(row.incident_id, 'incident_id') }}</template>
          </el-table-column>
          <el-table-column :label="t('currentClassification')" min-width="140">
            <template #default="{row}"><span class="cell-ellipsis">{{ pretty(row.classification, 'classification') }}</span></template>
          </el-table-column>
          <el-table-column :label="t('reason')" min-width="160">
            <template #default="{row}"><span class="cell-ellipsis">{{ pretty(row.reason, 'reason') }}</span></template>
          </el-table-column>
          <el-table-column :label="t('modifiedAt')" min-width="180">
            <template #default="{row}">{{ pretty(row.recalculated_at, 'recalculated_at') }}</template>
          </el-table-column>
        </template>
        <template v-else-if="active==='loss'">
          <el-table-column :label="t('occurredAt')" min-width="180">
            <template #default="{row}">{{ pretty(row.occurred_at, 'occurred_at') }}</template>
          </el-table-column>
          <el-table-column :label="t('component')" min-width="140">
            <template #default="{row}">
              <CopyableId v-if="String(row.component_id || '').length >= 16" :value="row.component_id" />
              <span v-else class="cell-ellipsis">{{ pretty(row.component_id, 'component_id') }}</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('reason')" min-width="160">
            <template #default="{row}"><span class="cell-ellipsis">{{ pretty(row.reason, 'reason') }}</span></template>
          </el-table-column>
          <el-table-column :label="t('lostRecords')" width="120">
            <template #default="{row}">{{ pretty(row.lost_records, 'lost_records') }}</template>
          </el-table-column>
          <el-table-column :label="t('details')" min-width="160">
            <template #default="{row}"><span class="cell-ellipsis">{{ pretty(row.detail, 'detail') }}</span></template>
          </el-table-column>
        </template>
        <template v-else>
          <el-table-column :label="t('occurredAt')" min-width="180">
            <template #default="{row}">{{ pretty(row.occurred_at, 'occurred_at') }}</template>
          </el-table-column>
          <el-table-column :label="t('type')" min-width="140">
            <template #default="{row}"><span class="cell-ellipsis">{{ pretty(row.alert_type, 'alert_type') }}</span></template>
          </el-table-column>
          <el-table-column :label="t('severity')" width="110">
            <template #default="{row}"><span class="cell-ellipsis">{{ pretty(row.severity, 'severity') }}</span></template>
          </el-table-column>
          <el-table-column :label="t('host')" min-width="160">
            <template #default="{row}">
              <span v-if="hostName(row)" class="cell-ellipsis">{{ hostName(row) }}</span>
              <CopyableId v-else-if="(row as TelemetryAlertRecord).node_uuid" :value="(row as TelemetryAlertRecord).node_uuid" />
              <span v-else>—</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('notified')" width="100">
            <template #default="{row}">{{ pretty(row.notified, 'notified') }}</template>
          </el-table-column>
        </template>
        <template #empty><AppEmpty class="empty-state" icon="ri-radar-line" :description="t('noData')" /></template>
      </el-table>
      <div class="mobile-only" v-loading="loading">
        <AppEmpty v-if="!records.length && !loading" class="empty-state" icon="ri-radar-line" :description="t('noData')" />
        <div v-else class="mobile-card-list">
          <article v-for="(row, index) in records" :key="index" class="mobile-card" @click="openRow(row)">
            <dl v-if="active==='assignments'" class="mobile-card-meta">
              <div><dt>{{ t('host') }}</dt><dd><span v-if="hostName(row)" class="cell-ellipsis">{{ hostName(row) }}</span><CopyableId v-else :value="(row as ObserverAssignmentRecord).node_uuid" /></dd></div>
              <div><dt>{{ t('observer') }}</dt><dd class="cell-ellipsis">{{ observerLabel(row) }}</dd></div>
              <div><dt>{{ t('validFrom') }}</dt><dd>{{ pretty((row as ObserverAssignmentRecord).valid_from, 'valid_from') }}</dd></div>
              <div><dt>{{ t('validTo') }}</dt><dd>{{ formatEnd((row as ObserverAssignmentRecord).valid_to, 'valid_to') }}</dd></div>
              <div><dt>{{ t('generation') }}</dt><dd>{{ pretty((row as ObserverAssignmentRecord).generation, 'generation') }}</dd></div>
              <div><dt>{{ t('configVersion') }}</dt><dd>{{ pretty((row as ObserverAssignmentRecord).config_version, 'config_version') }}</dd></div>
            </dl>
            <dl v-else-if="active==='agents'" class="mobile-card-meta">
              <div><dt>{{ t('host') }}</dt><dd><span v-if="hostName(row)" class="cell-ellipsis">{{ hostName(row) }}</span><CopyableId v-else :value="(row as AgentReliabilityRecord).node_uuid" /></dd></div>
              <div><dt>{{ t('walPressure') }}</dt><dd class="cell-ellipsis">{{ pretty((row as AgentReliabilityRecord).wal_pressure, 'wal_pressure') }}</dd></div>
              <div><dt>{{ t('walBytes') }}</dt><dd>{{ pretty((row as AgentReliabilityRecord).wal_bytes, 'wal_bytes') }}</dd></div>
              <div><dt>{{ t('pendingEvents') }}</dt><dd>{{ pretty((row as AgentReliabilityRecord).pending_events, 'pending_events') }}</dd></div>
              <div><dt>{{ t('oldestPending') }}</dt><dd>{{ pretty((row as AgentReliabilityRecord).oldest_pending, 'oldest_pending') }}</dd></div>
              <div><dt>{{ t('clock') }}</dt><dd>{{ t((row as AgentReliabilityRecord).clock_untrusted ? 'clockUntrusted' : 'clockTrusted') }}</dd></div>
              <div><dt>{{ t('protocolVersion') }}</dt><dd class="cell-ellipsis">{{ pretty((row as AgentReliabilityRecord).protocol_version, 'protocol_version') }}</dd></div>
              <div><dt>{{ t('modifiedAt') }}</dt><dd>{{ pretty((row as AgentReliabilityRecord).updated_at, 'updated_at') }}</dd></div>
            </dl>
            <dl v-else-if="active==='incidents'" class="mobile-card-meta">
              <div><dt>{{ t('startedAt') }}</dt><dd>{{ pretty((row as IncidentRecord).started_at, 'started_at') }}</dd></div>
              <div><dt>{{ t('endedAt') }}</dt><dd>{{ formatEnd((row as IncidentRecord).ended_at, 'ended_at') }}</dd></div>
              <div><dt>{{ t('currentClassification') }}</dt><dd class="cell-ellipsis">{{ pretty((row as IncidentRecord).current_classification, 'current_classification') }}</dd></div>
              <div><dt>{{ t('host') }}</dt><dd><span v-if="hostName(row)" class="cell-ellipsis">{{ hostName(row) }}</span><CopyableId v-else :value="(row as IncidentRecord).node_uuid" /></dd></div>
              <div><dt>{{ t('seenBy') }}</dt><dd class="cell-ellipsis">{{ seenBy((row as IncidentRecord).observer_evidence) }}</dd></div>
              <div><dt>{{ t('revision') }}</dt><dd>{{ pretty((row as IncidentRecord).revision, 'revision') }}</dd></div>
            </dl>
            <dl v-else-if="active==='revisions'" class="mobile-card-meta">
              <div><dt>{{ t('createdAt') }}</dt><dd>{{ pretty((row as IncidentRevisionRecord).created_at, 'created_at') }}</dd></div>
              <div><dt>{{ t('incidentID') }}</dt><dd>{{ pretty((row as IncidentRevisionRecord).incident_id, 'incident_id') }}</dd></div>
              <div><dt>{{ t('currentClassification') }}</dt><dd class="cell-ellipsis">{{ pretty((row as IncidentRevisionRecord).classification, 'classification') }}</dd></div>
              <div><dt>{{ t('reason') }}</dt><dd class="cell-ellipsis">{{ pretty((row as IncidentRevisionRecord).reason, 'reason') }}</dd></div>
              <div><dt>{{ t('modifiedAt') }}</dt><dd>{{ pretty((row as IncidentRevisionRecord).recalculated_at, 'recalculated_at') }}</dd></div>
            </dl>
            <dl v-else-if="active==='loss'" class="mobile-card-meta">
              <div><dt>{{ t('occurredAt') }}</dt><dd>{{ pretty((row as TelemetryDataLossRecord).occurred_at, 'occurred_at') }}</dd></div>
              <div><dt>{{ t('component') }}</dt><dd class="cell-ellipsis">{{ pretty((row as TelemetryDataLossRecord).component_id, 'component_id') }}</dd></div>
              <div><dt>{{ t('reason') }}</dt><dd class="cell-ellipsis">{{ pretty((row as TelemetryDataLossRecord).reason, 'reason') }}</dd></div>
              <div><dt>{{ t('lostRecords') }}</dt><dd>{{ pretty((row as TelemetryDataLossRecord).lost_records, 'lost_records') }}</dd></div>
              <div><dt>{{ t('details') }}</dt><dd class="cell-ellipsis">{{ pretty((row as TelemetryDataLossRecord).detail, 'detail') }}</dd></div>
            </dl>
            <dl v-else class="mobile-card-meta">
              <div><dt>{{ t('occurredAt') }}</dt><dd>{{ pretty((row as TelemetryAlertRecord).occurred_at, 'occurred_at') }}</dd></div>
              <div><dt>{{ t('type') }}</dt><dd class="cell-ellipsis">{{ pretty((row as TelemetryAlertRecord).alert_type, 'alert_type') }}</dd></div>
              <div><dt>{{ t('severity') }}</dt><dd class="cell-ellipsis">{{ pretty((row as TelemetryAlertRecord).severity, 'severity') }}</dd></div>
              <div><dt>{{ t('host') }}</dt><dd><span v-if="hostName(row)" class="cell-ellipsis">{{ hostName(row) }}</span><CopyableId v-else-if="(row as TelemetryAlertRecord).node_uuid" :value="(row as TelemetryAlertRecord).node_uuid" /><span v-else>—</span></dd></div>
              <div><dt>{{ t('notified') }}</dt><dd>{{ pretty((row as TelemetryAlertRecord).notified, 'notified') }}</dd></div>
            </dl>
          </article>
        </div>
      </div>
      <div class="pagination"><el-pagination v-model:current-page="query.page" v-model:page-size="query.page_size" layout="total, sizes, prev, pager, next" :total="total" @change="load"/></div>
    </template>
  </section>
  <CollectorEditorDialog v-model="editor" :value="editing" @saved="editorSaved" />
  <InstallCollectorDialog v-model="installDialog" :collector="installCollector" :token="installToken" />
  <AppDialog v-model="tokenDialog" :title="t('registrationToken')" mode="view" width="min(560px,92vw)">
    <el-input v-model="token" class="token-display mono" readonly>
      <template #append><el-button @click="copyToken"><i class="ri-file-copy-line"></i></el-button></template>
    </el-input>
  </AppDialog>
  <AppDrawer v-model="drawer" :title="drawerTitle()" mode="view">
    <dl v-if="assignmentRow" class="mobile-card-meta">
      <div><dt>{{ t('host') }}</dt><dd>{{ assignmentRow.server_name || '—' }}</dd></div>
      <div><dt>{{ t('observer') }}</dt><dd>{{ observerLabel(assignmentRow) }}</dd></div>
      <div><dt>{{ t('validFrom') }}</dt><dd>{{ pretty(assignmentRow.valid_from, 'valid_from') }}</dd></div>
      <div><dt>{{ t('validTo') }}</dt><dd>{{ formatEnd(assignmentRow.valid_to, 'valid_to') }}</dd></div>
      <div><dt>{{ t('generation') }}</dt><dd>{{ pretty(assignmentRow.generation, 'generation') }}</dd></div>
      <div><dt>{{ t('configVersion') }}</dt><dd>{{ pretty(assignmentRow.config_version, 'config_version') }}</dd></div>
      <div><dt>{{ t('nodeUUID') }}</dt><dd><CopyableId :value="assignmentRow.node_uuid" :compact="false" /></dd></div>
    </dl>
    <template v-else-if="agentRow">
      <dl class="mobile-card-meta">
        <div><dt>{{ t('host') }}</dt><dd>{{ agentRow.server_name || '—' }}</dd></div>
        <div><dt>{{ t('walPressure') }}</dt><dd>{{ pretty(agentRow.wal_pressure, 'wal_pressure') }}</dd></div>
        <div><dt>{{ t('walBytes') }}</dt><dd>{{ pretty(agentRow.wal_bytes, 'wal_bytes') }}</dd></div>
        <div><dt>{{ t('pendingEvents') }}</dt><dd>{{ pretty(agentRow.pending_events, 'pending_events') }}</dd></div>
        <div><dt>{{ t('oldestPending') }}</dt><dd>{{ pretty(agentRow.oldest_pending, 'oldest_pending') }}</dd></div>
        <div><dt>{{ t('clock') }}</dt><dd>{{ t(agentRow.clock_untrusted ? 'clockUntrusted' : 'clockTrusted') }}</dd></div>
        <div><dt>{{ t('protocolVersion') }}</dt><dd>{{ pretty(agentRow.protocol_version, 'protocol_version') }}</dd></div>
        <div><dt>{{ t('modifiedAt') }}</dt><dd>{{ pretty(agentRow.updated_at, 'updated_at') }}</dd></div>
        <div><dt>{{ t('nodeUUID') }}</dt><dd><CopyableId :value="agentRow.node_uuid" :compact="false" /></dd></div>
      </dl>
      <el-table class="drawer-sinks" :data="agentRow.sinks" row-key="endpoint_id">
        <el-table-column :label="t('observer')" min-width="140">
          <template #default="{row}">{{ observerLabel(row as AgentSink) }}</template>
        </el-table-column>
        <el-table-column :label="t('linkStatus')" width="110">
          <template #default="{row}">{{ t((row as AgentSink).connected ? 'connected' : 'disconnected') }}</template>
        </el-table-column>
        <el-table-column :label="t('pendingEvents')" width="110">
          <template #default="{row}">{{ pretty((row as AgentSink).pending_events, 'pending_events') }}</template>
        </el-table-column>
        <el-table-column :label="t('lastError')" min-width="140">
          <template #default="{row}">{{ pretty((row as AgentSink).last_error, 'last_error') }}</template>
        </el-table-column>
      </el-table>
    </template>
    <template v-else-if="incidentRow">
      <dl class="mobile-card-meta">
        <div><dt>{{ t('host') }}</dt><dd>{{ incidentRow.server_name || '—' }}</dd></div>
        <div><dt>{{ t('startedAt') }}</dt><dd>{{ pretty(incidentRow.started_at, 'started_at') }}</dd></div>
        <div><dt>{{ t('endedAt') }}</dt><dd>{{ formatEnd(incidentRow.ended_at, 'ended_at') }}</dd></div>
        <div><dt>{{ t('initialClassification') }}</dt><dd>{{ pretty(incidentRow.initial_classification, 'initial_classification') }}</dd></div>
        <div><dt>{{ t('currentClassification') }}</dt><dd>{{ pretty(incidentRow.current_classification, 'current_classification') }}</dd></div>
        <div><dt>{{ t('reason') }}</dt><dd>{{ pretty(incidentRow.reason, 'reason') }}</dd></div>
        <div><dt>{{ t('revision') }}</dt><dd>{{ pretty(incidentRow.revision, 'revision') }}</dd></div>
        <div><dt>{{ t('nodeUUID') }}</dt><dd><CopyableId :value="incidentRow.node_uuid" :compact="false" /></dd></div>
      </dl>
      <dl class="mobile-card-meta">
        <div v-for="item in incidentRow.observer_evidence" :key="item.observer_id">
          <dt>{{ observerLabel(item) }}</dt>
          <dd>{{ t(item.seen ? 'yes' : 'no') }} / {{ t(item.healthy ? 'healthy' : 'degraded') }}</dd>
        </div>
      </dl>
    </template>
    <template v-else-if="revisionRow">
      <dl class="mobile-card-meta">
        <div><dt>{{ t('incidentID') }}</dt><dd>{{ pretty(revisionRow.incident_id, 'incident_id') }}</dd></div>
        <div><dt>{{ t('revision') }}</dt><dd>{{ pretty(revisionRow.revision, 'revision') }}</dd></div>
        <div><dt>{{ t('currentClassification') }}</dt><dd>{{ pretty(revisionRow.classification, 'classification') }}</dd></div>
        <div><dt>{{ t('reason') }}</dt><dd>{{ pretty(revisionRow.reason, 'reason') }}</dd></div>
        <div><dt>{{ t('createdAt') }}</dt><dd>{{ pretty(revisionRow.created_at, 'created_at') }}</dd></div>
        <div><dt>{{ t('modifiedAt') }}</dt><dd>{{ pretty(revisionRow.recalculated_at, 'recalculated_at') }}</dd></div>
      </dl>
      <dl class="mobile-card-meta">
        <div v-for="item in revisionRow.observer_evidence" :key="item.observer_id">
          <dt>{{ observerLabel(item) }}</dt>
          <dd>{{ t(item.seen ? 'yes' : 'no') }} / {{ t(item.healthy ? 'healthy' : 'degraded') }}</dd>
        </div>
      </dl>
    </template>
    <dl v-else-if="lossRow" class="mobile-card-meta">
      <div><dt>{{ t('occurredAt') }}</dt><dd>{{ pretty(lossRow.occurred_at, 'occurred_at') }}</dd></div>
      <div><dt>{{ t('component') }}</dt><dd>{{ pretty(lossRow.component_id, 'component_id') }}</dd></div>
      <div><dt>{{ t('reason') }}</dt><dd>{{ pretty(lossRow.reason, 'reason') }}</dd></div>
      <div><dt>{{ t('lostRecords') }}</dt><dd>{{ pretty(lossRow.lost_records, 'lost_records') }}</dd></div>
      <div><dt>{{ t('details') }}</dt><dd>{{ pretty(lossRow.detail, 'detail') }}</dd></div>
      <div><dt>{{ t('factID') }}</dt><dd><CopyableId :value="lossRow.fact_id" :compact="false" /></dd></div>
    </dl>
    <dl v-else-if="alertRow" class="mobile-card-meta">
      <div><dt>{{ t('occurredAt') }}</dt><dd>{{ pretty(alertRow.occurred_at, 'occurred_at') }}</dd></div>
      <div><dt>{{ t('type') }}</dt><dd>{{ pretty(alertRow.alert_type, 'alert_type') }}</dd></div>
      <div><dt>{{ t('severity') }}</dt><dd>{{ pretty(alertRow.severity, 'severity') }}</dd></div>
      <div><dt>{{ t('host') }}</dt><dd>{{ alertRow.server_name || '—' }}</dd></div>
      <div><dt>{{ t('notified') }}</dt><dd>{{ pretty(alertRow.notified, 'notified') }}</dd></div>
      <div><dt>{{ t('message') }}</dt><dd>{{ pretty(alertRow.message, 'message') }}</dd></div>
      <div><dt>{{ t('nodeUUID') }}</dt><dd><CopyableId :value="alertRow.node_uuid" :compact="false" /></dd></div>
      <div><dt>{{ t('dedupKey') }}</dt><dd class="copyable-id__full mono">{{ alertRow.dedup_key || '—' }}</dd></div>
    </dl>
  </AppDrawer>
</template>
