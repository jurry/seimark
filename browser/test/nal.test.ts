import { test } from 'node:test';
import assert from 'node:assert/strict';
import { detectFraming, nalUnits, joinNALUnits, nalType } from '../src/nal.ts';
import { SeimarkError } from '../src/errors.ts';

const SPS = Uint8Array.of(0x67, 0x11);
const IDR = Uint8Array.of(0x65, 0x88);

const annexb = (units: Array<[number, Uint8Array]>) => {
  const out: number[] = [];
  for (const [scl, u] of units) {
    if (scl === 4) out.push(0x00);
    out.push(0x00, 0x00, 0x01, ...u);
  }
  return Uint8Array.from(out);
};

test('detects annex b from either start code length', () => {
  assert.equal(detectFraming(annexb([[4, SPS]])), 'annexb');
  assert.equal(detectFraming(annexb([[3, SPS]])), 'annexb');
});

test('detects length prefixed', () => {
  const au = Uint8Array.of(0x00, 0x00, 0x00, 0x02, 0x67, 0x11);
  assert.equal(detectFraming(au), 'length');
});

test('returns null for neither', () => {
  assert.equal(detectFraming(Uint8Array.of(0xff, 0xff, 0xff, 0xff)), null);
});

test('walks annex b and preserves each start code length', () => {
  const units = nalUnits(annexb([[4, SPS], [3, IDR]]), 'annexb');
  assert.equal(units.length, 2);
  assert.equal(units[0]!.startCodeLength, 4);
  assert.equal(units[1]!.startCodeLength, 3);
  assert.deepEqual(units[0]!.data, SPS);
  assert.deepEqual(units[1]!.data, IDR);
});

test('drops zero length units from back to back start codes', () => {
  const au = Uint8Array.of(0, 0, 0, 1, 0, 0, 0, 1, 0x65, 0x88);
  const units = nalUnits(au, 'annexb');
  assert.equal(units.length, 1);
  assert.deepEqual(units[0]!.data, IDR);
});

test('strips trailing zeros from an annex b unit', () => {
  const au = Uint8Array.of(0, 0, 0, 1, 0x65, 0x88, 0x00, 0x00, 0x00, 1, 0x67, 0x11);
  const units = nalUnits(au, 'annexb');
  assert.deepEqual(units[0]!.data, IDR);
  assert.deepEqual(units[1]!.data, SPS);
});

test('walks length prefixed', () => {
  const au = Uint8Array.of(0, 0, 0, 2, 0x67, 0x11, 0, 0, 0, 2, 0x65, 0x88);
  const units = nalUnits(au, 'length');
  assert.equal(units.length, 2);
  assert.deepEqual(units[1]!.data, IDR);
});

test('a length that overruns is truncated', () => {
  const au = Uint8Array.of(0, 0, 0, 9, 0x67, 0x11);
  let err: SeimarkError | undefined;
  try { nalUnits(au, 'length'); } catch (e) { err = e as SeimarkError; }
  assert.equal(err?.code, 'truncated');
});

test('a zero length is truncated', () => {
  const au = Uint8Array.of(0, 0, 0, 0, 0x67);
  let err: SeimarkError | undefined;
  try { nalUnits(au, 'length'); } catch (e) { err = e as SeimarkError; }
  assert.equal(err?.code, 'truncated');
});

test('join round trips annex b with mixed start code lengths', () => {
  const au = annexb([[4, SPS], [3, IDR]]);
  assert.deepEqual(joinNALUnits(nalUnits(au, 'annexb'), 'annexb'), au);
});

test('join round trips length prefixed', () => {
  const au = Uint8Array.of(0, 0, 0, 2, 0x67, 0x11, 0, 0, 0, 2, 0x65, 0x88);
  assert.deepEqual(joinNALUnits(nalUnits(au, 'length'), 'length'), au);
});

test('nalType masks the low five bits', () => {
  assert.equal(nalType(IDR), 5);
  assert.equal(nalType(Uint8Array.of(0x06)), 6);
});
