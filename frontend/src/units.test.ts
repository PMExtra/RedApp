import {it,expect} from 'vitest'
import {bytes} from './api'
it('formats IEC units with 1024 steps and two decimals',()=>{
 expect(bytes(40860.52*1048576)).toBe('39.90 GiB')
 expect(bytes(0)).toBe('0.00 B');expect(bytes(1)).toBe('1.00 B');expect(bytes(1023)).toBe('1023.00 B');expect(bytes(1024)).toBe('1.00 KiB');expect(bytes(1024**2)).toBe('1.00 MiB');expect(bytes(1024**4)).toBe('1.00 TiB')
})
it('handles unknown, negative and non-finite values',()=>{for(const n of [undefined,null,-1,NaN,Infinity])expect(bytes(n)).toBe('—')})
