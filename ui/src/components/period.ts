import { ref } from 'vue'
export function usePeriod() {
 const local = (d:Date) => new Date(d.getTime()-d.getTimezoneOffset()*60000).toISOString().slice(0,16)
 const to=ref(local(new Date())),from=ref(local(new Date(Date.now()-86400000)))
 const query=()=>({from:new Date(from.value).toISOString(),to:new Date(to.value).toISOString()})
 return {from,to,query}
}
