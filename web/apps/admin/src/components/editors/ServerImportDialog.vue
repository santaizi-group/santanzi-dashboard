<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { AppDialog, AppEmpty } from '@santaizi/ui'
import type { ServerBackup, ServerImportAction, ServerImportPreviewItem } from '@santaizi/api'
import { importServersBackup } from '@/api/adminApi'
import { notifyAPIError } from '@/composables/notify'

const props = defineProps<{
  modelValue: boolean
  document?: ServerBackup
  items: ServerImportPreviewItem[]
}>()
const emit = defineEmits<{ 'update:modelValue': [boolean]; applied: [] }>()
const { t, te } = useI18n()
const saving = ref(false)
const actions = reactive<Record<number, ServerImportAction>>({})
const dirty = computed(() => props.modelValue && props.items.length >= 0)

const changeLabels: Record<string, string> = {
  tag: 'group',
  note: 'note',
  public_note: 'publicNote',
  monitoring_options: 'monitoringOptions',
  display_index: 'displayIndex',
  hide_for_guest: 'hideForGuest',
  enable_ddns: 'enableDDNS',
  ddns_profiles: 'ddnsProfiles',
  probe_target: 'probeTarget',
  probe_tcp_ports: 'tcpPorts',
  probe_enable_icmp: 'enableICMP',
  probe_enable_tcp: 'enableTCP',
  probe_enable_mtr: 'enableMTR',
  traffic_policies: 'trafficPolicies',
}

const matchLabels: Record<string, string> = {
  create: 'importMatchCreate',
  update: 'importMatchUpdate',
  unchanged: 'importMatchUnchanged',
  ambiguous: 'importMatchAmbiguous',
}

watch(() => props.items, rows => {
  for (const key of Object.keys(actions)) delete actions[Number(key)]
  for (const row of rows) actions[row.index] = row.suggested_action
}, { immediate: true })

function allows(row: ServerImportPreviewItem, action: ServerImportAction) {
  return row.allowed_actions.includes(action)
}

function applyAction(row: ServerImportPreviewItem, action: ServerImportAction) {
  if (allows(row, action)) actions[row.index] = action
}

function skipAll() {
  for (const row of props.items) {
    if (allows(row, 'skip')) actions[row.index] = 'skip'
  }
}

function overwriteAll() {
  for (const row of props.items) {
    if (allows(row, 'overwrite')) actions[row.index] = 'overwrite'
  }
}

function changeText(row: ServerImportPreviewItem) {
  if (!row.changes.length) return '—'
  return row.changes.map(field => {
    const key = changeLabels[field]
    return key && te(key) ? t(key) : field
  }).join('、')
}

function matchText(row: ServerImportPreviewItem) {
  const key = matchLabels[row.match]
  return key && te(key) ? t(key) : row.match
}

function matchTone(row: ServerImportPreviewItem) {
  if (row.match === 'create') return 'success'
  if (row.match === 'update') return 'warning'
  if (row.match === 'ambiguous') return 'danger'
  return 'info'
}

function applyActionLabel(row: ServerImportPreviewItem) {
  if (allows(row, 'create')) return t('importMatchCreate')
  if (allows(row, 'overwrite')) return t('importOverwrite')
  return t('importSkip')
}

function applyActionValue(row: ServerImportPreviewItem): ServerImportAction {
  if (allows(row, 'create')) return 'create'
  if (allows(row, 'overwrite')) return 'overwrite'
  return 'skip'
}

async function submit() {
  if (!props.document) return
  const overwriteCount = props.items.filter(row => actions[row.index] === 'overwrite').length
  if (overwriteCount) {
    try {
      await ElMessageBox.confirm(t('importConfirmOverwrite', { n: overwriteCount }), t('confirm'), { type: 'warning', confirmButtonText: t('confirm'), cancelButtonText: t('cancel') })
    } catch {
      return
    }
  }
  saving.value = true
  try {
    const result = await importServersBackup({
      document: props.document,
      actions: props.items.map(row => ({ index: row.index, action: actions[row.index] ?? row.suggested_action })),
    })
    if (result.secrets_regenerated) {
      ElMessage.success(t('importSuccessSecretsRegenerated', { n: result.secrets_regenerated }))
    } else {
      ElMessage.success(t('importSuccess'))
    }
    emit('applied')
    emit('update:modelValue', false)
  } catch (error) {
    notifyAPIError(error, t as never, te)
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <AppDialog :model-value="modelValue" :title="t('importPreview')" mode="edit" :dirty="dirty" :submitting="saving" width="min(920px, 97vw)" @update:model-value="emit('update:modelValue', $event)">
    <p class="import-hint">{{ t('importHint') }}</p>
    <div class="import-toolbar">
      <el-button @click="skipAll">{{ t('importAllSkip') }}</el-button>
      <el-button @click="overwriteAll">{{ t('importAllOverwrite') }}</el-button>
    </div>
    <el-table v-if="items.length" :data="items" row-key="index">
      <el-table-column prop="name" :label="t('name')" min-width="160">
        <template #default="{ row }">
          <div class="import-name">
            <strong>{{ row.name }}</strong>
            <small v-if="row.warnings.includes('ddns_profiles_skipped')">{{ t('importWarningDdns') }}</small>
            <small v-if="row.warnings.includes('secret_conflict')">{{ t('importWarningSecretConflict') }}</small>
          </div>
        </template>
      </el-table-column>
      <el-table-column :label="t('importMatch')" width="110">
        <template #default="{ row }">
          <el-tag :type="matchTone(row)" effect="plain">{{ matchText(row) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column :label="t('importChanges')" min-width="180">
        <template #default="{ row }">{{ changeText(row) }}</template>
      </el-table-column>
      <el-table-column :label="t('actions')" min-width="180">
        <template #default="{ row }">
          <el-radio-group :model-value="actions[row.index]" @update:model-value="(value: string | number | boolean | undefined) => applyAction(row, value as ServerImportAction)">
            <el-radio v-if="allows(row, 'skip')" value="skip">{{ t('importSkip') }}</el-radio>
            <el-radio v-if="allows(row, applyActionValue(row)) && applyActionValue(row) !== 'skip'" :value="applyActionValue(row)">{{ applyActionLabel(row) }}</el-radio>
          </el-radio-group>
        </template>
      </el-table-column>
    </el-table>
    <AppEmpty v-else :description="t('noData')" />
    <template #footer="{ close }">
      <el-button :disabled="saving" @click="close()">{{ t('cancel') }}</el-button>
      <el-button type="primary" :loading="saving" @click="submit">{{ t('importServers') }}</el-button>
    </template>
  </AppDialog>
</template>

<style scoped>
.import-hint { margin: 0 0 12px; font-size: 13px; line-height: 1.5; color: var(--text-secondary); }
.import-toolbar { display: flex; flex-wrap: wrap; gap: 8px; margin: 0 0 12px; }
.import-name { display: flex; flex-direction: column; gap: 4px; min-width: 0; }
.import-name strong { font-size: 14px; line-height: 1.3; }
.import-name small { font-size: 12px; color: var(--text-secondary); }
.import-toolbar :deep(.el-button),
:deep(.el-radio) { min-height: 32px; }
:deep(.el-radio-group) { display: flex; flex-wrap: wrap; align-items: center; gap: var(--icon-text-gap); }
</style>
