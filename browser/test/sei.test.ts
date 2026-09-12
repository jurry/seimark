import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { buildMarkerNAL, parseSEI } from '../src/sei.ts';
import { SeimarkError } from '../src/errors.ts';

const vectors = fileURLToPath(new URL('../../vectors/', import.meta.url));

test('builds the spec example nal unit byte for byte', () => {
  const body = new Uint8Array(readFileSync(vectors + 'markers/001-spec-example.bin'));
  const want = new Uint8Array(readFileSync(vectors + 'nal/001-spec-example.bin'));
  assert.deepEqual(buildMarkerNAL(body), want);
});

test('parses the spec example back to one message', () => {
  const nal = new Uint8Array(readFileSync(vectors + 'nal/001-spec-example.bin'));
  const msgs = parseSEI(nal);
  assert.equal(msgs.length, 1);
  assert.equal(msgs[0]!.type, 5);
  assert.equal(msgs[0]!.body.length, 22);
});

test('codes a payload size over 254 with the 0xff run', () => {
  const body = new Uint8Array(300);
  body[0] = 1;
  const nal = buildMarkerNAL(body);
  assert.equal(nal[1], 5);
  assert.equal(nal[2], 0xff);
  assert.equal(nal[3], 300 + 16 - 255);
  assert.equal(parseSEI(nal)[0]!.body.length, 300);
});

test('a nal unit too short to hold an rbsp is unparsable', () => {
  let err: SeimarkError | undefined;
  try { parseSEI(Uint8Array.of(0x06)); } catch (e) { err = e as SeimarkError; }
  assert.equal(err?.code, 'unparsable_sei');
});

test('a payload size that overruns the unit is unparsable', () => {
  let err: SeimarkError | undefined;
  try { parseSEI(Uint8Array.of(0x06, 0x05, 0x40, 0x01, 0x80)); } catch (e) { err = e as SeimarkError; }
  assert.equal(err?.code, 'unparsable_sei');
});

test('a foreign unregistered message keeps its uuid and is not a marker', () => {
  const foreign = new Uint8Array(16).fill(0xab);
  const body = new Uint8Array(4).fill(1);
  const rbsp = Uint8Array.of(0x06, 0x05, 20, ...foreign, ...body, 0x80);
  const msgs = parseSEI(rbsp);
  assert.deepEqual(msgs[0]!.uuid, foreign);
});
