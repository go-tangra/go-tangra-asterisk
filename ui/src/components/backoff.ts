// Reconnect delay for attempt n (0-based): exponential from base to max with
// "equal jitter" — half the window fixed, half random — so clients that lost
// the stream together do not reconnect in lockstep.
export function backoff(attempt:number,random:()=>number=Math.random,base=1000,max=30000):number{const window=Math.min(max,base*2**Math.max(0,attempt));return Math.round(window/2+random()*window/2)}
