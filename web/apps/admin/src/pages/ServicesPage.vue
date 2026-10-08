<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { AppDrawer, AppEmpty } from '@santaizi/ui'
import MonitorEditorDialog from '@/components/editors/MonitorEditorDialog.vue'
import { deleteMonitor, listMonitorHistory, listMonitors, type ResourceRecord } from '@/api/adminApi'
import {formatAdminValue} from '@/composables/format'
import { notifyAPIError } from '@/composables/notify'
import { PAGE_SIZES, readStoredPageSize, writeStoredPageSize } from '@/composables/pageSize'
import { isRowSelected, toggleRowSelection } from '@/composables/selection'
import type { MonitorRecord } from '@/types/admin'

const { t, te, locale } = useI18n()
const route = useRoute()
const loading = ref(false), editor = ref(false), historyDrawer = ref(false), historyLoading = ref(false)
const items = ref<MonitorRecord[]>([]), selected = ref<MonitorRecord[]>([]), editing = ref<MonitorRecord>(), history = ref<ResourceRecord[]>([]), historyTitle = ref('')
const total = ref(0)
const query = reactive({ page: 1, page_size: readStoredPageSize(route.path), q: '', sort: 'id', order: 'desc' as const })
async function load() { writeStoredPageSize(route.path, query.page_size); loading.value = true; try { const result = await listMonitors(query); items.value = result.data; total.value = result.meta.total || result.data.length } catch (error) { notifyAPIError(error, t as never, te) } finally { loading.value = false } }
function open(item?: MonitorRecord) { editing.value = item; editor.value = true }
async function remove(itemsToDelete: MonitorRecord[]) { await ElMessageBox.confirm(t('confirmDelete'), t('dangerousAction'), { type: 'warning' }); try { await Promise.all(itemsToDelete.map(item => deleteMonitor(item.id))); selected.value = []; ElMessage.success(t('deleteSuccess')); await load() } catch (error) { notifyAPIError(error, t as never, te) } }
async function showHistory(item: MonitorRecord) { historyTitle.value = item.name; historyDrawer.value = true; historyLoading.value = true; try { history.value = (await listMonitorHistory(item.id)).data } catch (error) { notifyAPIError(error, t as never, te) } finally { historyLoading.value = false } }
function monitorType(value: MonitorRecord['type']) { return t(value === 'http' ? 'monitorHTTP' : value === 'icmp' ? 'monitorICMP' : 'monitorTCP') }
function display(value: unknown, key: string) { return formatAdminValue(value, key, locale.value, t as never, te) }
function onSelect(row: MonitorRecord, checked: boolean | string | number) { selected.value = toggleRowSelection(selected.value, row, !!checked) }
function scopeLabel(row: MonitorRecord) { return row.scope.mode === 'include' ? t('scopeSelectedServers') : row.scope.mode === 'exclude' ? t('scopeExceptSelected') : t('scopeAll') }
onMounted(load)
</script>

<template>
  <div class="page-head"><h1>{{ t('services') }}</h1><el-button type="primary" @click="open()"><i class="ri-add-line"></i>{{ t('createMonitor') }}</el-button></div>
  <section class="surface table-card">
    <div class="toolbar"><el-input v-model="query.q" class="search-input" clearable :placeholder="t('search')" @keyup.enter="query.page=1;load()"><template #prefix><i class="ri-search-line"></i></template></el-input><el-button @click="query.page=1;load()"><i class="ri-search-line"></i>{{ t('submitSearch') }}</el-button><el-button v-if="selected.length" type="danger" plain @click="remove(selected)"><i class="ri-delete-bin-6-line"></i>{{ t('batchDelete') }}</el-button><span class="toolbar-spacer"></span><el-button @click="load"><i class="ri-refresh-line"></i>{{ t('refresh') }}</el-button></div>
    <el-table class="desktop-only" v-loading="loading" :data="items" row-key="id" @selection-change="selected=$event"><el-table-column type="selection" width="46"/><el-table-column prop="name" :label="t('name')" min-width="160"/><el-table-column :label="t('monitorType')" width="130"><template #default="{row}"><el-tag effect="plain">{{ monitorType(row.type) }}</el-tag></template></el-table-column><el-table-column prop="target" :label="t('target')" min-width="230" show-overflow-tooltip/><el-table-column prop="interval_seconds" :label="t('intervalSeconds')" width="120"/><el-table-column :label="t('notificationGroup')" width="160"><template #default="{row}">{{ row.notify ? row.notification_tag : t('disabled') }}</template></el-table-column><el-table-column :label="t('serverScope')" width="150"><template #default="{row}">{{ scopeLabel(row) }}</template></el-table-column><el-table-column :label="t('actions')" width="72" fixed="right"><template #default="{row}"><el-dropdown trigger="click"><el-button text class="actions-more" :aria-label="t('actions')"><i class="ri-more-fill"></i></el-button><template #dropdown><el-dropdown-menu><el-dropdown-item @click="showHistory(row)"><i class="ri-line-chart-line"></i>{{ t('monitorHistory') }}</el-dropdown-item><el-dropdown-item @click="open(row)"><i class="ri-edit-line"></i>{{ t('edit') }}</el-dropdown-item><el-dropdown-item divided @click="remove([row])"><i class="ri-delete-bin-6-line"></i>{{ t('delete') }}</el-dropdown-item></el-dropdown-menu></template></el-dropdown></template></el-table-column><template #empty><AppEmpty icon="ri-pulse-line" :description="t('noData')"/></template></el-table>
    <div class="mobile-only" v-loading="loading">
      <AppEmpty v-if="!items.length && !loading" icon="ri-pulse-line" :description="t('noData')"/>
      <div v-else class="mobile-card-list">
        <article v-for="row in items" :key="row.id" class="mobile-card">
          <div class="mobile-card-head">
            <el-checkbox :model-value="isRowSelected(selected, row)" @change="onSelect(row, $event)" />
            <div class="mobile-card-title"><strong>{{ row.name }}</strong></div>
            <div class="mobile-card-actions">
              <el-dropdown trigger="click">
                <el-button text class="actions-more" :aria-label="t('actions')"><i class="ri-more-fill"></i></el-button>
                <template #dropdown>
                  <el-dropdown-menu>
                    <el-dropdown-item @click="showHistory(row)"><i class="ri-line-chart-line"></i>{{ t('monitorHistory') }}</el-dropdown-item>
                    <el-dropdown-item @click="open(row)"><i class="ri-edit-line"></i>{{ t('edit') }}</el-dropdown-item>
                    <el-dropdown-item divided @click="remove([row])"><i class="ri-delete-bin-6-line"></i>{{ t('delete') }}</el-dropdown-item>
                  </el-dropdown-menu>
                </template>
              </el-dropdown>
            </div>
          </div>
          <dl class="mobile-card-meta">
            <div><dt>{{ t('monitorType') }}</dt><dd><el-tag effect="plain">{{ monitorType(row.type) }}</el-tag></dd></div>
            <div><dt>{{ t('target') }}</dt><dd>{{ row.target }}</dd></div>
            <div><dt>{{ t('intervalSeconds') }}</dt><dd>{{ row.interval_seconds }}</dd></div>
            <div><dt>{{ t('notificationGroup') }}</dt><dd>{{ row.notify ? row.notification_tag : t('disabled') }}</dd></div>
            <div><dt>{{ t('serverScope') }}</dt><dd>{{ scopeLabel(row) }}</dd></div>
          </dl>
        </article>
      </div>
    </div>
    <div class="pagination"><el-pagination v-model:current-page="query.page" v-model:page-size="query.page_size" :page-sizes="PAGE_SIZES" layout="total, sizes, prev, pager, next" :total="total" @change="load"/></div>
  </section>
  <MonitorEditorDialog v-model="editor" :value="editing" @saved="load"/>
  <AppDrawer v-model="historyDrawer" :title="`${t('monitorHistory')} · ${historyTitle}`" mode="view" size="min(840px,96vw)"><el-table v-loading="historyLoading" :data="history"><el-table-column prop="created_at" :label="t('createdAt')" min-width="190"><template #default="{row}">{{ display(row.created_at,'created_at') }}</template></el-table-column><el-table-column prop="server_id" :label="t('server')" width="100"/><el-table-column prop="avg_delay" :label="t('averageLatency')" width="150"/><el-table-column prop="up" :label="t('upCount')" width="110"/><el-table-column prop="down" :label="t('downCount')" width="110"/></el-table></AppDrawer>
</template>
