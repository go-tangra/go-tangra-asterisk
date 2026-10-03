import { ref, watch } from 'vue'
import { fromWallClock, loadTimezone, pbxTimezone, toWallClock } from './timezone'
// Period boundaries are typed as PBX wall-clock time and sent as UTC instants.
export function usePeriod() {
 const end=Date.now(),start=end-86400000
 const to=ref(toWallClock(end)),from=ref(toWallClock(start))
 // Defaults the user has not touched follow a timezone learnt later.
 watch(pbxTimezone,(tz,old)=>{if(from.value===toWallClock(start,old)&&to.value===toWallClock(end,old)){from.value=toWallClock(start,tz);to.value=toWallClock(end,tz)}})
 void loadTimezone()
 const query=()=>({from:fromWallClock(from.value),to:fromWallClock(to.value)})
 return {from,to,query}
}
