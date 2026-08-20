<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { SORT_OPTIONS, SORT_OPTION_COLUMNS, type ListMode, type SortOrder, type SortProp } from '../../composables/useServerListFilters'

const emit = defineEmits<{
  'update:tagFilter': [value: string]
  'update:onlineFilter': [value: 'all' | 'online' | 'offline']
  'update:listMode': [value: ListMode]
  'update:sortProp': [value: SortProp]
  'update:sortOrder': [value: SortOrder]
}>()

const { t } = useI18n()
const props = defineProps<{
  groups: Array<{ name: string; count: number }>
  tagFilter: string
  onlineFilter: 'all' | 'online' | 'offline'
  listMode: ListMode
  sortProp: SortProp
  sortOrder: SortOrder
  showOnlineFilter: boolean
}>()

const sortLabel = computed(() => {
  const option = SORT_OPTIONS.find(item => item.prop === props.sortProp)
  return t(option?.labelKey || 'nazhua.sortWeight')
})

function toggleGroup(name: string) {
  emit('update:tagFilter', props.tagFilter === name ? '' : name)
}

function toggleOnline(value: 'online' | 'offline') {
  emit('update:onlineFilter', props.onlineFilter === value ? 'all' : value)
}
</script>

<template>
  <div class="nazhua-filter">
    <div class="nazhua-filter__groups" :aria-label="t('nazhua.group')">
      <el-button v-for="group in groups" :key="group.name" :type="tagFilter === group.name ? 'primary' : 'default'" :title="`${group.count}`" @click="toggleGroup(group.name)">
        {{ group.name }}
      </el-button>
    </div>
    <div class="nazhua-filter__tools">
      <el-dropdown trigger="click" popper-class="nazhua-sort-menu" @command="emit('update:sortProp', $event as SortProp)">
        <el-button class="nazhua-filter__sort" :aria-label="t('nazhua.sort')">
          <span>{{ sortLabel }}</span>
          <span
            class="nazhua-filter__sort-order"
            role="button"
            tabindex="0"
            :aria-label="t('nazhua.sort')"
            @click.stop="emit('update:sortOrder', sortOrder === 'asc' ? 'desc' : 'asc')"
            @keydown.enter.stop="emit('update:sortOrder', sortOrder === 'asc' ? 'desc' : 'asc')"
            @keydown.space.prevent.stop="emit('update:sortOrder', sortOrder === 'asc' ? 'desc' : 'asc')"
          ><i :class="sortOrder === 'asc' ? 'ri-arrow-up-line' : 'ri-arrow-down-line'"></i></span>
        </el-button>
        <template #dropdown>
          <el-dropdown-menu>
            <div v-for="column in SORT_OPTION_COLUMNS" :key="column.group" class="nazhua-sort-menu__column" role="none">
              <el-dropdown-item
                v-for="option in column.options"
                :key="option.prop"
                :command="option.prop"
                :class="{ 'is-current': option.prop === sortProp }"
              >{{ t(option.labelKey) }}</el-dropdown-item>
            </div>
          </el-dropdown-menu>
        </template>
      </el-dropdown>
      <div v-if="showOnlineFilter" class="nazhua-filter__online">
        <el-button :type="onlineFilter === 'online' ? 'primary' : 'default'" @click="toggleOnline('online')">{{ t('nazhua.online') }}</el-button>
        <el-button :type="onlineFilter === 'offline' ? 'primary' : 'default'" @click="toggleOnline('offline')">{{ t('nazhua.offline') }}</el-button>
      </div>
      <el-button-group class="nazhua-filter__modes">
        <el-button :type="listMode === 'card' ? 'primary' : 'default'" :aria-label="t('nazhua.modeCard')" @click="emit('update:listMode', 'card')"><i class="ri-gallery-view-2"></i></el-button>
        <el-button :type="listMode === 'server-status' ? 'primary' : 'default'" :aria-label="t('nazhua.modeServerStatus')" @click="emit('update:listMode', 'server-status')"><i class="ri-server-line"></i></el-button>
      </el-button-group>
    </div>
  </div>
</template>
