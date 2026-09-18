<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getPublicServerAvailability, type PublicAvailability } from '@santaizi/api'
import type { ServerStatusView } from '../domain/serverStatusView'
import { formatAvailabilityPercent } from '../domain/transfer'
import { formatTimestamp, formatUptime } from '../domain/serverStatusView'
import {
  billingText,
  cycleStatusLabel,
  planTagLabel,
  remainingText,
  trafficQuotaText,
} from '../domain/publicNoteDisplay'
import LatencyChart from './LatencyChart.vue'
import ServerResourceHistory from './ServerResourceHistory.vue'

const availabilityCache = new Map<number, PublicAvailability>()

const props = defineProps<{
  server: ServerStatusView
  showAvailability: boolean
}>()

const { t, te, locale } = useI18n()
const availability = ref<PublicAvailability | null>(null)

function translate(key: string, values?: Record<string, unknown>) {
  return values ? String(t(key, values)) : String(t(key))
}

const billing = computed(() => billingText(props.server.publicNote, translate, te))
const remaining = computed(() => remainingText(props.server.publicNote, translate))
const quota = computed(() => trafficQuotaText(props.server.publicNote, translate))
const platformText = computed(() => {
  const version = props.server.platformVersion
  const label = props.server.platformLabel || props.server.platform
  if (!label) return ''
  return version ? `${label} ${version}` : label
})
const cpuText = computed(() => {
  const models = props.server.cpuModels.join(', ')
  const cores = props.server.cpuCoreCount
    ? `${props.server.cpuCoreCount} ${t('cores')}`
    : ''
  const usage = `${props.server.cpu.percent.toFixed(1)}%`
  return [models, cores, usage].filter(Boolean).join(' · ')
})
const memText = computed(() => {
  const metric = props.server.memory
  if (metric.total <= 0) return ''
  return `${metric.usedLabel} / ${metric.totalLabel} (${metric.percent.toFixed(1)}%)`
})
const diskText = computed(() => {
  const metric = props.server.disk
  if (metric.total <= 0) return ''
  return `${metric.usedLabel} / ${metric.totalLabel} (${metric.percent.toFixed(1)}%)`
})
const swapText = computed(() => {
  if (!props.server.swap) return t('swapOff')
  return `${props.server.swap.usedLabel} / ${props.server.swap.totalLabel}`
})
const bootLabel = computed(() => formatTimestamp(props.server.bootTime, locale.value))
const availabilityRate = computed(() => formatAvailabilityPercent(availability.value?.availability_percent))
const offlineCount = computed(() => availability.value?.offline_count ?? 0)
const longestOffline = computed(() => {
  const seconds = availability.value?.longest_offline_seconds ?? 0
  return seconds > 0 ? formatUptime(seconds, locale.value) : '0'
})

function cycleDetail(cycle: ServerStatusView['cycles'][number]) {
  return [
    `${cycle.usedLabel} / ${cycle.quotaLabel}`,
    cycle.remainingLabel ? `${t('remaining')} ${cycle.remainingLabel}` : '',
  ].filter(Boolean).join(' · ')
}

async function loadAvailability(id: number) {
  if (!props.showAvailability) {
    availability.value = null
    return
  }
  const cached = availabilityCache.get(id)
  if (cached) {
    availability.value = cached
    return
  }
  try {
    const result = await getPublicServerAvailability(id, { days: 30 })
    availabilityCache.set(id, result)
    if (props.server.id === id) availability.value = result
  } catch {
    if (props.server.id === id) availability.value = null
  }
}

watch(() => props.server.id, (id) => {
  availability.value = props.showAvailability ? (availabilityCache.get(id) ?? null) : null
  void loadAvailability(id)
}, { immediate: true })
</script>

<template>
  <div class="ss-detail">
    <div class="ss-detail__head">
      <div class="ss-detail__ident">
        <span class="status-dot" :class="server.online ? 'online' : 'offline'"></span>
        <span
          v-if="server.flagCode"
          class="server-flag"
          :class="`fi fi-${server.flagCode}`"
          aria-hidden="true"
        />
        <span v-else class="server-flag server-flag--empty" aria-hidden="true"><i class="ri-global-line"></i></span>
        <span>{{ server.location || '—' }}</span>
        <span v-if="server.group" class="ss-chip">{{ server.group }}</span>
      </div>

      <div v-if="server.slogan || server.publicNote.hasBillMeta || server.publicNote.hasPlanMeta || server.publicNote.hasBuy" class="ss-detail__tags">
        <span v-if="server.slogan" class="meta-tag">{{ server.slogan }}</span>
        <span v-if="billing" class="meta-tag meta-tag--billing">{{ billing }}</span>
        <span
          v-if="remaining"
          class="meta-tag"
          :class="{
            'meta-tag--success': server.publicNote.bill.remainingTone === 'success',
            'meta-tag--warning': server.publicNote.bill.remainingTone === 'warning',
            'meta-tag--danger': server.publicNote.bill.remainingTone === 'danger',
          }"
        >{{ remaining }}</span>
        <span v-if="server.publicNote.bill.bandwidth" class="meta-tag">{{ server.publicNote.bill.bandwidth }}</span>
        <span v-if="quota" class="meta-tag">{{ quota }}</span>
        <span v-for="tag in server.publicNote.planTags" :key="`${server.id}-${tag}`" class="meta-tag meta-tag--plan">{{ planTagLabel(tag, translate) }}</span>
        <a
          v-if="server.publicNote.hasBuy"
          class="buy-link"
          :href="server.orderLink"
          target="_blank"
          rel="noopener noreferrer"
          @click.stop
        >
          <i :class="server.publicNote.presentation.buyBtnIcon || 'ri-shopping-bag-3-line'"></i>
          {{ server.publicNote.presentation.buyBtnText || t('purchase') }}
        </a>
      </div>
    </div>

    <dl class="ss-detail__grid">
      <div v-if="platformText"><dt>{{ t('platform') }}</dt><dd>{{ platformText }}{{ server.arch ? ` [${server.arch}]` : '' }}</dd></div>
      <div v-if="server.virtualization"><dt>{{ t('virtualization') }}</dt><dd>{{ server.virtualization }}</dd></div>
      <div v-if="cpuText" class="is-span"><dt>{{ t('cpu') }}</dt><dd>{{ cpuText }}</dd></div>
      <div v-if="memText"><dt>{{ t('memory') }}</dt><dd>{{ memText }}</dd></div>
      <div v-if="diskText"><dt>{{ t('disk') }}</dt><dd>{{ diskText }}</dd></div>
      <div><dt>{{ t('metric_swap') }}</dt><dd>{{ swapText }}</dd></div>
      <div v-if="server.transferIn || server.transferOut" class="is-span">
        <dt>{{ t('traffic') }}</dt>
        <dd>{{ t('inboundTraffic') }} {{ server.transferInLabel }} / {{ t('outboundTraffic') }} {{ server.transferOutLabel }}</dd>
      </div>
      <div v-if="server.hasLoad"><dt>{{ t('load') }}</dt><dd>{{ server.load1.toFixed(2) }} / {{ server.load5.toFixed(2) }} / {{ server.load15.toFixed(2) }}</dd></div>
      <div v-if="server.processes !== null"><dt>{{ t('metric_process_count') }}</dt><dd>{{ server.processes }}</dd></div>
      <div v-if="server.tcp !== null || server.udp !== null">
        <dt>{{ t('connCount') }}</dt>
        <dd>TCP {{ server.tcp ?? 0 }} / UDP {{ server.udp ?? 0 }}</dd>
      </div>
      <div v-if="bootLabel"><dt>{{ t('bootTime') }}</dt><dd>{{ bootLabel }}</dd></div>
      <div v-if="server.lastActiveLabel"><dt>{{ t('lastActive') }}</dt><dd>{{ server.lastActiveLabel }}</dd></div>
      <div v-if="server.uptimeLabel"><dt>{{ t('uptime') }}</dt><dd>{{ server.uptimeLabel }}</dd></div>
      <div v-if="server.agentVersion"><dt>{{ t('version') }}</dt><dd>{{ server.agentVersion }}</dd></div>
      <div v-if="server.gpuNames.length" class="is-span"><dt>{{ t('metric_gpu') }}</dt><dd>{{ server.gpuNames.join(', ') }}</dd></div>
      <div v-if="server.temperatures.length" class="is-span">
        <dt>{{ t('capability_temperature') }}</dt>
        <dd>{{ server.temperatures.map((row) => `${row.name || t('metric_temperature_max')} ${row.value.toFixed(1)}°C`).join(', ') }}</dd>
      </div>
    </dl>

    <section v-if="server.cycles.length" class="ss-detail__section">
      <h3>{{ t('cycleTransfer') }}</h3>
      <div class="ss-detail__cycles">
        <div v-for="cycle in server.cycles" :key="`${server.id}-${cycle.policyId}-${cycle.name}`" class="ss-cycle">
          <span class="ss-cycle__name">{{ cycle.name || t('cycleTransfer') }}</span>
          <span
            class="ss-metric-bar"
            role="meter"
            :aria-label="cycle.name || t('cycleTransfer')"
            :aria-valuenow="Number(cycle.usagePercent.toFixed(1))"
            aria-valuemin="0"
            aria-valuemax="100"
          >
            <span class="ss-metric-bar__fill" :style="{ width: `${Math.max(0, Math.min(100, cycle.usagePercent))}%` }"></span>
            <span class="ss-metric-bar__label">{{ cycle.usagePercent.toFixed(1) }}%</span>
          </span>
          <span class="ss-cycle__detail">{{ cycleDetail(cycle) }}</span>
          <span class="ss-cycle__status" :class="`ss-traffic--${cycle.statusLevel}`">{{ cycleStatusLabel(cycle.statusLevel, translate) }}</span>
        </div>
      </div>
    </section>

    <section v-if="showAvailability && availability" class="ss-detail__section">
      <h3>{{ t('availability') }}</h3>
      <dl class="ss-detail__availability">
        <div><dt>{{ t('availability30d') }}</dt><dd>{{ availabilityRate || '—' }}</dd></div>
        <div><dt>{{ t('offlineCount') }}</dt><dd>{{ offlineCount }}</dd></div>
        <div><dt>{{ t('longestOffline') }}</dt><dd>{{ longestOffline }}</dd></div>
      </dl>
    </section>

    <ServerResourceHistory :server="server" />

    <LatencyChart :server-id="server.id" />
  </div>
</template>
