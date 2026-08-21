<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { TrafficPolicyRecord } from '@/types/admin'

type QuotaUnit = 'MB' | 'GB' | 'TB'

const UNIT_FACTOR: Record<QuotaUnit, number> = {
  MB: 1048576,
  GB: 1073741824,
  TB: 1099511627776,
}
const DEFAULT_QUOTA_BYTES = 1073741824
const MIN_QUOTA_BYTES = 1048576
const QUOTA_UNITS: QuotaUnit[] = ['MB', 'GB', 'TB']

const props = defineProps<{ modelValue: TrafficPolicyRecord[]; notificationGroups: string[] }>()
const emit = defineEmits<{ 'update:modelValue': [TrafficPolicyRecord[]] }>()
const { t } = useI18n()

const quotaUnits = ref<QuotaUnit[]>([])
const quotaDrafts = ref<Record<number, string>>({})

watch(() => props.modelValue.length, (len) => {
  quotaUnits.value = Array.from({ length: len }, (_, index) => quotaUnits.value[index] ?? 'GB')
  for (const key of Object.keys(quotaDrafts.value)) {
    if (Number(key) >= len) delete quotaDrafts.value[Number(key)]
  }
}, { immediate: true })

function add() {
  const cycleStart = new Date().toISOString().replace(/\.\d{3}Z$/, 'Z')
  quotaUnits.value = [...quotaUnits.value, 'GB']
  emit('update:modelValue', [...props.modelValue, {
    name: '',
    direction: 'total',
    mode: 'recurring',
    cycle_start: cycleStart,
    cycle_interval: 1,
    cycle_unit: 'month',
    quota_bytes: DEFAULT_QUOTA_BYTES,
    warning_percent: 80,
    notification_tag: 'default',
    enabled: true,
  }])
}

function remove(index: number) {
  quotaUnits.value = quotaUnits.value.filter((_, current) => current !== index)
  const drafts: Record<number, string> = {}
  for (const [key, value] of Object.entries(quotaDrafts.value)) {
    const current = Number(key)
    if (current === index) continue
    drafts[current > index ? current - 1 : current] = value
  }
  quotaDrafts.value = drafts
  emit('update:modelValue', props.modelValue.filter((_, current) => current !== index))
}

function byteLabel(value: number) {
  if (value >= UNIT_FACTOR.TB) return `${(value / UNIT_FACTOR.TB).toFixed(2)} TB`
  if (value >= UNIT_FACTOR.GB) return `${(value / UNIT_FACTOR.GB).toFixed(2)} GB`
  return `${(value / UNIT_FACTOR.MB).toFixed(2)} MB`
}

function clampNumber(value: unknown, min: number, max?: number, fallback = min) {
  const next = Number(value)
  if (!Number.isFinite(next)) return fallback
  if (max === undefined) return Math.max(min, next)
  return Math.min(max, Math.max(min, next))
}

function formatQuotaAmount(bytes: number, unit: QuotaUnit) {
  const amount = bytes / UNIT_FACTOR[unit]
  if (!Number.isFinite(amount)) return '1'
  if (Number.isInteger(amount)) return String(amount)
  return String(Number(amount.toFixed(6)))
}

function parseQuotaBytes(value: unknown, unit: QuotaUnit) {
  const amount = Number(value)
  if (!Number.isFinite(amount) || amount <= 0) return DEFAULT_QUOTA_BYTES
  return Math.max(MIN_QUOTA_BYTES, Math.round(amount * UNIT_FACTOR[unit]))
}

function quotaDisplay(index: number, bytes: number) {
  if (quotaDrafts.value[index] !== undefined) return quotaDrafts.value[index]
  return formatQuotaAmount(bytes, quotaUnits.value[index] ?? 'GB')
}

function onQuotaInput(index: number, value: string | number) {
  quotaDrafts.value[index] = String(value)
}

function commitQuota(index: number, policy: TrafficPolicyRecord) {
  const unit = quotaUnits.value[index] ?? 'GB'
  const draft = quotaDrafts.value[index]
  delete quotaDrafts.value[index]
  policy.quota_bytes = draft === undefined
    ? Math.max(MIN_QUOTA_BYTES, Number(policy.quota_bytes) || DEFAULT_QUOTA_BYTES)
    : parseQuotaBytes(draft, unit)
}

function onQuotaUnitChange(index: number, unit: QuotaUnit) {
  quotaUnits.value[index] = unit
  delete quotaDrafts.value[index]
}
</script>

<template>
  <div class="editor-section-title"><h3>{{ t('trafficPolicies') }}</h3><el-button type="primary" plain @click="add"><i class="ri-add-line"></i>{{ t('addTrafficPolicy') }}</el-button></div>
  <div v-if="modelValue.length" class="traffic-policy-list">
    <section v-for="(policy, index) in modelValue" :key="policy.id || index" class="traffic-policy-card">
      <div class="condition-head"><span class="condition-icon"><i class="ri-exchange-2-line"></i></span><strong>{{ policy.name || t('unnamedPolicy') }}</strong><el-switch v-model="policy.enabled"/><el-button circle :aria-label="t('delete')" @click="remove(index)"><i class="ri-delete-bin-6-line"></i></el-button></div>
      <div v-if="policy.usage" class="traffic-progress"><el-progress :percentage="Math.min(100, Math.round(policy.usage.usage_percent))" :status="policy.usage.status === 'exceeded' ? 'exception' : undefined"/><span>{{ byteLabel(policy.usage.used_bytes) }} / {{ byteLabel(policy.quota_bytes) }}</span></div>
      <div class="editor-grid">
        <el-form-item :label="t('name')" required><el-input v-model="policy.name" /></el-form-item>
        <el-form-item :label="t('notificationGroup')"><el-select v-model="policy.notification_tag" filterable allow-create class="field-full"><el-option v-for="group in notificationGroups" :key="group" :label="group" :value="group" /></el-select></el-form-item>
        <el-form-item :label="t('trafficDirection')"><el-select v-model="policy.direction" class="field-full"><el-option :label="t('inbound')" value="inbound"/><el-option :label="t('outbound')" value="outbound"/><el-option :label="t('totalDirection')" value="total"/></el-select></el-form-item>
        <el-form-item :label="t('trafficMode')"><el-segmented v-model="policy.mode" :options="[{ label: t('cumulative'), value: 'cumulative' }, { label: t('recurring'), value: 'recurring' }]" /></el-form-item>
        <el-form-item :label="t('warningPercent')"><el-input v-model.number="policy.warning_percent" inputmode="numeric" class="field-full" @blur="policy.warning_percent = clampNumber(policy.warning_percent, 1, 99, 80)"><template #suffix>%</template></el-input></el-form-item>
        <el-form-item :label="t('quotaBytes')">
          <div class="compound-field">
            <el-input
              :model-value="quotaDisplay(index, policy.quota_bytes)"
              inputmode="decimal"
              @update:model-value="onQuotaInput(index, $event)"
              @blur="commitQuota(index, policy)"
            />
            <el-select :model-value="quotaUnits[index] ?? 'GB'" @change="onQuotaUnitChange(index, $event as QuotaUnit)">
              <el-option v-for="unit in QUOTA_UNITS" :key="unit" :label="unit" :value="unit" />
            </el-select>
          </div>
        </el-form-item>
        <el-form-item v-if="policy.mode === 'recurring'" :label="t('cycle')"><div class="compound-field"><el-input v-model.number="policy.cycle_interval" inputmode="numeric" @blur="policy.cycle_interval = clampNumber(policy.cycle_interval, 1, undefined, 1)" /><el-select v-model="policy.cycle_unit"><el-option :label="t('hour')" value="hour"/><el-option :label="t('day')" value="day"/><el-option :label="t('week')" value="week"/><el-option :label="t('month')" value="month"/><el-option :label="t('year')" value="year"/></el-select></div></el-form-item>
        <el-form-item v-if="policy.mode === 'recurring'" class="span-2" :label="t('cycleStart')" required><el-date-picker v-model="policy.cycle_start" type="datetime" value-format="YYYY-MM-DDTHH:mm:ssZ" :clearable="false" class="field-full" /></el-form-item>
      </div>
    </section>
  </div>
  <el-empty v-else :description="t('noTrafficPolicies')" :image-size="56" />
</template>
