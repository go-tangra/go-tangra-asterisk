import type { RouteRecordRaw } from 'vue-router'
import './main.css'
export const routes: RouteRecordRaw[] = [
 { path:'/asterisk', name:'asterisk-calls', component:()=>import('./views/calls/index.vue'), meta:{module:'asterisk',requires:'calls:read'} },
 { path:'/asterisk/overview', name:'asterisk-overview', component:()=>import('./views/overview/index.vue'), meta:{module:'asterisk',requires:'stats:read'} },
 { path:'/asterisk/extensions', name:'asterisk-extensions', component:()=>import('./views/extensions/index.vue'), meta:{module:'asterisk',requires:'stats:read'} },
 { path:'/asterisk/live', name:'asterisk-live', component:()=>import('./views/live/index.vue'), meta:{module:'asterisk',requires:'live:read'} },
 { path:'/asterisk/dashboards', name:'asterisk-dashboards', component:()=>import('./views/dashboards/index.vue'), meta:{module:'asterisk',requires:'dashboard:read'} },
]
export default routes
