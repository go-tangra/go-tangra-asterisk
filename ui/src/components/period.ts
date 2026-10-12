import { ref, watch } from 'vue'
import { fromWallClock, loadTimezone, pbxTimezone, toWallClock } from './timezone'
// Period boundaries are typed as PBX wall-clock time and sent as UTC instants.
// The default period is the last `days` days up to now.
export function usePeriod(days=1) {
 const end=Date.now(),start=end-days*86400000
 const to=ref(toWallClock(end)),from=ref(toWallClock(start))
 // Defaults the user has not touched follow a timezone learnt later.
 watch(pbxTimezone,(tz,old)=>{if(from.value===toWallClock(start,old)&&to.value===toWallClock(end,old)){from.value=toWallClock(start,tz);to.value=toWallClock(end,tz)}})
 void loadTimezone()
 const query=()=>({from:fromWallClock(from.value),to:fromWallClock(to.value)})
 return {from,to,query}
}
