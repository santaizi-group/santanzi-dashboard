<script setup lang="ts">
import { toRef } from 'vue'
import { useI18n } from 'vue-i18n'
import { AppEmpty } from '@santaizi/ui'
import { NETWORK_RANGES, useNetworkMonitor } from '../composables/useNetworkMonitor'
import MonitorLineChart from './MonitorLineChart.vue'

const props = defineProps<{ serverId: number; hideHeading?: boolean }>()
const { t } = useI18n()
const {
  loading,
  failed,
  empty,
  aggregated,
  autoRefresh,
  cutPeak,
  hours,
  series,
  load,
} = useNetworkMonitor(toRef(props, 'serverId'))
</script>

<template>
  <section class="ss-latency">
    <header class="ss-latency__head">
      <h3 v-if="!hideHeading">{{ t('nazhua.networkMonitor') }}</h3>
      <div class="ss-latency__toolbar">
        <div class="ss-latency__toggles">
          <label class="ss-latency__switch">
            <span>{{ t('nazhua.aggregate') }}</span>
            <el-switch v-model="aggregated" size="small" />
          </label>
          <label class="ss-latency__switch">
            <span>{{ t('nazhua.autoRefresh') }}</span>
            <el-switch v-model="autoRefresh" size="small" />
          </label>
          <label class="ss-latency__switch">
            <span>{{ t('nazhua.cutPeak') }}</span>
            <el-switch v-model="cutPeak" size="small" />
          </label>
        </div>
        <div class="ss-latency__ranges" role="group" :aria-label="t('nazhua.recent')">
          <span>{{ t('nazhua.recent') }}</span>
          <el-button-group>
            <el-button
              v-for="item in NETWORK_RANGES"
              :key="item.hours"
              :type="hours === item.hours ? 'primary' : 'default'"
              @click="hours = item.hours"
            >{{ t(item.labelKey) }}</el-button>
          </el-button-group>
        </div>
      </div>
    </header>
    <div v-if="failed || empty || loading" class="ss-latency__empty">
      <AppEmpty
        :tone="failed ? 'danger' : 'default'"
        :icon="failed ? 'ri-error-warning-line' : loading ? 'ri-loader-4-line' : 'ri-line-chart-line'"
        :title="failed ? t('loadFailed') : ''"
        :description="t(failed ? 'requestFailed' : loading ? 'loading' : 'noData')"
      />
      <button v-if="failed" type="button" @click="load()">
        <i class="ri-refresh-line"></i>{{ t('refresh') }}
      </button>
    </div>
    <MonitorLineChart v-else-if="aggregated" :series="series" />
    <div v-else class="ss-latency__grid">
      <article v-for="item in series" :key="item.name" class="ss-latency__card">
        <header>
          <strong>{{ item.name }}</strong>
          <span>{{ item.average.toFixed(2) }} ms</span>
        </header>
        <MonitorLineChart :series="[item]" compact lazy />
      </article>
    </div>
  </section>
</template>
