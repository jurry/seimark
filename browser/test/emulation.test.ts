import { test } from 'node:test';
import assert from 'node:assert/strict';
import { addEmulationPrevention, removeEmulationPrevention } from '../src/emulation.ts';

const hex = (s: string) => Uint8Array.from(Buffer.from(s.replace(/\s/g, ''), 'hex'));

test('the spec example gets two emulation prevention bytes', () => {
  const rbsp = hex(`05 26 44 a7 3c b9 b3 6c 45 9a 8f 1a a3 aa 43 1f 62 4a
                    01 00 00 06 5b 4f 7b ed f4 00 00 00 00 00 9f 3c 1a 77 e2 b0 4d 51 80`);
  const want = hex(`05 26 44 a7 3c b9 b3 6c 45 9a 8f 1a a3 aa 43 1f 62 4a
                    01 00 00 06 5b 4f 7b ed f4 00 00 03 00 00 03 00 9f 3c 1a 77 e2 b0 4d 51 80`);
  assert.deepEqual(addEmulationPrevention(rbsp), want);
  assert.deepEqual(removeEmulationPrevention(want), rbsp);
});

test('all four escaped values are escaped', () => {
  for (const b of [0x00, 0x01, 0x02, 0x03]) {
    const rbsp = Uint8Array.of(0x00, 0x00, b);
    assert.deepEqual(addEmulationPrevention(rbsp), Uint8Array.of(0x00, 0x00, 0x03, b));
  }
});

test('a byte above three after two zeros is not escaped', () => {
  const rbsp = Uint8Array.of(0x00, 0x00, 0x04);
  assert.deepEqual(addEmulationPrevention(rbsp), rbsp);
});

test('round trip on random data', () => {
  for (let i = 0; i < 200; i++) {
    const n = 1 + Math.floor(Math.random() * 64);
    const rbsp = new Uint8Array(n);
    for (let j = 0; j < n; j++) rbsp[j] = Math.floor(Math.random() * 4);
    assert.deepEqual(removeEmulationPrevention(addEmulationPrevention(rbsp)), rbsp);
  }
});
