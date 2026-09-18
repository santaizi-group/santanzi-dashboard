import { computed, ref } from 'vue'
import {
  SERVER_SORT_OPTIONS,
  resolveServerSortProp,
  type ServerSortOrder,
  type ServerSortProp,
} from '@santaizi/status-core'

const SORT_PROP_KEY = 'santaizi-status-sort-prop'
const SORT_ORDER_KEY = 'santaizi-status-sort-order'

function readStoredSortProp(): ServerSortProp {
  return resolveServerSortProp(localStorage.getItem(SORT_PROP_KEY))
}

function readStoredSortOrder(): ServerSortOrder {
  return localStorage.getItem(SORT_ORDER_KEY) === 'asc' ? 'asc' : 'desc'
}

export function useStatusSort() {
  const sortProp = ref<ServerSortProp>(readStoredSortProp())
  const sortOrder = ref<ServerSortOrder>(readStoredSortOrder())
  const sortOption = computed(() => SERVER_SORT_OPTIONS.find((option) => option.prop === sortProp.value))

  function setSortProp(prop: ServerSortProp) {
    sortProp.value = resolveServerSortProp(prop)
    localStorage.setItem(SORT_PROP_KEY, sortProp.value)
  }

  function setSortOrder(order: ServerSortOrder) {
    sortOrder.value = order === 'asc' ? 'asc' : 'desc'
    localStorage.setItem(SORT_ORDER_KEY, sortOrder.value)
  }

  function toggleSort(prop: ServerSortProp) {
    if (sortProp.value === prop) {
      setSortOrder(sortOrder.value === 'asc' ? 'desc' : 'asc')
      return
    }
    setSortProp(prop)
    setSortOrder('desc')
  }

  function toggleOrder() {
    setSortOrder(sortOrder.value === 'asc' ? 'desc' : 'asc')
  }

  return {
    sortProp,
    sortOrder,
    sortOption,
    setSortProp,
    setSortOrder,
    toggleSort,
    toggleOrder,
  }
}
