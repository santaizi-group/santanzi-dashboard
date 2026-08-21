<script setup lang="ts">
import { computed, nextTick, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, type FormInstance } from 'element-plus'
import { AppDialog } from '@santaizi/ui'
import { createCollector, listAllServers, listNotificationGroups, updateCollector, updateCollectorScope, type CollectorRecord, type ServerRecord } from '@/api/adminApi'
import LocationPicker from '@/components/LocationPicker.vue'
import { useEditorSnapshot } from '@/composables/editorSnapshot'
import { notifyAPIError } from '@/composables/notify'
import { isProbeCollector } from '@/domain/collectorKind'
import type { CollectorScope, CollectorWriteRouteIntervalSeconds } from '@santaizi/api'
import { joinHostPort, parsePort, splitHostPort } from '@/domain/collectorAddress'

const defaultListenPort = 5556
const routeIntervals = [3600, 86400, 604800] as const satisfies readonly CollectorWriteRouteIntervalSeconds[]

function normalizeRouteInterval(value: unknown): CollectorWriteRouteIntervalSeconds {
  const n = Number(value)
  return (routeIntervals as readonly number[]).includes(n) ? n as CollectorWriteRouteIntervalSeconds : 86400
}

const props = defineProps<{ modelValue: boolean; value?: CollectorRecord }>()
const emit = defineEmits<{ 'update:modelValue': [boolean]; saved: [string, CollectorRecord?] }>()
const { t, te } = useI18n()
const saving = ref(false)
const formRef = ref<FormInstance>()
const servers = ref<ServerRecord[]>([])
const notificationGroups = ref<string[]>(['default'])
const selectedServerIds = ref<string[]>([])
const form = reactive({
  id: '',
  name: '',
  kind: 'observer' as 'observer' | 'probe',
  host: '',
  listen_port: String(defaultListenPort),
  access_port: '',
  tls: true,
  insecure_tls: false,
  location: '',
  probe_interval_seconds: 30,
  mtr_interval_seconds: 300,
  mtr_probes: 10,
  route_interval_seconds: 86400 as CollectorWriteRouteIntervalSeconds,
  route_keep: 10,
  tcp_ports: '22,443',
  enable_icmp: true,
  enable_tcp: true,
  enable_mtr: true,
  ip_families: ['ipv4', 'ipv6'] as Array<'ipv4' | 'ipv6'>,
  notify: false,
  notification_tag: 'default',
  latency_notify: false,
  min_latency_ms: 0,
  max_latency_ms: 0,
  fail_threshold: 3,
  scopes: [{ type: 'all', value: '' }] as CollectorScope[],
})
const snapshotValue = computed(() => ({ form, selectedServerIds: selectedServerIds.value }))
const { dirty, capture } = useEditorSnapshot(snapshotValue, computed(() => props.modelValue))
const groups = computed(() => [...new Set(servers.value.map(server => server.tag).filter(Boolean))].sort())
const tags = groups
const transferData = computed(() => servers.value.map(server => ({ key: String(server.id), label: server.name })))
const firstServerScopeIndex = computed(() => form.scopes.findIndex(scope => scope.type === 'server'))
const isProbe = computed(() => form.kind === 'probe')

function portRule(required: boolean) {
  return {
    validator: (_rule: unknown, value: unknown, callback: (error?: Error) => void) => {
      if (value === '' || value === null || value === undefined) {
        if (required) callback(new Error(t('required')))
        else callback()
        return
      }
      if (parsePort(value) == null) callback(new Error(t('required')))
      else callback()
    },
  }
}

function collapseScopesForUi(scopes: CollectorScope[]): { ui: CollectorScope[]; serverIds: string[] } {
  const serverIds = scopes.filter(scope => scope.type === 'server').map(scope => scope.value.trim()).filter(Boolean)
  const others: CollectorScope[] = scopes
    .filter(scope => scope.type !== 'server')
    .map(scope => ({ type: scope.type, value: scope.value }))
  if (scopes.some(scope => scope.type === 'server')) {
    return { ui: [...others, { type: 'server', value: '' }], serverIds }
  }
  return { ui: others.length ? others : [{ type: 'all', value: '' }], serverIds: [] }
}

function buildScopesForSubmit(): CollectorScope[] {
  if (form.scopes.some(scope => scope.type === 'all')) return [{ type: 'all', value: '' }]
  const others: CollectorScope[] = form.scopes
    .filter(scope => scope.type !== 'server')
    .map(scope => ({ type: scope.type, value: scope.value.trim() }))
  const serverScopes: CollectorScope[] = selectedServerIds.value.map(id => ({ type: 'server', value: id }))
  return [...others, ...serverScopes]
}

function addScope() {
  if (form.scopes.some(scope => scope.type === 'all')) form.scopes = []
  if (form.scopes.some(scope => scope.type === 'server')) form.scopes.push({ type: 'group', value: '' })
  else form.scopes.push({ type: 'server', value: '' })
}

function changeScope(index: number) {
  const scope = form.scopes[index]
  if (!scope) return
  if (scope.type === 'all') {
    form.scopes = [{ type: 'all', value: '' }]
    selectedServerIds.value = []
    return
  }
  if (scope.type === 'server') {
    const others = form.scopes.filter((item, current) => current !== index && item.type !== 'server' && item.type !== 'all')
    form.scopes = [...others, { type: 'server', value: '' }]
    return
  }
  scope.value = ''
  if (!form.scopes.some(item => item.type === 'server')) selectedServerIds.value = []
}

function removeScope(index: number) {
  const removed = form.scopes[index]
  form.scopes.splice(index, 1)
  if (removed?.type === 'server' || !form.scopes.some(scope => scope.type === 'server')) selectedServerIds.value = []
  if (!form.scopes.length) form.scopes = [{ type: 'all', value: '' }]
}

async function reset(value?: CollectorRecord) {
  const rawScopes: CollectorScope[] = value?.scopes?.length
    ? value.scopes.map((scope): CollectorScope => ({
      type: scope.type as CollectorScope['type'],
      value: scope.value,
    }))
    : [{ type: 'all', value: '' }]
  const collapsed = collapseScopesForUi(rawScopes)
  const parsed = splitHostPort(value?.address || '')
  const access = parsePort(parsed.port)
  const listen = parsePort(value?.listen_port) ?? access ?? defaultListenPort
  Object.assign(form, {
    id: value?.id || '',
    name: value?.name || '',
    kind: isProbeCollector(value) ? 'probe' : 'observer',
    host: parsed.host,
    listen_port: String(listen),
    access_port: access == null ? '' : String(access),
    tls: value?.tls ?? true,
    insecure_tls: value?.insecure_tls ?? false,
    location: value?.location || '',
    probe_interval_seconds: value?.probe_interval_seconds || 30,
    mtr_interval_seconds: value?.mtr_interval_seconds || 300,
    mtr_probes: value?.mtr_probes || 10,
    route_interval_seconds: normalizeRouteInterval(value?.route_interval_seconds),
    route_keep: value?.route_keep || 10,
    tcp_ports: value?.tcp_ports || '22,443',
    enable_icmp: value?.enable_icmp ?? true,
    enable_tcp: value?.enable_tcp ?? true,
    enable_mtr: value?.enable_mtr ?? true,
    ip_families: [
      ...(value?.enable_ipv4 === false ? [] : ['ipv4' as const]),
      ...(value?.enable_ipv6 === false ? [] : ['ipv6' as const]),
    ],
    notify: value?.notify ?? false,
    notification_tag: value?.notification_tag || 'default',
    latency_notify: value?.latency_notify ?? false,
    min_latency_ms: value?.min_latency_ms || 0,
    max_latency_ms: value?.max_latency_ms || 0,
    fail_threshold: value?.fail_threshold || 3,
    scopes: collapsed.ui,
  })
  selectedServerIds.value = collapsed.serverIds
  try {
    const [serverResult, groups] = await Promise.all([listAllServers(), listNotificationGroups()])
    servers.value = serverResult.data
    notificationGroups.value = groups.length ? groups : ['default']
  } catch (error) { notifyAPIError(error, t as never, te) }
  await nextTick()
  capture()
}

async function submit() {
  await formRef.value?.validate()
  if (isProbe.value && !form.ip_families.length) {
    ElMessage.warning(t('invalidProbeIPFamily'))
    return
  }
  const scopes = buildScopesForSubmit()
  const hasAll = scopes.some(scope => scope.type === 'all')
  const incomplete = scopes.some(scope => scope.type !== 'all' && !scope.value.trim())
  if (!scopes.length || incomplete || (hasAll && scopes.length !== 1)) {
    ElMessage.warning(t('invalidCollectorScope'))
    return
  }
  const probeFields = {
    probe_interval_seconds: Number(form.probe_interval_seconds) || 30,
    mtr_interval_seconds: Number(form.mtr_interval_seconds) || 300,
    mtr_probes: Number(form.mtr_probes) || 10,
    route_interval_seconds: normalizeRouteInterval(form.route_interval_seconds),
    route_keep: Number(form.route_keep) || 10,
    tcp_ports: form.tcp_ports.trim() || '22,443',
    enable_icmp: form.enable_icmp,
    enable_tcp: form.enable_tcp,
    enable_mtr: form.enable_mtr,
    enable_ipv4: form.ip_families.includes('ipv4'),
    enable_ipv6: form.ip_families.includes('ipv6'),
    notify: form.notify,
    notification_tag: form.notification_tag,
    latency_notify: form.latency_notify,
    min_latency_ms: Number(form.min_latency_ms) || 0,
    max_latency_ms: Number(form.max_latency_ms) || 0,
    fail_threshold: Number(form.fail_threshold) || 3,
  }
  let payload: Parameters<typeof createCollector>[0]
  if (isProbe.value) {
    payload = {
      name: form.name,
      kind: form.kind,
      location: form.location,
      scopes,
      ...probeFields,
    }
  } else {
    const listen = parsePort(form.listen_port)
    const access = parsePort(form.access_port) ?? listen
    if (!form.host.trim() || listen == null || access == null) {
      ElMessage.warning(t('required'))
      return
    }
    payload = {
      name: form.name,
      kind: form.kind,
      address: joinHostPort(form.host, access),
      listen_port: listen,
      tls: form.tls,
      insecure_tls: form.insecure_tls,
      location: form.location,
      scopes,
      ...probeFields,
    }
  }
  saving.value = true
  try {
    let token = ''
    let created: CollectorRecord | undefined
    if (form.id) {
      await updateCollector(form.id, payload)
      await updateCollectorScope(form.id, { scopes })
    } else {
      const result = await createCollector(payload)
      token = result.registration_token
      created = result.collector as CollectorRecord
    }
    capture()
    emit('update:modelValue', false)
    emit('saved', token, created)
    ElMessage.success(t('saveSuccess'))
  } catch (error) { notifyAPIError(error, t as never, te) }
  finally { saving.value = false }
}

watch(() => props.modelValue, value => { if (value) void reset(props.value) })
</script>

<template>
  <AppDialog :model-value="modelValue" :title="form.id ? t('editCollector') : t('createCollector')" mode="edit" :dirty="dirty" :submitting="saving" width="min(920px, 96vw)" @update:model-value="emit('update:modelValue', $event)">
    <el-form ref="formRef" :model="form" label-position="top" @submit.prevent="submit">
      <div class="editor-grid">
        <el-form-item :label="t('name')" prop="name" :rules="[{ required: true, message: t('required') }]"><el-input v-model="form.name" /></el-form-item>
        <el-form-item :label="t('collectorKind')">
          <el-select v-model="form.kind" :disabled="Boolean(form.id)">
            <el-option :label="t('collectorKindObserver')" value="observer" />
            <el-option :label="t('collectorKindProbe')" value="probe" />
          </el-select>
        </el-form-item>
        <template v-if="!isProbe">
          <el-form-item :label="t('address')" prop="host" :rules="[{ required: true, message: t('required') }]"><el-input v-model="form.host" placeholder="collector.example.com" /></el-form-item>
          <el-form-item :label="t('listenPort')" prop="listen_port" :rules="[portRule(true)]"><el-input v-model="form.listen_port" inputmode="numeric" placeholder="5556" /></el-form-item>
          <el-form-item :label="t('accessPort')" prop="access_port" :rules="[portRule(false)]"><el-input v-model="form.access_port" inputmode="numeric" :placeholder="form.listen_port || '5556'" /></el-form-item>
        </template>
        <el-form-item :label="t('location')"><LocationPicker v-model="form.location" /></el-form-item>
        <template v-if="isProbe">
          <el-form-item :label="t('probeInterval')"><el-input v-model.number="form.probe_interval_seconds" inputmode="numeric" /></el-form-item>
          <el-form-item :label="t('mtrInterval')"><el-input v-model.number="form.mtr_interval_seconds" inputmode="numeric" /></el-form-item>
          <el-form-item :label="t('mtrProbes')"><el-input v-model.number="form.mtr_probes" inputmode="numeric" /></el-form-item>
          <el-form-item :label="t('routeInterval')">
            <el-select v-model="form.route_interval_seconds">
              <el-option :label="t('routeIntervalHour')" :value="3600" />
              <el-option :label="t('routeIntervalDay')" :value="86400" />
              <el-option :label="t('routeIntervalWeek')" :value="604800" />
            </el-select>
          </el-form-item>
          <el-form-item :label="t('routeKeep')"><el-input v-model.number="form.route_keep" inputmode="numeric" /></el-form-item>
          <el-form-item :label="t('defaultTcpPorts')"><el-input v-model="form.tcp_ports" /></el-form-item>
          <el-form-item :label="t('probeIPFamilies')" prop="ip_families" :rules="[{ required: true, type: 'array', min: 1, message: t('invalidProbeIPFamily') }]">
            <el-select v-model="form.ip_families" multiple>
              <el-option label="IPv4" value="ipv4" />
              <el-option label="IPv6" value="ipv6" />
            </el-select>
          </el-form-item>
          <el-form-item :label="t('failThreshold')"><el-input v-model.number="form.fail_threshold" inputmode="numeric" /></el-form-item>
          <el-form-item :label="t('notificationGroup')">
            <el-select v-model="form.notification_tag" filterable allow-create>
              <el-option v-for="group in notificationGroups" :key="group" :label="group" :value="group" />
            </el-select>
          </el-form-item>
        </template>
      </div>
      <div v-if="!isProbe" class="switch-grid"><label><span>{{ t('tls') }}</span><el-switch v-model="form.tls" /></label><label v-if="form.tls"><span>{{ t('insecureTLS') }}</span><el-switch v-model="form.insecure_tls" /></label></div>
      <div v-else class="switch-grid">
        <label><span>{{ t('enableICMP') }}</span><el-switch v-model="form.enable_icmp" /></label>
        <label><span>{{ t('enableTCP') }}</span><el-switch v-model="form.enable_tcp" /></label>
        <label><span>{{ t('enableMTR') }}</span><el-switch v-model="form.enable_mtr" /></label>
        <label><span>{{ t('notify') }}</span><el-switch v-model="form.notify" /></label>
        <label><span>{{ t('EnableLatencyNotification') }}</span><el-switch v-model="form.latency_notify" /></label>
      </div>
      <div v-if="isProbe && form.latency_notify" class="editor-grid">
        <el-form-item :label="t('MinLatency')"><el-input v-model.number="form.min_latency_ms" inputmode="numeric" /></el-form-item>
        <el-form-item :label="t('MaxLatency')"><el-input v-model.number="form.max_latency_ms" inputmode="numeric" /></el-form-item>
      </div>
      <div class="editor-section">
        <div class="editor-section-title"><h3>{{ t('scope') }}</h3><el-button @click="addScope"><i class="ri-add-line"></i>{{ t('addScope') }}</el-button></div>
        <div class="scope-list">
          <div v-for="(scope, index) in form.scopes" :key="index" class="scope-block">
            <div class="typed-scope-row">
              <el-select v-model="scope.type" @change="changeScope(index)">
                <el-option :label="t('scopeAll')" value="all"/>
                <el-option :label="t('scopeServer')" value="server"/>
                <el-option :label="t('scopeGroup')" value="group"/>
                <el-option :label="t('scopeTag')" value="tag"/>
              </el-select>
              <span v-if="scope.type === 'server'" class="scope-all-value">{{ selectedServerIds.length ? `${t('selectedServers')} · ${selectedServerIds.length}` : t('PleaseSelect') }}</span>
              <el-select v-else-if="scope.type === 'group'" v-model="scope.value" filterable allow-create><el-option v-for="group in groups" :key="group" :label="group" :value="group"/></el-select>
              <el-select v-else-if="scope.type === 'tag'" v-model="scope.value" filterable allow-create><el-option v-for="tag in tags" :key="tag" :label="tag" :value="tag"/></el-select>
              <span v-else class="scope-all-value">{{ t('allServers') }}</span>
              <el-button circle :disabled="form.scopes.length === 1" :aria-label="t('delete')" @click="removeScope(index)"><i class="ri-delete-bin-6-line"></i></el-button>
            </div>
            <el-transfer
              v-if="scope.type === 'server' && index === firstServerScopeIndex"
              v-model="selectedServerIds"
              filterable
              :filter-placeholder="t('searchServers')"
              :data="transferData"
              :titles="[t('availableServers'), t('selectedServers')]"
              class="server-transfer"
            />
          </div>
        </div>
      </div>
    </el-form>
    <template #footer="{ close }"><el-button :disabled="saving" @click="close()">{{ t('cancel') }}</el-button><el-button type="primary" :loading="saving" @click="submit">{{ t('save') }}</el-button></template>
  </AppDialog>
</template>
