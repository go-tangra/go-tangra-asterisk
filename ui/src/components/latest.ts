import { getCurrentInstance, onUnmounted } from 'vue'
// Only the latest request of a view may apply its result: starting a request
// aborts the previous one, and a response whose signal is aborted is stale and
// must be dropped (fetch may already have resolved when abort is called).
export function useLatest(){
 let controller:AbortController|undefined
 const start=()=>{controller?.abort();controller=new AbortController();return controller.signal}
 const cancel=()=>controller?.abort()
 if(getCurrentInstance())onUnmounted(cancel)
 return {start,cancel}
}
