<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { formatConnPair, type ServerStatusView } from '../domain/serverStatusView'
import type { StatusTableColumns } from '../domain/statusTableColumns'
import { usageTone } from '../domain/transfer'
import { billingText, remainingText, trafficUsageText } from '../domain/publicNoteDisplay'
import OsLogo from './OsLogo.vue'

const props = defineProps<{
  server: ServerStatusView
  columns: StatusTableColumns
  showGroup?: boolean
}>()

const emit = defineEmits<{ select: [] }>()
const { t, te } = useI18n()

function translate(key: string, values?: Record<string, unknown>) {
  return values ? String(t(key, values)) : String(t(key))
}

const price = computed(() => billingText(props.server.publicNote, translate, te))
const remaining = computed(() => remainingText(props.server.publicNote, translate))
const traffic = computed(() => trafficUsageText(props.server.trafficUsage.kind, props.server.trafficUsage.valueLabel, translate))
const availabilityLabel = computed(() => {
  if (props.server.available === true) return t('available')
  if (props.server.available === false) return t('unavailable')
  return t('unknown')
})
const speed = computed(() => `${props.server.speedInLabel.replace(/\/s$/, '')} | ${props.server.speedOutLabel.replace(/\/s$/, '')}`)
const conn = computed(() => formatConnPair(props.server.tcp, props.server.udp) || '—')
</script>

<template>
  <div class="ss-row" :class="{ 'is-offline': !server.online }">
    <button
      type="button"
      class="ss-row__main"
      aria-haspopup="dialog"
      :aria-label="t('serverDetail')"
      @click="emit('select')"
    >
      <span class="ss-cell ss-cell--status" :data-label="t('status')">
        <span class="status-dot" :class="server.online ? 'online' : 'offline'"></span>
      </span>
      <span class="ss-cell ss-cell--name" :data-label="t('name')">
        <strong>{{ server.name }}</strong>
        <span v-if="showGroup !== false && server.group" class="ss-chip">{{ server.group }}</span>
      </span>
      <span class="ss-cell ss-cell--os" :data-label="t('platform')">
        <OsLogo :platform="server.platform" />
        <span>{{ [server.platformLabel || server.platform, server.platformVersion].filter(Boolean).join(' ') || '—' }}</span>
      </span>
      <span v-if="columns.location" class="ss-cell ss-cell--loc" :data-label="t('location')">
        <span
          v-if="server.flagCode"
          class="server-flag"
          :class="`fi fi-${server.flagCode}`"
          aria-hidden="true"
        />
        <span v-else class="server-flag server-flag--empty" aria-hidden="true"><i class="ri-global-line"></i></span>
        <span class="ss-chip ss-chip--muted">{{ server.location || '—' }}</span>
      </span>
      <span v-if="columns.price" class="ss-cell" :data-label="t('price')">{{ price || '—' }}</span>
      <span class="ss-cell" :data-label="t('online')">{{ server.uptimeLabel || '—' }}</span>
      <span v-if="columns.availability" class="ss-cell" :data-label="t('availability')">{{ availabilityLabel }}</span>
      <span class="ss-cell" :data-label="t('load')">{{ server.hasLoad ? server.load1.toFixed(2) : '—' }}</span>
      <span class="ss-cell ss-cell--num" :data-label="t('connCount')">{{ conn }}</span>
      <span class="ss-cell ss-cell--num" :data-label="t('networkSpeed')">{{ speed }}</span>
      <span
        class="ss-cell ss-cell--traffic"
        :class="server.trafficUsage.statusLevel ? `ss-traffic--${server.trafficUsage.statusLevel}` : ''"
        :data-label="t('traffic')"
      >{{ traffic }}</span>
      <span class="ss-cell" :data-label="t('cores')">
        <span class="ss-usage" :class="`ss-usage--${usageTone(server.cpu.percent)}`">{{ server.cpu.percent.toFixed(1) }}%</span>
      </span>
      <span class="ss-cell" :data-label="t('memory')">
        <span class="ss-usage" :class="`ss-usage--${usageTone(server.memory.percent)}`">{{ server.memory.percent.toFixed(1) }}%</span>
      </span>
      <span class="ss-cell" :data-label="t('disk')">
        <span class="ss-usage" :class="`ss-usage--${usageTone(server.disk.percent)}`">{{ server.disk.percent.toFixed(1) }}%</span>
      </span>
      <span
        v-if="columns.remaining"
        class="ss-cell"
        :class="{
          'meta-tag--success': server.publicNote.bill.remainingTone === 'success',
          'meta-tag--warning': server.publicNote.bill.remainingTone === 'warning',
          'meta-tag--danger': server.publicNote.bill.remainingTone === 'danger',
        }"
        :data-label="t('remaining')"
      >{{ remaining || '—' }}</span>
    </button>
  </div>
</template>
