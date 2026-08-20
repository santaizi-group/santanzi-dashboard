<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { listPublicCycleTransfer, type CycleTransfer } from '@santaizi/api'
import { useInjectedStatusStore } from '@santaizi/status-core'
import { AppEmpty } from '@santaizi/ui'
import NazhuaSearch from '../components/layout/NazhuaSearch.vue'
import FilterBar from '../components/home/FilterBar.vue'
import WorldMap from '../components/home/WorldMap.vue'
import ServerCard from '../components/home/ServerCard.vue'
import ServerStatusList from '../components/home/ServerStatusList.vue'
import { useServerListFilters } from '../composables/useServerListFilters'
import { mapCycleTransfers, toNazhuaServerViews } from '../domain/nazhuaServerView'

const { t } = useI18n()
const store = useInjectedStatusStore()
const viewportWidth = ref(window.innerWidth)
const cycleRows = ref<CycleTransfer[]>([])

const {
  listMode,
  tagFilter,
  onlineFilter,
  searchWord,
  sortProp,
  sortOrder,
  groups,
  serverCount,
  filteredServers,
  mapLocations,
  setListMode,
  setSortProp,
  setSortOrder,
} = useServerListFilters(() => store.servers)

const cycleMap = computed(() => mapCycleTransfers(cycleRows.value))
const serverViews = computed(() => toNazhuaServerViews(filteredServers.value, cycleMap.value))
const showOnlineFilter = computed(() => serverCount.value.online !== serverCount.value.total)
const effectiveListMode = computed(() => (viewportWidth.value > 1024 ? listMode.value : 'card'))
const showMap = computed(() => serverViews.value.length > 0 && mapLocations.value.length > 0)
const worldMapWidth = computed(() => {
  const width = viewportWidth.value
  const listWidth = width > 1440
    ? 1300
    : width > 1280
      ? 1120
      : width > 1024
        ? 1024
        : width > 800
          ? 800
          : width > 720
            ? 720
            : width
  return Math.max(listWidth - 40, 300)
})

async function loadCycleTransfers() {
  try {
    const result = await listPublicCycleTransfer()
    cycleRows.value = result.data || []
  } catch {
    cycleRows.value = []
  }
}

async function retry() {
  await store.load()
  if (!store.loadError) await loadCycleTransfers()
}

function updateViewport() {
  viewportWidth.value = window.innerWidth
}

onMounted(() => {
  loadCycleTransfers()
  window.addEventListener('resize', updateViewport)
})

onBeforeUnmount(() => window.removeEventListener('resize', updateViewport))
</script>

<template>
  <div class="nazhua-home">
    <NazhuaSearch v-model="searchWord" :servers="store.servers" />
    <div class="nazhua-home__body">
      <WorldMap v-if="showMap" :locations="mapLocations" :width="worldMapWidth" />
      <FilterBar
        :groups="groups"
        :tag-filter="tagFilter"
        :online-filter="onlineFilter"
        :list-mode="listMode"
        :sort-prop="sortProp"
        :sort-order="sortOrder"
        :show-online-filter="showOnlineFilter"
        @update:tag-filter="tagFilter = $event"
        @update:online-filter="onlineFilter = $event"
        @update:list-mode="setListMode($event)"
        @update:sort-prop="setSortProp($event)"
        @update:sort-order="setSortOrder($event)"
      />
      <div v-if="serverViews.length" class="nazhua-home__list" :class="`mode-${effectiveListMode}`">
        <template v-if="effectiveListMode === 'card'">
          <ServerCard v-for="server in serverViews" :key="server.id" :server="server" />
        </template>
        <ServerStatusList v-else :servers="serverViews" />
      </div>
      <div v-else class="nazhua-empty">
        <AppEmpty
          :tone="store.loadError ? 'danger' : 'default'"
          :icon="store.loadError ? 'ri-error-warning-line' : 'ri-server-line'"
          :title="store.loadError ? t('nazhua.loadFailed') : ''"
          :description="t(store.loadError ? 'nazhua.requestFailed' : store.loading ? 'nazhua.loading' : 'nazhua.noData')"
        />
        <el-button v-if="store.loadError" type="primary" @click="retry"><i class="ri-refresh-line"></i>{{ t('nazhua.refresh') }}</el-button>
      </div>
    </div>
  </div>
</template>
