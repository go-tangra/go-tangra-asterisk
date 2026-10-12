import type { RouteRecordRaw } from 'vue-router'
import './main.css'
export const routes: RouteRecordRaw[] = [
 { path:'/asterisk/team', name:'asterisk-team', component:()=>import('./views/team/index.vue'), meta:{module:'asterisk',requires:'stats:read'} },
 { path:'/asterisk/team/:extension', name:'asterisk-agent', component:()=>import('./views/team/agent.vue'), meta:{module:'asterisk',requires:'stats:read'} },
 { path:'/asterisk', name:'asterisk-calls', component:()=>import('./views/calls/index.vue'), meta:{module:'asterisk',requires:'calls:read'} },
 { path:'/asterisk/calls/log', name:'asterisk-call-log', component:()=>import('./views/calls/log.vue'), meta:{module:'asterisk',requires:'calls:read'} },
 { path:'/asterisk/quality', name:'asterisk-quality', component:()=>import('./views/quality/index.vue'), meta:{module:'asterisk',requires:'stats:read'} },
 { path:'/asterisk/overview', redirect:{name:'asterisk-team'} },
 { path:'/asterisk/extensions', redirect:{name:'asterisk-team'} },
 { path:'/asterisk/live', name:'asterisk-live', component:()=>import('./views/live/index.vue'), meta:{module:'asterisk',requires:'live:read'} },
 { path:'/asterisk/dashboards', name:'asterisk-dashboards', component:()=>import('./views/dashboards/index.vue'), meta:{module:'asterisk',requires:'dashboard:read'} },
]
export default routes
