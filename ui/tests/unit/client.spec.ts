import { afterEach, expect, it, vi } from 'vitest'
import { get, recordingURL } from '../../src/api/client'
afterEach(()=>vi.unstubAllGlobals())
it('uses the portal cookie and safely encodes call identifiers',async()=>{const fetch=vi.fn().mockResolvedValue({ok:true,status:200,json:async()=>({summary:{linkedid:'a/b'}})});vi.stubGlobal('fetch',fetch);await get('/api/asterisk/calls/{linkedid}',{}, {linkedid:'a/b'});expect(fetch.mock.calls[0]?.[0]).toBe('/api/asterisk/calls/a%2Fb');expect(fetch.mock.calls[0]?.[1]).toMatchObject({credentials:'same-origin'});expect(recordingURL('a/../b')).toBe('/api/asterisk/recordings/a%2F..%2Fb')})
