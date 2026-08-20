<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { CycleTransfer, ServerRecord } from '@santaizi/api'
import { listPublicCycleTransfer } from '@santaizi/api'
import { AppEmpty } from '@santaizi/ui'
import { formatCycleBytes } from '@santaizi/theme-server-status'
import { percentOf } from '../../domain/nazhuaServerView'
import { formatDateTime } from '../../utils/host'

const props = defineProps<{ server: ServerRecord }>()
const { t, locale } = useI18n()
const rows = ref<CycleTransfer[]>([])
const loading = ref(false)
const failed = ref(false)

async function load() {
  loading.value = true
  failed.value = false
  try {
    const result = await listPublicCycleTransfer(props.server.id)
    rows.value = result.data || []
  } catch {
    failed.value = true
    rows.value = []
  } finally {
    loading.value = false
  }
}

const hasRows = computed(() => rows.value.length > 0)
const visible = computed(() => failed.value || hasRows.value)

function statusKey(status?: string) {
  if (!status || status === 'normal' || status === 'ok') return 'ok'
  if (status === 'warning' || status === 'critical' || status === 'exceeded') return status
  return 'ok'
}

function usage(row: CycleTransfer) {
  const used = Number(row.used_bytes || 0)
  const quota = Number(row.quota_bytes || 0)
  return quota > 0 ? percentOf(used, quota) : Number(row.usage_percent || 0)
}

function remaining(row: CycleTransfer) {
  return Number(row.remaining_bytes ?? Math.max(Number(row.quota_bytes || 0) - Number(row.used_bytes || 0), 0))
}

function windowText(row: CycleTransfer) {
  return [formatDateTime(row.window_start, locale.value), formatDateTime(row.window_end, locale.value)].filter(Boolean).join(' ~ ')
}

onMounted(load)
watch(() => props.server.id, load)
</script>

<template>
  <section v-if="visible" class="nazhua-cycle-transfer">
    <header class="nazhua-cycle-transfer__head">
      <h2>{{ t('nazhua.cycleTransfer') }}</h2>
      <button type="button" :disabled="loading" @click="load"><i class="ri-refresh-line"></i>{{ t('nazhua.refresh') }}</button>
    </header>
    <div v-if="failed" class="nazhua-cycle-transfer__empty">
      <AppEmpty
        tone="danger"
        icon="ri-error-warning-line"
        :title="t('nazhua.loadFailed')"
        :description="t('nazhua.requestFailed')"
      />
      <button type="button" :disabled="loading" @click="load"><i class="ri-refresh-line"></i>{{ t('nazhua.refresh') }}</button>
    </div>
    <div v-else class="nazhua-cycle-transfer__list">
      <article v-for="row in rows" :key="row.policy_id" class="nazhua-cycle-transfer__item">
        <div class="nazhua-cycle-transfer__row">
          <strong>{{ row.name || t('nazhua.cycleTransfer') }}</strong>
          <span class="nazhua-cycle-transfer__status" :class="`is-${statusKey(row.status)}`">
            {{ t(`nazhua.cycleStatus.${statusKey(row.status)}`) }}
          </span>
          <div class="nazhua-cycle-transfer__bar">
            <div class="nazhua-cycle-transfer__fill" :class="`is-${statusKey(row.status)}`" :style="{ width: `${Math.min(100, usage(row))}%` }" />
            <i
              v-if="row.warning_percent && row.warning_percent > 0 && row.warning_percent < 100"
              class="nazhua-cycle-transfer__warn"
              :style="{ left: `${row.warning_percent}%` }"
            />
          </div>
          <p class="nazhua-cycle-transfer__usage">
            {{ formatCycleBytes(Number(row.used_bytes || 0)) }}
            /
            {{ formatCycleBytes(Number(row.quota_bytes || 0)) }}
            ({{ usage(row).toFixed(1) }}%)
            ·
            {{ t('nazhua.remainingBytes') }}
            {{ formatCycleBytes(remaining(row)) }}
          </p>
        </div>
        <p v-if="windowText(row) || row.next_reset_at" class="nazhua-cycle-transfer__meta">
          <template v-if="windowText(row)">{{ t('nazhua.windowRange') }} {{ windowText(row) }}</template>
          <template v-if="windowText(row) && row.next_reset_at"> · </template>
          <template v-if="row.next_reset_at">{{ t('nazhua.nextReset') }} {{ formatDateTime(row.next_reset_at, locale) }}</template>
        </p>
      </article>
    </div>
  </section>
</template>
