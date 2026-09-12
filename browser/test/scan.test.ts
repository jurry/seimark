import { test } from 'node:test';
import assert from 'node:assert/strict';
import { markersIn, stripMarkers, hasIDR, firstVCLIndex } from '../src/scan.ts';
import { buildMarkerNAL } from '../src/sei.ts';
import { encodeMarker } from '../src/marker.ts';
import { nalUnits } from '../src/nal.ts';
import { SeimarkError } from '../src/errors.ts';

const marker = encodeMarker({
  timeSource: 'send',
  originTimeUs: 1789246800000000n,
  sequence: 3,
  streamId: Uint8Array.of(0x9f, 0x3c, 0x1a, 0x77, 0xe2, 0xb0, 0x4d, 0x51),
  payload: null,
});

const AUD = Uint8Array.of(0x09, 0x10);
const SPS = Uint8Array.of(0x67, 0x11);
const IDR = Uint8Array.of(0x65, 0x88);
const P = Uint8Array.of(0x41, 0x88);

const annexb = (...units: Uint8Array[]) =>
  Uint8Array.from(units.flatMap((u) => [0x00, 0x00, 0x00, 0x01, ...u]));

test('finds a marker among other nal units', () => {
  const au = annexb(AUD, SPS, buildMarkerNAL(marker), IDR);
  const { markers, warnings } = markersIn(au, 'annexb');
  assert.equal(markers.length, 1);
  assert.equal(markers[0]!.sequence, 3);
  assert.deepEqual(warnings, []);
});

test('an access unit with no marker yields nothing and no warning', () => {
  const { markers, warnings } = markersIn(annexb(SPS, IDR), 'annexb');
  assert.deepEqual(markers, []);
  assert.deepEqual(warnings, []);
});

test('a foreign unregistered uuid is skipped without a warning', () => {
  const foreign = Uint8Array.from([0x06, 0x05, 20, ...new Uint8Array(16).fill(0xab), 1, 2, 3, 4, 0x80]);
  const { markers, warnings } = markersIn(annexb(foreign, IDR), 'annexb');
  assert.deepEqual(markers, []);
  assert.deepEqual(warnings, []);
});

test('an unparsable sei unit becomes a warning and the scan continues', () => {
  const broken = Uint8Array.of(0x06, 0x05, 0x40, 0x01);
  const au = annexb(broken, buildMarkerNAL(marker), IDR);
  const { markers, warnings } = markersIn(au, 'annexb');
  assert.equal(markers.length, 1);
  assert.equal(warnings.length, 1);
  assert.equal(warnings[0]!.code, 'unparsable_sei');
});

test('stripMarkers removes only marker units', () => {
  const au = annexb(SPS, buildMarkerNAL(marker), IDR);
  const stripped = stripMarkers(au, 'annexb');
  assert.deepEqual(markersIn(stripped, 'annexb').markers, []);
  assert.equal(nalUnits(stripped, 'annexb').length, 2);
});

test('stripping an unmarked access unit is a no-op', () => {
  const au = annexb(SPS, IDR);
  assert.deepEqual(stripMarkers(au, 'annexb'), au);
});

test('hasIDR sees type five and not type one', () => {
  assert.equal(hasIDR(annexb(SPS, IDR), 'annexb'), true);
  assert.equal(hasIDR(annexb(SPS, P), 'annexb'), false);
});

test('firstVCLIndex points past the parameter sets', () => {
  const units = nalUnits(annexb(AUD, SPS, IDR), 'annexb');
  assert.equal(firstVCLIndex(units), 2);
});

test('an access unit with no vcl unit is no_vcl', () => {
  const units = nalUnits(annexb(AUD, SPS), 'annexb');
  let err: SeimarkError | undefined;
  try { firstVCLIndex(units); } catch (e) { err = e as SeimarkError; }
  assert.equal(err?.code, 'no_vcl');
});
