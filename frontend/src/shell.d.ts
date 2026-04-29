// Module Federation remote type declarations for shell-exposed modules.
// The actual implementations are resolved by Module Federation at runtime.

declare module 'shell/vben/stores' {
  import type { StoreDefinition } from 'pinia';
  export const useAccessStore: StoreDefinition;
}

declare module 'shell/vben/common-ui' {
  import type { Component } from 'vue';
  export const Page: Component;
  export function useVbenDrawer(options: any): [Component, any];
  export type VbenFormProps = any;
}

declare module 'shell/vben/icons' {
  import type { Component } from 'vue';
  export const LucideEye: Component;
  export const LucidePhoneCall: Component;
  export const LucideUsers: Component;
  export const LucideBarChart3: Component;
}

declare module 'shell/vben/layouts' {
  import type { Component } from 'vue';
  export const BasicLayout: Component;
}

declare module 'shell/app-layout' {
  import type { Component } from 'vue';
  const component: Component;
  export default component;
}

declare module 'shell/adapter/vxe-table' {
  export function useVbenVxeGrid(options: any): any;
  export type VxeGridProps<_T = unknown> = any;
}

declare module 'shell/locales' {
  export function $t(key: string, ...args: any[]): string;
}
