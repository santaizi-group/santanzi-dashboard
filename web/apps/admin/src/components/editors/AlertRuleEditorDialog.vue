<script setup lang="ts">
import { computed, nextTick, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, type FormInstance } from 'element-plus'
import { AppDialog } from '@santaizi/ui'
import { createAlertRule, listAllServers, listNotificationGroups, updateAlertRule, type ServerRecord } from '@/api/adminApi'
import { useEditorSnapshot } from '@/composables/editorSnapshot'
import { notifyAPIError } from '@/composables/notify'
import type { AlertCondition, AlertMetric, AlertRuleRecord } from '@/types/admin'

interface VisualCondition extends AlertCondition { scope_mode: 'all' | 'include' | 'exclude'; server_ids: number[] }
const props = defineProps<{ modelValue: boolean; value?: AlertRuleRecord }>()
const emit = defineEmits<{ 'update:modelValue': [boolean]; saved: [] }>()
const { t, te } = useI18n()
const saving = ref(false)
const formRef = ref<FormInstance>()
const servers = ref<ServerRecord[]>([])
const groups = ref<string[]>([])
const form = reactive<{ id: number; name: string; notification_tag: string; trigger_mode: 'always' | 'once'; enabled: boolean; conditions: VisualCondition[] }>({ id: 0, name: '', notification_tag: 'default', trigger_mode: 'always', enabled: true, conditions: [] })
const { dirty, capture } = useEditorSnapshot(form, computed(() => props.modelValue))
const transferData = computed(() => servers.value.map(server => ({ key: server.id, label: server.name })))
const metricGroups = computed(() => [
  { label: t('resourceUsage'), options: ['cpu', 'gpu', 'temperature_max', 'memory', 'swap', 'disk'] as AlertMetric[] },
  { label: t('networkPerformance'), options: ['net_in_speed', 'net_out_speed', 'net_all_speed', 'tcp_conn_count', 'udp_conn_count'] as AlertMetric[] },
  { label: t('systemHealth'), options: ['offline', 'load1', 'load5', 'load15', 'process_count'] as AlertMetric[] },
])
const metricIcons: Record<AlertMetric, string> = {
  cpu: 'ri-cpu-line', gpu: 'ri-cpu-line', temperature_max: 'ri-temp-hot-line', memory: 'ri-database-2-line', swap: 'ri-swap-line', disk: 'ri-hard-drive-3-line',
  net_in_speed: 'ri-download-cloud-2-line', net_out_speed: 'ri-upload-cloud-2-line', net_all_speed: 'ri-speed-up-line', offline: 'ri-wifi-off-line',
  load1: 'ri-pulse-line', load5: 'ri-pulse-line', load15: 'ri-pulse-line', tcp_conn_count: 'ri-links-line', udp_conn_count: 'ri-broadcast-line', process_count: 'ri-stack-line',
}
function unit(type: AlertMetric) {
  if (['cpu', 'gpu', 'memory', 'swap', 'disk'].includes(type)) return '%'
  if (type === 'temperature_max') return '°C'
  if (type.includes('net_')) return 'B/s'
  return ''
}
function durationHint(sampleCount: number) {
  const seconds = Math.max(0, Number(sampleCount) || 0) * 3
  if (seconds >= 60) return t('durationHintMinutes', { minutes: (seconds / 60).toFixed(1) })
  return t('durationHintSeconds', { seconds })
}
function clampNumber(value: unknown, min: number, max?: number, fallback = min) {
  const next = Number(value)
  if (!Number.isFinite(next)) return fallback
  if (max === undefined) return Math.max(min, next)
  return Math.min(max, Math.max(min, next))
}
function blankCondition(): VisualCondition { return { type: 'cpu', min: 0, max: 90, duration_seconds: 30, scope: { mode: 'all', server_ids: [] }, scope_mode: 'all', server_ids: [] } }
function visualCondition(value: AlertCondition): VisualCondition {
  return { ...blankCondition(), ...value, scope_mode: value.scope.mode, server_ids: [...value.scope.server_ids] }
}
function reset(value?: AlertRuleRecord) {
  Object.assign(form, { id: value?.id || 0, name: value?.name || '', notification_tag: value?.notification_tag || 'default', trigger_mode: value?.trigger_mode || 'always', enabled: value?.enabled ?? true })
  form.conditions = value?.conditions?.length ? value.conditions.map(visualCondition) : [blankCondition()]
  nextTick(capture)
}
async function loadOptions() {
  try {
    const [serverResult, groupResult] = await Promise.all([listAllServers(), listNotificationGroups()])
    servers.value = serverResult.data; groups.value = groupResult.length ? groupResult : ['default']
  } catch (error) { notifyAPIError(error, t as never, te) }
}
async function submit() {
  await formRef.value?.validate()
  saving.value = true
  try {
    const conditions: AlertCondition[] = form.conditions.map(condition => ({
      type: condition.type,
      min: condition.type === 'offline' ? null : condition.min,
      max: condition.type === 'offline' ? null : condition.max,
      duration_seconds: condition.duration_seconds,
      scope: { mode: condition.scope_mode, server_ids: condition.scope_mode === 'all' ? [] : [...condition.server_ids] },
    }))
    const payload = { name: form.name, notification_tag: form.notification_tag, trigger_mode: form.trigger_mode, enabled: form.enabled, conditions }
    if (form.id) await updateAlertRule(form.id, payload); else await createAlertRule(payload)
    capture(); emit('update:modelValue', false); emit('saved'); ElMessage.success(t('saveSuccess'))
  } catch (error) { notifyAPIError(error, t as never, te) }
  finally { saving.value = false }
}
watch(() => props.modelValue, value => { if (value) { reset(props.value); void loadOptions() } })
</script>

<template>
  <AppDialog :model-value="modelValue" :title="form.id ? t('editAlertRule') : t('createAlertRule')" mode="edit" :dirty="dirty" :submitting="saving" width="min(1040px, 97vw)" @update:model-value="emit('update:modelValue', $event)">
    <el-form ref="formRef" :model="form" label-position="top" @submit.prevent="submit">
      <div class="editor-grid">
        <el-form-item :label="t('name')" prop="name" :rules="[{ required: true, message: t('required') }]"><el-input v-model="form.name" /></el-form-item>
        <el-form-item :label="t('notificationGroup')"><el-select v-model="form.notification_tag" filterable allow-create class="field-full"><el-option v-for="group in groups" :key="group" :label="group" :value="group"/></el-select></el-form-item>
        <el-form-item :label="t('triggerMode')"><el-radio-group v-model="form.trigger_mode"><el-radio-button value="always">{{ t('triggerAlways') }}</el-radio-button><el-radio-button value="once">{{ t('triggerOnce') }}</el-radio-button></el-radio-group></el-form-item>
        <el-form-item :label="t('enabled')"><el-switch v-model="form.enabled" /></el-form-item>
      </div>
      <div class="editor-section">
        <div class="editor-section-title"><h3>{{ t('alertConditions') }}</h3><el-button type="primary" plain @click="form.conditions.push(blankCondition())"><i class="ri-add-line"></i>{{ t('addCondition') }}</el-button></div>
        <div class="condition-list">
          <section v-for="(condition, index) in form.conditions" :key="index" class="condition-card">
            <div class="condition-head"><span class="condition-icon"><i :class="metricIcons[condition.type]"></i></span><strong>{{ t(`metric_${condition.type}`) }}</strong><el-button circle :disabled="form.conditions.length === 1" :aria-label="t('delete')" @click="form.conditions.splice(index, 1)"><i class="ri-delete-bin-6-line"></i></el-button></div>
            <div class="editor-grid condition-fields">
              <el-form-item :label="t('informationType')">
                <el-select v-model="condition.type" class="field-full">
                  <el-option-group v-for="group in metricGroups" :key="group.label" :label="group.label"><el-option v-for="metric in group.options" :key="metric" :value="metric"><div class="metric-option"><i :class="metricIcons[metric]"></i><span>{{ t(`metric_${metric}`) }}</span></div></el-option></el-option-group>
                </el-select>
              </el-form-item>
              <el-form-item :label="t('durationSeconds')">
                <el-input v-model.number="condition.duration_seconds" inputmode="numeric" class="field-full" @blur="condition.duration_seconds = clampNumber(condition.duration_seconds, 3, 86400, 30)" />
                <span class="condition-duration-hint">{{ durationHint(condition.duration_seconds) }}</span>
              </el-form-item>
              <el-form-item v-if="condition.type !== 'offline'" :label="`${t('minimumThreshold')} ${unit(condition.type)}`"><el-input v-model.number="condition.min" inputmode="numeric" class="field-full" @blur="condition.min = clampNumber(condition.min, 0, undefined, 0)" /></el-form-item>
              <el-form-item v-if="condition.type !== 'offline'" :label="`${t('maximumThreshold')} ${unit(condition.type)}`"><el-input v-model.number="condition.max" inputmode="numeric" class="field-full" @blur="condition.max = clampNumber(condition.max, 0, undefined, 0)" /></el-form-item>
            </div>
            <el-form-item :label="t('serverScope')" class="scope-field">
              <el-radio-group v-model="condition.scope_mode" class="scope-mode">
                <el-radio-button value="all">{{ t('scopeAll') }}</el-radio-button>
                <el-radio-button value="include">{{ t('scopeSelectedServers') }}</el-radio-button>
                <el-radio-button value="exclude">{{ t('scopeExceptSelected') }}</el-radio-button>
              </el-radio-group>
              <el-transfer
                v-if="condition.scope_mode !== 'all'"
                v-model="condition.server_ids"
                filterable
                :filter-placeholder="t('searchServers')"
                :data="transferData"
                :titles="[t('availableServers'), condition.scope_mode === 'include' ? t('selectedServers') : t('excludedServers')]"
                class="server-transfer"
              />
            </el-form-item>
          </section>
        </div>
      </div>
    </el-form>
    <template #footer="{ close }"><el-button :disabled="saving" @click="close()">{{ t('cancel') }}</el-button><el-button type="primary" :loading="saving" @click="submit">{{ t('save') }}</el-button></template>
  </AppDialog>
</template>
