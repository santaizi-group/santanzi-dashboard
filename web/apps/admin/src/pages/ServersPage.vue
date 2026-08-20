<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { AppDialog, AppDrawer, AppEmpty } from '@santaizi/ui'
import type { ConnectionPath, ProbePath, ServerBackup, ServerImportPreviewItem, TrafficPolicyHistory, TrafficSummary } from '@santaizi/api'
import ServerEditorDialog from '@/components/editors/ServerEditorDialog.vue'
import ServerGroupManagerDialog from '@/components/editors/ServerGroupManagerDialog.vue'
import ServerImportDialog from '@/components/editors/ServerImportDialog.vue'
import InstallAgentDialog from '@/components/InstallAgentDialog.vue'
import UpgradeAgentDialog from '@/components/UpgradeAgentDialog.vue'
import ProbePathDialog from '@/components/ProbePathDialog.vue'
import TrafficHistoryPanel from '@/components/TrafficHistoryPanel.vue'
import CopyableText from '@/components/CopyableText.vue'
import { batchDeleteServers, batchUpdateServerGroup, deleteOfflineHistory, deleteServer, exportServers, getServerInstallPreview, getServerTrafficHistory, listConnectionPaths, listOfflineHistory, listProbePaths, listServerAvailability, listServers, previewServerImport, resetServerAvailability, resetServerSecret, updateServerDisplayIndex, type ResourceRecord, type ServerRecord } from '@/api/adminApi'
import { formatAdminValue, formatBytes } from '@/composables/format'
import { notifyAPIError } from '@/composables/notify'
import { readStoredPageSize, writeStoredPageSize } from '@/composables/pageSize'
import { isRowSelected, toggleRowSelection } from '@/composables/selection'
import { shortId } from '@/composables/shortId'
import {
  buildAvailabilitySegments,
  coverageLabel,
  formatDurationMs,
  summarizeAvailability,
  type AvailabilitySegment,
  type ObserverEvidence,
} from '@/domain/availability'
import { hostAddresses } from '@/domain/hostAddress'
import { defaultInstallPreviewBody } from '@/domain/installAgent'
import { parsePublicNote } from '@/domain/publicNote'
import { probeHasNoTarget, probePathKey, probeTargetText } from '@/domain/probePath'
import { hostCoverageIcon, hostCoverageTone, hostListTone } from '@/domain/serverPresence'

const { t, te, locale } = useI18n()
const route = useRoute()
const loading = ref(false), editor = ref(false), installDialog = ref(false), upgradeDialog = ref(false), groupManager = ref(false), importer = ref(false)
const items = ref<ServerRecord[]>([]), selected = ref<ServerRecord[]>([]), editing = ref<ServerRecord>(), installServer = ref<ServerRecord>(), installSecret = ref(''), upgradeServer = ref<ServerRecord>()
const importFile = ref<HTMLInputElement>(), importDocument = ref<ServerBackup>(), importItems = ref<ServerImportPreviewItem[]>([]), exporting = ref(false)
const total = ref(0)
const query = reactive({ page: 1, page_size: readStoredPageSize(route.path), q: '', sort: 'display_index', order: 'desc' as const })
const historyDrawer = ref(false), historyLoading = ref(false), historyServer = ref<ServerRecord>(), historyTab = ref('availability'), history = ref<ResourceRecord[]>([]), availability = ref<ResourceRecord[]>([]), connectionPaths = ref<ConnectionPath[]>([]), probePaths = ref<ProbePath[]>([]), trafficHistories = ref<TrafficPolicyHistory[]>([])
const probeDialog = ref(false)
const activeProbe = ref<ProbePath>()
const availabilityHours = ref(6)
const availabilityRangeOptions = [1, 6, 24]
const sortDraft = ref<Record<number, string>>({})
const sortSaving = ref<Record<number, boolean>>({})
const hoverCapable = ref(false)
const noteDialog = ref(false)
const noteServer = ref<ServerRecord>()
const copyingInstall = ref<number>()
let hoverMedia: MediaQueryList | undefined

function onHoverMediaChange() {
  hoverCapable.value = !!hoverMedia?.matches
}

async function exportAll() {
  exporting.value = true
  try {
    const payload = await exportServers()
    const now = new Date()
    const stamp = `${now.getFullYear()}${String(now.getMonth() + 1).padStart(2, '0')}${String(now.getDate()).padStart(2, '0')}`
    const blob = new Blob([`${JSON.stringify(payload, null, 2)}\n`], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = `santaizi-servers-${stamp}.json`
    link.click()
    URL.revokeObjectURL(url)
  } catch (error) {
    notifyAPIError(error, t as never, te)
  } finally {
    exporting.value = false
  }
}

function pickImport() {
  importFile.value?.click()
}

async function onImportFile(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file) return
  let document: ServerBackup
  try {
    document = JSON.parse(await file.text()) as ServerBackup
  } catch {
    ElMessage.warning(t('invalidJSON'))
    return
  }
  try {
    const preview = await previewServerImport(document)
    importDocument.value = document
    importItems.value = preview.items
    importer.value = true
  } catch (error) {
    notifyAPIError(error, t as never, te)
  }
}

async function load() {
  writeStoredPageSize(route.path, query.page_size)
  loading.value = true
  try {
    const result = await listServers(query)
    items.value = result.data
    total.value = result.meta.total || result.data.length
    sortDraft.value = Object.fromEntries(result.data.map(server => [server.id, String(server.display_index)]))
  } catch (error) {
    notifyAPIError(error, t as never, te)
  } finally {
    loading.value = false
  }
}
function open(item?: ServerRecord) { editing.value = item; editor.value = true }
function publicSummary(server: ServerRecord) {
  const parsed = parsePublicNote(server.public_note ? JSON.stringify(server.public_note) : '')
  return parsed.form.presentation.slogan || parsed.form.presentation.locationLabel || ''
}
function hasPublicSummary(server: ServerRecord) { return Boolean(publicSummary(server)) }
function hasAdminNote(server: ServerRecord) { return Boolean(server.note?.trim()) }
function openNote(server: ServerRecord) {
  if (!hasAdminNote(server)) return
  noteServer.value = server
  noteDialog.value = true
}
function reportedAddresses(server: ServerRecord) { return hostAddresses(server.host) }
function agentVersionText(server: ServerRecord) { return server.host?.Version?.trim() || '—' }
async function removeOne(server: ServerRecord) { await ElMessageBox.confirm(t('confirmDelete'), t('dangerousAction'), { type: 'warning' }); try { await deleteServer(server.id); ElMessage.success(t('deleteSuccess')); await load() } catch (error) { notifyAPIError(error, t as never, te) } }
async function groupSelected() { try { const { value } = await ElMessageBox.prompt(t('group'), t('batchGroup'), { inputValue: selected.value[0]?.tag || '' }); await batchUpdateServerGroup(selected.value.map(server => server.id), value); await load() } catch { /* user cancelled */ } }
async function deleteSelected() { await ElMessageBox.confirm(t('confirmDelete'), t('dangerousAction'), { type: 'warning' }); try { await batchDeleteServers(selected.value.map(server => server.id)); selected.value = []; await load(); ElMessage.success(t('deleteSuccess')) } catch (error) { notifyAPIError(error, t as never, te) } }
function status(server: ServerRecord) { return hostListTone(server) }
function coverageText(server: ServerRecord) {
  const coverage = server.telemetry?.coverage
  if (coverage && /^\d+\/\d+$/.test(coverage)) return coverage
  const key = coverage || status(server)
  return te(key) ? t(key) : (key || '—')
}
function availabilityTone(server: ServerRecord) { return hostCoverageTone(server) }
function availabilityIcon(server: ServerRecord) { return hostCoverageIcon(hostCoverageTone(server)) }
function hasTraffic(server?: ServerRecord | null) { return Boolean(server?.traffic_summaries?.length) }
function primaryTrafficSummary(server: ServerRecord): TrafficSummary | undefined {
  const rows = server.traffic_summaries || []
  if (!rows.length) return undefined
  return rows.reduce((best, row) => row.usage_percent > best.usage_percent ? row : best)
}
function trafficTone(status?: string) {
  if (status === 'exceeded') return 'is-bad'
  if (status === 'warning') return 'is-warn'
  if (status === 'normal') return 'is-ok'
  return ''
}
function trafficLabel(server: ServerRecord) {
  const summary = primaryTrafficSummary(server)
  if (!summary) return ''
  return `${formatBytes(summary.used_bytes, locale.value)} / ${formatBytes(summary.quota_bytes, locale.value)}`
}
function hasReportedTransfer(server: ServerRecord) {
  const state = server.state
  if (!state) return false
  return state.NetInTransfer != null || state.NetOutTransfer != null
}
function usedTrafficLabel(server: ServerRecord) {
  if (!hasReportedTransfer(server)) return ''
  return formatBytes((server.state?.NetInTransfer || 0) + (server.state?.NetOutTransfer || 0), locale.value)
}
function display(value: unknown, key: string) { return formatAdminValue(value, key, locale.value, t as never, te) }
function observerLabel(item: { observer_kind?: string; observer_name?: string; observer_id?: string }) {
  if (item.observer_kind === 'primary' || item.observer_id === 'primary') return t('observerKindPrimary')
  const name = item.observer_name || item.observer_id || ''
  return name ? shortId(name) : '—'
}
function pathRowKey(row: ConnectionPath) { return `${row.node_uuid}:${row.observer_id}` }
function pathObserverLabel(path: ConnectionPath) {
  if (path.observer_kind === 'primary') return t('observerKindPrimary')
  return path.observer_name || path.observer_id
}
function probeChipText(path: ProbePath) {
  if (probeHasNoTarget(path)) return '—'
  if (path.reachable && path.sampled_at) return display(path.display_rtt_ms, 'display_rtt_ms')
  if (!path.sampled_at) return t('offline')
  return t('probeTimeout')
}
function openProbe(row: ProbePath) {
  activeProbe.value = row
  probeDialog.value = true
}
function onSelect(row: ServerRecord, checked: boolean | string | number) { selected.value = toggleRowSelection(selected.value, row, !!checked) }
function showInstall(server: ServerRecord, secret = '') { installServer.value = server; installSecret.value = secret; installDialog.value = true }
function showUpgrade(server: ServerRecord) { upgradeServer.value = server; upgradeDialog.value = true }
async function copyDefaultInstall(server: ServerRecord) {
  if (copyingInstall.value) return
  copyingInstall.value = server.id
  try {
    const preview = await getServerInstallPreview(server.id, defaultInstallPreviewBody())
    await navigator.clipboard.writeText(preview.command)
    ElMessage.success(t('copied'))
  } catch (error) {
    notifyAPIError(error, t as never, te)
  } finally {
    copyingInstall.value = undefined
  }
}
async function resetSecret(server: ServerRecord) { await ElMessageBox.confirm(t('confirmResetSecret'), t('confirm'), { type: 'warning' }); try { const result = await resetServerSecret(server.id); showInstall(server, result.secret) } catch (error) { notifyAPIError(error, t as never, te) } }
async function resetAvailabilityHistory(server: ServerRecord) { await ElMessageBox.confirm(t('confirmResetAvailability'), t('confirm'), { type: 'warning' }); try { await resetServerAvailability(server.id); await load(); ElMessage.success(t('saveSuccess')) } catch (error) { notifyAPIError(error, t as never, te) } }
async function saved(server: ServerRecord, created: boolean) { await load(); if (created) showInstall(server, server.secret || '') }
async function loadAvailabilityHistory(server: ServerRecord) {
  const to = new Date()
  const from = new Date(to.getTime() - availabilityHours.value * 3600 * 1000)
  const rows: ResourceRecord[] = []
  let cursor: string | undefined
  for (let page = 0; page < 3; page++) {
    const result = await listServerAvailability(server.id, {
      from: from.toISOString(),
      to: to.toISOString(),
      limit: 1000,
      cursor,
    })
    rows.push(...result.data)
    cursor = result.meta.next_cursor
    if (!cursor) break
  }
  availability.value = rows
}
async function loadTrafficHistory(server: ServerRecord) {
  if (!hasTraffic(server)) {
    trafficHistories.value = []
    return
  }
  const tz = Intl.DateTimeFormat().resolvedOptions().timeZone
  const result = await getServerTrafficHistory(server.id, { tz })
  trafficHistories.value = result.data
}
async function showHistory(server: ServerRecord, tab = 'availability') {
  historyServer.value = server
  historyDrawer.value = true
  historyTab.value = hasTraffic(server) ? tab : (tab === 'traffic' ? 'availability' : tab)
  historyLoading.value = true
  history.value = []
  availability.value = []
  connectionPaths.value = []
  probePaths.value = []
  trafficHistories.value = []
  try {
    const tasks: Promise<unknown>[] = [
      listOfflineHistory(server.id).then(result => { history.value = result.data }),
      listConnectionPaths({ server_id: server.id }).then(result => { connectionPaths.value = result.data }),
      listProbePaths({ server_id: server.id }).then(result => { probePaths.value = result.data }),
      loadAvailabilityHistory(server),
    ]
    if (hasTraffic(server)) tasks.push(loadTrafficHistory(server))
    await Promise.all(tasks)
  } catch (error) {
    notifyAPIError(error, t as never, te)
  } finally {
    historyLoading.value = false
  }
}
async function onAvailabilityRangeChange() {
  const server = historyServer.value
  if (!server || !historyDrawer.value) return
  historyLoading.value = true
  try {
    await loadAvailabilityHistory(server)
  } catch (error) {
    notifyAPIError(error, t as never, te)
  } finally {
    historyLoading.value = false
  }
}
const availabilitySegmentsAsc = computed(() => buildAvailabilitySegments(availability.value))
const availabilitySegments = computed(() => [...availabilitySegmentsAsc.value].reverse())
const availabilitySummary = computed(() => summarizeAvailability(availabilitySegmentsAsc.value))
function segmentTone(segment: AvailabilitySegment) {
  if (segment.kind === 'gap') return ''
  if (segment.connectivity === 'full') return 'online'
  if (segment.connectivity === 'partial') return 'degraded'
  if (segment.connectivity === 'unavailable') return 'offline'
  return ''
}
function segmentStatusText(segment: AvailabilitySegment) {
  if (segment.kind === 'gap') return t('noObservation')
  const host = te(segment.host) ? t(segment.host) : (segment.host || t('unknown'))
  const connectivity = te(segment.connectivity) ? t(segment.connectivity) : (segment.connectivity || t('unknown'))
  return `${host} · ${connectivity}`
}
function segmentObserverText(segment: AvailabilitySegment) {
  if (segment.kind === 'gap') return '—'
  const names = segment.observerEvidence.filter(item => item.seen).map(item => observerLabel(item))
  return names.length ? names.join(', ') : '—'
}
function observerDetail(item: ObserverEvidence) {
  return `${observerLabel(item)} · ${t(item.seen ? 'yes' : 'no')} / ${t(item.healthy ? 'healthy' : 'degraded')}`
}
function percentText(value: number | null) {
  if (value == null) return '—'
  return `${new Intl.NumberFormat(locale.value, { maximumFractionDigits: 1 }).format(value)}%`
}
async function removeHistory(row: ResourceRecord) { await ElMessageBox.confirm(t('confirmDelete'), t('dangerousAction'), { type: 'warning' }); await deleteOfflineHistory(Number(row.id)); await showHistory(historyServer.value!) }

async function commitDisplayIndex(server: ServerRecord) {
  if (sortSaving.value[server.id]) return
  const next = Number(String(sortDraft.value[server.id] ?? '').trim())
  if (!Number.isFinite(next)) {
    sortDraft.value = { ...sortDraft.value, [server.id]: String(server.display_index) }
    return
  }
  if (next === server.display_index) {
    sortDraft.value = { ...sortDraft.value, [server.id]: String(server.display_index) }
    return
  }
  sortSaving.value = { ...sortSaving.value, [server.id]: true }
  try {
    await updateServerDisplayIndex(server.id, next)
    ElMessage.success(t('saveSuccess'))
    await load()
  } catch (error) {
    sortDraft.value = { ...sortDraft.value, [server.id]: String(server.display_index) }
    notifyAPIError(error, t as never, te)
  } finally {
    sortSaving.value = { ...sortSaving.value, [server.id]: false }
  }
}

onMounted(async () => {
  hoverMedia = window.matchMedia('(hover: hover) and (pointer: fine)')
  onHoverMediaChange()
  hoverMedia.addEventListener('change', onHoverMediaChange)
  await load()
  if (route.query.create === '1') open()
})
onUnmounted(() => { hoverMedia?.removeEventListener('change', onHoverMediaChange) })
</script>

<template>
  <div class="servers-page">
  <div class="page-head"><h1>{{ t('servers') }}</h1><el-button type="primary" @click="open()"><i class="ri-add-line"></i>{{ t('createServer') }}</el-button></div>
  <section class="surface table-card">
    <div class="toolbar">
      <el-input v-model="query.q" class="search-input" clearable :placeholder="t('search')" @keyup.enter="query.page=1;load()"><template #prefix><i class="ri-search-line"></i></template></el-input>
      <el-button @click="query.page=1;load()"><i class="ri-search-line"></i>{{ t('submitSearch') }}</el-button>
      <el-button @click="groupManager=true"><i class="ri-folder-settings-line"></i>{{ t('manageGroups') }}</el-button>
      <template v-if="selected.length">
        <el-button @click="groupSelected"><i class="ri-folder-transfer-line"></i>{{ t('batchGroup') }}</el-button>
        <el-button type="danger" plain @click="deleteSelected"><i class="ri-delete-bin-6-line"></i>{{ t('batchDelete') }}</el-button>
      </template>
      <span class="toolbar-spacer"></span>
      <input ref="importFile" class="sr-only" type="file" accept="application/json,.json" @change="onImportFile">
      <el-button @click="pickImport"><i class="ri-upload-line"></i>{{ t('importServers') }}</el-button>
      <el-button :loading="exporting" @click="exportAll"><i class="ri-download-line"></i>{{ t('exportServers') }}</el-button>
      <el-button @click="load"><i class="ri-refresh-line"></i>{{ t('refresh') }}</el-button>
    </div>
    <div class="servers-table-wrap desktop-only">
    <el-table v-loading="loading" height="100%" :data="items" row-key="id" @selection-change="selected=$event">
      <el-table-column type="selection" width="46"/>
      <el-table-column class-name="col-status" label-class-name="col-status" width="44" align="center">
        <template #default="{row}">
          <span class="status-dot" :class="status(row)"></span>
        </template>
      </el-table-column>
      <el-table-column prop="name" :label="t('name')" min-width="200">
        <template #default="{row}">
          <div class="server-name">
            <strong>{{ row.name }}</strong>
            <small v-if="hasPublicSummary(row)">{{ publicSummary(row) }}</small>
          </div>
        </template>
      </el-table-column>
      <el-table-column :label="t('note')" width="72" align="center">
        <template #default="{row}">
          <el-tooltip v-if="hasAdminNote(row)" :disabled="!hoverCapable || noteDialog" placement="top" :show-after="300" :enterable="true">
            <template #content>
              <div class="admin-note-tip">{{ row.note }}</div>
            </template>
            <el-button text class="actions-icon admin-note-btn" :aria-label="t('note')" @click="openNote(row)">
              <i class="ri-sticky-note-fill"></i>
            </el-button>
          </el-tooltip>
          <span v-else class="admin-note-empty" aria-hidden="true"><i class="ri-sticky-note-line"></i></span>
        </template>
      </el-table-column>
      <el-table-column prop="tag" :label="t('group')" width="140">
        <template #default="{row}"><el-tag effect="plain">{{ row.tag || 'default' }}</el-tag></template>
      </el-table-column>
      <el-table-column :label="t('displayIndex')" width="140">
        <template #default="{row}">
          <el-input
            v-model="sortDraft[row.id]"
            class="sort-input"
            inputmode="numeric"
            :disabled="!!sortSaving[row.id]"
            @keyup.enter="commitDisplayIndex(row)"
            @blur="commitDisplayIndex(row)"
          />
        </template>
      </el-table-column>
      <el-table-column :label="t('platform')" min-width="170">
        <template #default="{row}">{{ row.host?.Platform || '—' }}</template>
      </el-table-column>
      <el-table-column :label="t('agentVersion')" min-width="120">
        <template #default="{row}">{{ agentVersionText(row) }}</template>
      </el-table-column>
      <el-table-column :label="t('ipv4')" min-width="140">
        <template #default="{row}">
          <CopyableText :value="reportedAddresses(row).ipv4 || null" />
        </template>
      </el-table-column>
      <el-table-column :label="t('ipv6')" min-width="180">
        <template #default="{row}">
          <CopyableText :value="reportedAddresses(row).ipv6 || null" />
        </template>
      </el-table-column>
      <el-table-column :label="t('availability')" width="140">
        <template #default="{row}">
          <el-button text class="availability-entry" :class="availabilityTone(row)" :aria-label="t('availabilityHistory')" @click="showHistory(row)">
            <i :class="availabilityIcon(row)"></i>{{ coverageText(row) }}
          </el-button>
        </template>
      </el-table-column>
      <el-table-column :label="t('traffic')" min-width="160">
        <template #default="{row}">
          <el-button v-if="primaryTrafficSummary(row)" text class="traffic-entry" :class="trafficTone(primaryTrafficSummary(row)?.status)" :aria-label="t('traffic')" @click="showHistory(row, 'traffic')">
            {{ trafficLabel(row) }}
          </el-button>
          <span v-else-if="usedTrafficLabel(row)" class="traffic-entry is-ok">{{ usedTrafficLabel(row) }}</span>
          <span v-else class="muted">—</span>
        </template>
      </el-table-column>
      <el-table-column prop="last_active" :label="t('lastSeen')" width="190">
        <template #default="{row}">{{ display(row.last_active,'last_active') }}</template>
      </el-table-column>
      <el-table-column :label="t('actions')" width="108" fixed="right">
        <template #default="{row}">
          <div class="inline-actions">
            <el-tooltip :content="t('copyCommand')" placement="top" :show-after="300" :disabled="!hoverCapable">
              <el-button text class="actions-icon" :aria-label="t('copyCommand')" :loading="copyingInstall === row.id" @click="copyDefaultInstall(row)">
                <i class="ri-file-copy-line"></i>
              </el-button>
            </el-tooltip>
            <el-dropdown trigger="click">
              <el-button text class="actions-more" :aria-label="t('actions')"><i class="ri-more-fill"></i></el-button>
              <template #dropdown>
                <el-dropdown-menu>
                  <el-dropdown-item @click="open(row)"><i class="ri-edit-line"></i>{{ t('edit') }}</el-dropdown-item>
                  <el-dropdown-item @click="showInstall(row)"><i class="ri-download-cloud-2-line"></i>{{ t('installAgent') }}</el-dropdown-item>
                  <el-dropdown-item @click="showUpgrade(row)"><i class="ri-refresh-line"></i>{{ t('upgradeAgent') }}</el-dropdown-item>
                  <el-dropdown-item @click="showHistory(row)"><i class="ri-history-line"></i>{{ t('availabilityHistory') }}</el-dropdown-item>
                  <el-dropdown-item @click="resetSecret(row)"><i class="ri-key-2-line"></i>{{ t('resetSecret') }}</el-dropdown-item>
                  <el-dropdown-item @click="resetAvailabilityHistory(row)"><i class="ri-restart-line"></i>{{ t('resetAvailability') }}</el-dropdown-item>
                  <el-dropdown-item divided @click="removeOne(row)"><i class="ri-delete-bin-6-line"></i>{{ t('delete') }}</el-dropdown-item>
                </el-dropdown-menu>
              </template>
            </el-dropdown>
          </div>
        </template>
      </el-table-column>
      <template #empty><AppEmpty icon="ri-server-line" :description="t('noData')"/></template>
    </el-table>
    </div>
    <div class="mobile-only" v-loading="loading">
      <AppEmpty v-if="!items.length && !loading" icon="ri-server-line" :description="t('noData')"/>
      <div v-else class="mobile-card-list">
        <article v-for="row in items" :key="row.id" class="mobile-card mobile-card--server">
          <div class="mobile-card-head">
            <el-checkbox :model-value="isRowSelected(selected, row)" @change="onSelect(row, $event)" />
            <span class="mobile-card-status"><span class="status-dot" :class="status(row)"></span></span>
            <div class="mobile-card-title">
              <strong>{{ row.name }}</strong>
              <small v-if="hasPublicSummary(row)">{{ publicSummary(row) }}</small>
            </div>
            <div class="mobile-card-actions">
              <el-tooltip v-if="hasAdminNote(row)" :disabled="!hoverCapable || noteDialog" placement="top" :show-after="300" :enterable="true">
                <template #content>
                  <div class="admin-note-tip">{{ row.note }}</div>
                </template>
                <el-button text class="actions-icon admin-note-btn" :aria-label="t('note')" @click="openNote(row)">
                  <i class="ri-sticky-note-fill"></i>
                </el-button>
              </el-tooltip>
              <span v-else class="admin-note-empty" aria-hidden="true"><i class="ri-sticky-note-line"></i></span>
              <el-tooltip :content="t('copyCommand')" placement="top" :show-after="300" :disabled="!hoverCapable">
                <el-button text class="actions-icon" :aria-label="t('copyCommand')" :loading="copyingInstall === row.id" @click="copyDefaultInstall(row)">
                  <i class="ri-file-copy-line"></i>
                </el-button>
              </el-tooltip>
              <el-dropdown trigger="click">
                <el-button text class="actions-more" :aria-label="t('actions')"><i class="ri-more-fill"></i></el-button>
                <template #dropdown>
                  <el-dropdown-menu>
                    <el-dropdown-item @click="open(row)"><i class="ri-edit-line"></i>{{ t('edit') }}</el-dropdown-item>
                    <el-dropdown-item @click="showInstall(row)"><i class="ri-download-cloud-2-line"></i>{{ t('installAgent') }}</el-dropdown-item>
                    <el-dropdown-item @click="showUpgrade(row)"><i class="ri-refresh-line"></i>{{ t('upgradeAgent') }}</el-dropdown-item>
                    <el-dropdown-item @click="showHistory(row)"><i class="ri-history-line"></i>{{ t('availabilityHistory') }}</el-dropdown-item>
                    <el-dropdown-item @click="resetSecret(row)"><i class="ri-key-2-line"></i>{{ t('resetSecret') }}</el-dropdown-item>
                    <el-dropdown-item @click="resetAvailabilityHistory(row)"><i class="ri-restart-line"></i>{{ t('resetAvailability') }}</el-dropdown-item>
                    <el-dropdown-item divided @click="removeOne(row)"><i class="ri-delete-bin-6-line"></i>{{ t('delete') }}</el-dropdown-item>
                  </el-dropdown-menu>
                </template>
              </el-dropdown>
            </div>
          </div>
          <div class="mobile-card-chips">
            <el-tag effect="plain">{{ row.tag || 'default' }}</el-tag>
            <el-button text class="availability-entry" :class="availabilityTone(row)" :aria-label="t('availabilityHistory')" @click="showHistory(row)">
              <i :class="availabilityIcon(row)"></i>{{ coverageText(row) }}
            </el-button>
            <el-button v-if="primaryTrafficSummary(row)" text class="traffic-entry" :class="trafficTone(primaryTrafficSummary(row)?.status)" :aria-label="t('traffic')" @click="showHistory(row, 'traffic')">
              {{ trafficLabel(row) }}
            </el-button>
            <span v-else-if="usedTrafficLabel(row)" class="traffic-entry is-ok">{{ usedTrafficLabel(row) }}</span>
          </div>
          <dl class="mobile-card-meta mobile-card-meta--stats">
            <div><dt>{{ t('platform') }}</dt><dd>{{ row.host?.Platform || '—' }}</dd></div>
            <div><dt>{{ t('agentVersion') }}</dt><dd>{{ agentVersionText(row) }}</dd></div>
            <div><dt>{{ t('lastSeen') }}</dt><dd>{{ display(row.last_active,'last_active') }}</dd></div>
            <div><dt>{{ t('ipv4') }}</dt><dd><CopyableText :value="reportedAddresses(row).ipv4 || null" /></dd></div>
            <div><dt>{{ t('ipv6') }}</dt><dd><CopyableText :value="reportedAddresses(row).ipv6 || null" /></dd></div>
          </dl>
          <dl class="mobile-card-meta mobile-card-meta--sort">
            <div>
              <dt>{{ t('displayIndex') }}</dt>
              <dd>
                <el-input
                  v-model="sortDraft[row.id]"
                  class="sort-input"
                  inputmode="numeric"
                  :disabled="!!sortSaving[row.id]"
                  @keyup.enter="commitDisplayIndex(row)"
                  @blur="commitDisplayIndex(row)"
                />
              </dd>
            </div>
          </dl>
        </article>
      </div>
    </div>
    <div class="pagination"><el-pagination v-model:current-page="query.page" v-model:page-size="query.page_size" layout="total, sizes, prev, pager, next" :total="total" @change="load"/></div>
  </section>
  <ServerEditorDialog v-model="editor" :value="editing" @saved="saved"/>
  <ServerGroupManagerDialog v-model="groupManager" @changed="load"/>
  <ServerImportDialog v-model="importer" :document="importDocument" :items="importItems" @applied="load"/>
  <InstallAgentDialog v-model="installDialog" :server="installServer" :secret="installSecret"/>
  <UpgradeAgentDialog v-model="upgradeDialog" :server="upgradeServer"/>
  <AppDialog v-model="noteDialog" :title="`${t('note')} · ${noteServer?.name || ''}`" mode="view" width="min(560px,92vw)">
    <p class="admin-note-body">{{ noteServer?.note }}</p>
  </AppDialog>
  <AppDrawer v-model="historyDrawer" :title="`${t('details')} · ${historyServer?.name || ''}`" mode="view" size="min(980px,96vw)">
    <el-tabs v-model="historyTab">
      <el-tab-pane :label="t('availability')" name="availability">
        <div class="availability-toolbar">
          <el-select v-model="availabilityHours" class="toolbar-filter" @change="onAvailabilityRangeChange">
            <el-option v-for="hours in availabilityRangeOptions" :key="hours" :label="t('rangeLastHours', { hours })" :value="hours" />
          </el-select>
          <div v-if="availabilitySummary.windowStart" class="availability-summary">
            <span>{{ t('availabilityRate') }} <strong>{{ percentText(availabilitySummary.availablePercent) }}</strong></span>
            <span>{{ t('outages') }} <strong>{{ availabilitySummary.outageCount }}</strong></span>
            <span>{{ t('degradations') }} <strong>{{ availabilitySummary.degradedCount }}</strong></span>
            <span v-if="availabilitySummary.gapMs">{{ t('noObservation') }} <strong>{{ formatDurationMs(availabilitySummary.gapMs) }}</strong></span>
          </div>
        </div>
        <el-table class="availability-history-table" v-loading="historyLoading" table-layout="fixed" :data="availabilitySegments">
          <el-table-column :label="t('status')" min-width="160">
            <template #default="{row}">
              <span class="state-label"><span class="status-dot" :class="segmentTone(row)"></span>{{ segmentStatusText(row) }}</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('startedAt')" min-width="170"><template #default="{row}">{{ display(new Date(row.start).toISOString(), 'started_at') }}</template></el-table-column>
          <el-table-column :label="t('endedAt')" min-width="170"><template #default="{row}">{{ display(new Date(row.end).toISOString(), 'ended_at') }}</template></el-table-column>
          <el-table-column :label="t('lasted')" width="110"><template #default="{row}">{{ formatDurationMs(row.end - row.start) }}</template></el-table-column>
          <el-table-column :label="t('coverage')" width="110"><template #default="{row}">{{ coverageLabel(row) }}</template></el-table-column>
          <el-table-column :label="t('observers')" min-width="160">
            <template #default="{row}">
              <el-tooltip :disabled="row.kind === 'gap' || !row.observerEvidence.length" placement="top" :show-after="300">
                <template #content>
                  <div class="availability-observers">
                    <div v-for="item in row.observerEvidence" :key="item.observer_id">{{ observerDetail(item) }}</div>
                  </div>
                </template>
                <span class="cell-ellipsis">{{ segmentObserverText(row) }}</span>
              </el-tooltip>
            </template>
          </el-table-column>
          <template #empty><AppEmpty icon="ri-history-line" :description="t('noData')"/></template>
        </el-table>
      </el-tab-pane>
      <el-tab-pane v-if="hasTraffic(historyServer)" :label="t('traffic')" name="traffic">
        <TrafficHistoryPanel :items="trafficHistories" :loading="historyLoading" />
      </el-tab-pane>
      <el-tab-pane :label="t('nodeLinks')" name="connections">
        <el-table v-loading="historyLoading" :data="connectionPaths" :row-key="pathRowKey">
          <el-table-column class-name="col-status" label-class-name="col-status" width="44" align="center">
            <template #default="{row}"><span class="status-dot" :class="row.sink.connected ? 'online' : 'offline'"></span></template>
          </el-table-column>
          <el-table-column :label="t('observer')" min-width="160"><template #default="{row}">{{ pathObserverLabel(row) }}</template></el-table-column>
          <el-table-column :label="t('linkStatus')" width="110"><template #default="{row}">{{ t(row.sink.connected ? 'connected' : 'disconnected') }}</template></el-table-column>
          <el-table-column :label="t('lastObservation')" min-width="180"><template #default="{row}">{{ display(row.last_seen, 'last_seen') }}</template></el-table-column>
          <el-table-column :label="t('latency')" width="110"><template #default="{row}">{{ row.sink.rtt_sampled_at ? display(row.sink.last_rtt_ms, 'last_rtt_ms') : '—' }}</template></el-table-column>
          <el-table-column :label="t('pendingEvents')" width="120"><template #default="{row}">{{ display(row.sink.pending_events, 'pending_events') }}</template></el-table-column>
          <template #empty><AppEmpty icon="ri-links-line" :title="t('noPathsTitle')" :description="t('noPathsHint')"/></template>
        </el-table>
      </el-tab-pane>
      <el-tab-pane :label="t('probeObservation')" name="probes">
        <el-table v-loading="historyLoading" :data="probePaths" :row-key="probePathKey" @row-click="openProbe">
          <el-table-column :label="t('collector')" min-width="160"><template #default="{row}">{{ row.collector_name || row.collector_id }}</template></el-table-column>
          <el-table-column :label="t('target')" min-width="160"><template #default="{row}">{{ probeTargetText(row) }}</template></el-table-column>
          <el-table-column :label="t('latency')" width="120"><template #default="{row}">{{ probeChipText(row) }}</template></el-table-column>
          <el-table-column :label="t('lastObservation')" min-width="180"><template #default="{row}">{{ display(row.sampled_at, 'sampled_at') }}</template></el-table-column>
          <template #empty><AppEmpty icon="ri-radar-line" :title="t('noProbePathsTitle')" :description="t('noProbePathsHint')"/></template>
        </el-table>
      </el-tab-pane>
      <el-tab-pane :label="t('offlineHistory')" name="offline">
        <el-table v-loading="historyLoading" :data="history">
          <el-table-column prop="started_at" :label="t('startedAt')" min-width="190"><template #default="{row}">{{display(row.started_at,'started_at')}}</template></el-table-column>
          <el-table-column prop="ended_at" :label="t('endedAt')" min-width="190"><template #default="{row}">{{display(row.ended_at,'ended_at')}}</template></el-table-column>
          <el-table-column prop="duration" :label="t('duration')" width="120"/>
          <el-table-column :label="t('actions')" width="72"><template #default="{row}"><el-button text class="actions-icon" type="danger" :aria-label="t('delete')" @click="removeHistory(row)"><i class="ri-delete-bin-line"></i></el-button></template></el-table-column>
        </el-table>
      </el-tab-pane>
    </el-tabs>
  </AppDrawer>
  <ProbePathDialog v-model="probeDialog" :path="activeProbe" />
  </div>
</template>

<style scoped>
.sort-input { width: 112px; }
.sort-input :deep(.el-input__wrapper) { padding-left: 8px; padding-right: 8px; }
</style>
