export interface ObserverSight {
  seen?: boolean
  observer_kind?: string
  observer_name?: string
  observer_id?: string
}

export function missedObserverText(items: ObserverSight[] | undefined, label: (item: ObserverSight) => string): string {
  const names = (items || [])
    .filter(item => item.seen === false)
    .map(item => label(item).trim())
    .filter(name => name && name !== '—')
  return names.join(', ')
}
