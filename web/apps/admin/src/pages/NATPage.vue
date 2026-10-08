<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { AppEmpty } from '@santaizi/ui'
import NATEditorDialog from '@/components/editors/NATEditorDialog.vue'
import { deleteNATTunnel, listAllServers, listNATTunnels, type ServerRecord } from '@/api/adminApi'
import { notifyAPIError } from '@/composables/notify'
import { PAGE_SIZES, readStoredPageSize, writeStoredPageSize } from '@/composables/pageSize'
import { isRowSelected, toggleRowSelection } from '@/composables/selection'
import type { NATTunnelRecord } from '@/types/admin'

const { t, te } = useI18n()
const route = useRoute()
const loading = ref(false), editor = ref(false), total = ref(0)
const items = ref<NATTunnelRecord[]>([]), selected = ref<NATTunnelRecord[]>([]), editing = ref<NATTunnelRecord>(), servers = ref<ServerRecord[]>([])
const serverNames = computed(() => Object.fromEntries(servers.value.map(server => [server.id, server.name])))
const query = reactive({ page: 1, page_size: readStoredPageSize(route.path), q: '', sort: 'id', order: 'desc' as const })
async function load() { writeStoredPageSize(route.path, query.page_size); loading.value = true; try { const [result, serverResult] = await Promise.all([listNATTunnels(query), listAllServers()]); items.value = result.data; total.value = result.meta.total || result.data.length; servers.value = serverResult.data } catch (error) { notifyAPIError(error, t as never, te) } finally { loading.value = false } }
function open(item?: NATTunnelRecord) { editing.value = item; editor.value = true }
async function remove(rows: NATTunnelRecord[]) { await ElMessageBox.confirm(t('confirmDelete'), t('dangerousAction'), { type: 'warning' }); try { await Promise.all(rows.map(row => deleteNATTunnel(row.id))); selected.value = []; await load(); ElMessage.success(t('deleteSuccess')) } catch (error) { notifyAPIError(error, t as never, te) } }
function onSelect(row: NATTunnelRecord, checked: boolean | string | number) { selected.value = toggleRowSelection(selected.value, row, !!checked) }
onMounted(load)
</script>
<template>
  <div class="page-head"><h1>{{ t('nat') }}</h1><el-button type="primary" @click="open()"><i class="ri-add-line"></i>{{ t('createNATTunnel') }}</el-button></div>
  <section class="surface table-card"><div class="toolbar"><el-input v-model="query.q" class="search-input" clearable :placeholder="t('search')" @keyup.enter="query.page=1;load()"><template #prefix><i class="ri-search-line"></i></template></el-input><el-button @click="query.page=1;load()"><i class="ri-search-line"></i>{{ t('submitSearch') }}</el-button><el-button v-if="selected.length" type="danger" plain @click="remove(selected)"><i class="ri-delete-bin-6-line"></i>{{ t('batchDelete') }}</el-button><span class="toolbar-spacer"></span><el-button @click="load"><i class="ri-refresh-line"></i>{{ t('refresh') }}</el-button></div>
    <el-table class="desktop-only" v-loading="loading" :data="items" row-key="id" @selection-change="selected=$event"><el-table-column type="selection" width="46"/><el-table-column prop="name" :label="t('name')" min-width="160"/><el-table-column :label="t('server')" min-width="160"><template #default="{row}">{{ serverNames[row.server_id] || t('unknown') }}</template></el-table-column><el-table-column prop="target" :label="t('localService')" min-width="220"/><el-table-column prop="domain" :label="t('bindDomain')" min-width="220"/><el-table-column :label="t('actions')" width="72" fixed="right"><template #default="{row}"><el-dropdown trigger="click"><el-button text class="actions-more" :aria-label="t('actions')"><i class="ri-more-fill"></i></el-button><template #dropdown><el-dropdown-menu><el-dropdown-item @click="open(row)"><i class="ri-edit-line"></i>{{ t('edit') }}</el-dropdown-item><el-dropdown-item divided @click="remove([row])"><i class="ri-delete-bin-6-line"></i>{{ t('delete') }}</el-dropdown-item></el-dropdown-menu></template></el-dropdown></template></el-table-column><template #empty><AppEmpty icon="ri-route-line" :description="t('noData')"/></template></el-table>
    <div class="mobile-only" v-loading="loading">
      <AppEmpty v-if="!items.length && !loading" icon="ri-route-line" :description="t('noData')"/>
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
                    <el-dropdown-item @click="open(row)"><i class="ri-edit-line"></i>{{ t('edit') }}</el-dropdown-item>
                    <el-dropdown-item divided @click="remove([row])"><i class="ri-delete-bin-6-line"></i>{{ t('delete') }}</el-dropdown-item>
                  </el-dropdown-menu>
                </template>
              </el-dropdown>
            </div>
          </div>
          <dl class="mobile-card-meta">
            <div><dt>{{ t('server') }}</dt><dd>{{ serverNames[row.server_id] || t('unknown') }}</dd></div>
            <div><dt>{{ t('localService') }}</dt><dd>{{ row.target }}</dd></div>
            <div><dt>{{ t('bindDomain') }}</dt><dd>{{ row.domain }}</dd></div>
          </dl>
        </article>
      </div>
    </div>
    <div class="pagination"><el-pagination v-model:current-page="query.page" v-model:page-size="query.page_size" :page-sizes="PAGE_SIZES" layout="total, sizes, prev, pager, next" :total="total" @change="load"/></div></section>
  <NATEditorDialog v-model="editor" :value="editing" @saved="load"/>
</template>
