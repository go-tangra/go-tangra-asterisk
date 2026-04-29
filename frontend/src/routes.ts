import type { RouteRecordRaw } from 'vue-router';

const routes: RouteRecordRaw[] = [
  {
    path: '/asterisk',
    name: 'Asterisk',
    component: () => import('shell/app-layout'),
    redirect: '/asterisk/calls',
    meta: {
      order: 480,
      icon: 'lucide:phone',
      title: 'asterisk.menu.asterisk',
      keepAlive: true,
      authority: ['platform:admin', 'tenant:manager'],
    },
    children: [
      {
        path: 'calls',
        name: 'AsteriskCalls',
        meta: {
          icon: 'lucide:phone-call',
          title: 'asterisk.menu.calls',
          authority: ['platform:admin', 'tenant:manager'],
        },
        component: () => import('./views/calls/index.vue'),
      },
      {
        path: 'overview',
        name: 'AsteriskOverview',
        meta: {
          icon: 'lucide:bar-chart-3',
          title: 'asterisk.menu.overview',
          authority: ['platform:admin', 'tenant:manager'],
        },
        component: () => import('./views/overview/index.vue'),
      },
      {
        path: 'extensions',
        name: 'AsteriskExtensions',
        meta: {
          icon: 'lucide:users',
          title: 'asterisk.menu.extensions',
          authority: ['platform:admin', 'tenant:manager'],
        },
        component: () => import('./views/extensions/index.vue'),
      },
      {
        path: 'dashboards',
        name: 'AsteriskDashboards',
        meta: {
          icon: 'lucide:activity',
          title: 'asterisk.menu.dashboards',
          authority: ['platform:admin', 'tenant:manager'],
        },
        component: () => import('./views/dashboards/index.vue'),
      },
    ],
  },
];

export default routes;
