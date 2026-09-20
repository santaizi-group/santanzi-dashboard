/// <reference types="vite/client" />
/// <reference types="@types/geojson" />

declare module '*.vue' {
  import type { DefineComponent } from 'vue'
  const component: DefineComponent
  export default component
}

declare module '*.svg?url' {
  const url: string
  export default url
}

declare module '*.json?url' {
  const url: string
  export default url
}

declare module 'd3-geo' {
  export interface GeoProjection {
    (coordinates: [number, number]): [number, number] | null
    scale(value: number): GeoProjection
    translate(value: [number, number]): GeoProjection
    fitSize(size: [number, number], object: unknown): GeoProjection
    precision(value: number): GeoProjection
    rotate(value: [number, number] | [number, number, number]): GeoProjection
    clipAngle(value: number | null): GeoProjection
  }

  export interface GeoPath {
    (object: unknown): string | null
    context(context: CanvasRenderingContext2D | null): GeoPath
  }

  export function geoPath(projection?: unknown, context?: CanvasRenderingContext2D | null): GeoPath
  export function geoEquirectangular(): GeoProjection
  export function geoOrthographic(): GeoProjection
  export function geoGraticule(): () => unknown
  export function geoContains(object: unknown, point: [number, number]): boolean
}
