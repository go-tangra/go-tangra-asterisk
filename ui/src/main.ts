import { createApp } from 'vue'
import { createRouter, createWebHistory } from 'vue-router'
import { createPinia } from 'pinia'
import { abilitiesPlugin } from '@casl/vue'
import { createMongoAbility } from '@casl/ability'
import { routes } from './routes'
import App from './App.vue'
import '@go-tangra/ui/theme.css'
// Standalone development denies permission-dependent actions until the portal provides abilities.
createApp(App).use(createPinia()).use(createRouter({history:createWebHistory(),routes})).use(abilitiesPlugin,createMongoAbility([])).mount('#app')
