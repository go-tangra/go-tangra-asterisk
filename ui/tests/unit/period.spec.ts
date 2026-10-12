import { expect, it, vi } from 'vitest'
import { usePeriod } from '../../src/components/period'
it('defaults to the last day, or the last given number of days',()=>{vi.useFakeTimers({now:Date.parse('2026-10-12T05:17:00Z'),toFake:['Date']});vi.stubGlobal('fetch',vi.fn(()=>new Promise(()=>{})));const span=(p:ReturnType<typeof usePeriod>)=>Date.parse(p.query().to)-Date.parse(p.query().from);expect(span(usePeriod())).toBe(86400000);expect(span(usePeriod(7))).toBe(7*86400000);vi.unstubAllGlobals();vi.useRealTimers()})
