import { onMounted, onUnmounted, ref } from 'vue'
// A reactive "now" that ticks every second while the component lives, for
// running timers.
export function useNow(){const now=ref(Date.now());let timer:ReturnType<typeof setInterval>|undefined;onMounted(()=>{timer=setInterval(()=>{now.value=Date.now()},1000)});onUnmounted(()=>clearInterval(timer));return now}
// "4:07" or "1:02:33" from milliseconds.
export function elapsed(ms:number):string{const s=Math.max(0,Math.floor(ms/1000)),h=Math.floor(s/3600),m=Math.floor(s/60)%60,r=s%60;const two=(n:number)=>String(n).padStart(2,'0');return h?`${h}:${two(m)}:${two(r)}`:`${m}:${two(r)}`}
