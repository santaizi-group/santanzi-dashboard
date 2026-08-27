<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { formatLatencyMs } from '@/composables/format'
import {
  formatProbeLoss,
  hopGeoLine,
  probeHopLossTone,
  probeHopRouteStats,
  probeHopRttShare,
  type ProbeHopView,
} from '@/domain/probePath'

const props = defineProps<{
  hops: ProbeHopView[]
  changed?: boolean
}>()

const { t, locale } = useI18n()
const stats = computed(() => probeHopRouteStats(props.hops))

function hopTitle(hop: ProbeHopView) {
  return [hop.address || '*', hop.hostname, hopGeoLine(hop, t('hopPrivate'))].filter(Boolean).join(' · ')
}

function geoText(hop: ProbeHopView) {
  return hopGeoLine(hop, t('hopPrivate'))
}

function isDestination(index: number) {
  return stats.value.reached && index === stats.value.lastIndex
}

function rttShare(hop: ProbeHopView) {
  return probeHopRttShare(hop.rttMs, stats.value.maxRttMs)
}
</script>

<template>
  <div class="probe-hop-route">
    <div class="probe-hop-summary">
      <span class="probe-hop-chip">{{ t('hopCount', { n: stats.hopCount }) }}</span>
      <span class="probe-hop-chip">{{ formatLatencyMs(stats.lastRttMs, locale) }}</span>
      <span class="probe-hop-chip" :class="probeHopLossTone(stats.lastLoss)">{{ formatProbeLoss(stats.lastLoss, locale) }}</span>
      <span class="probe-hop-chip" :class="stats.reached ? 'is-ok' : 'is-fail'">
        <i :class="stats.reached ? 'ri-flag-line' : 'ri-close-circle-line'" aria-hidden="true"></i>{{ stats.reached ? t('hopReached') : t('hopUnreached') }}
      </span>
      <span v-if="changed" class="probe-hop-chip is-warn">
        <i class="ri-route-line" aria-hidden="true"></i>{{ t('hopRouteChanged') }}
      </span>
    </div>
    <ol class="probe-hops">
      <li
        v-for="(hop, index) in hops"
        :key="`${hop.ttl}-${hop.address}-${index}`"
        class="probe-hop"
        :class="{
          'is-timeout': !hop.address,
          'is-destination': isDestination(index),
        }"
      >
        <span class="probe-hop__spine"><span class="probe-hop__ttl">{{ hop.ttl }}</span></span>
        <span class="probe-hop__addr" :title="hopTitle(hop)">
          <span class="probe-hop__ip">{{ hop.address || '*' }}</span>
          <span class="probe-hop__meta">
            <span v-if="hop.hostname" class="probe-hop__host">{{ hop.hostname }}</span>
            <span v-if="geoText(hop)" class="probe-hop__geo">{{ geoText(hop) }}</span>
            <span v-if="isDestination(index)" class="probe-hop__mark">
              <i class="ri-flag-line" aria-hidden="true"></i>{{ t('hopDestination') }}
            </span>
          </span>
        </span>
        <span class="probe-hop__metrics">
          <span class="probe-hop__rtt">{{ formatLatencyMs(hop.rttMs, locale) }}</span>
          <span class="probe-hop__rtt-track" aria-hidden="true">
            <span class="probe-hop__rtt-fill" :style="{ width: `${rttShare(hop)}%` }"></span>
          </span>
          <span class="probe-hop__loss" :class="probeHopLossTone(hop.loss)">{{ formatProbeLoss(hop.loss, locale) }}</span>
        </span>
      </li>
    </ol>
  </div>
</template>
