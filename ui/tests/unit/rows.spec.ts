import { expect, it, vi } from 'vitest'
import { useRowActivation } from '../../src/components/rows'
type Row={id:string}
const items:Row[]=[{id:'a'},{id:'b'}]
function key(target:HTMLElement,k:string){const event=new KeyboardEvent('keydown',{key:k,bubbles:true,cancelable:true});target.dispatchEvent(event);return event}
it('makes rows focusable with a stable key and an accessible name',()=>{const rows=useRowActivation<Row>(r=>r.id,r=>'Open '+r.id,()=>items,()=>{});expect(rows.rowAttrs({id:'b'})).toMatchObject({tabindex:'0','aria-label':'Open b','data-row-key':'b'})})
it('opens the focused row on Enter and Space only',()=>{const open=vi.fn();const rows=useRowActivation<Row>(r=>r.id,r=>r.id,()=>items,open);const wrapper=document.createElement('div');wrapper.addEventListener('keydown',rows.onKeydown);const row=document.createElement('div');for(const [k,v] of Object.entries(rows.rowAttrs({id:'b'})))row.setAttribute(k,v);const inner=document.createElement('button');row.append(inner);wrapper.append(row);expect(key(row,'Enter').defaultPrevented).toBe(true);key(row,' ');key(row,'a');key(inner,'Enter');expect(open.mock.calls).toEqual([[{id:'b'}],[{id:'b'}]])})
