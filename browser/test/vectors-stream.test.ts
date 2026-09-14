import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { hasIDR, markersIn, stripMarkers } from '../src/scan.ts';
import { nalUnits, nalType } from '../src/nal.ts';
import { Writer } from '../src/writer.ts';
import { type TimeSource } from '../src/marker.ts';

const vectors = fileURLToPath(new URL('../../vectors/streams/', import.meta.url));

interface Line {
  au: number;
  sequence: number;
  origin_us: number;
  stream_id: string;
  time_source: string;
  payload?: string;
}

function accessUnits(stream: Uint8Array): Uint8Array[] {
  const units = nalUnits(stream, 'annexb');
  const out: Uint8Array[] = [];
  let current: typeof units = [];
  let seenVCL = false;

  const flush = () => {
    if (current.length > 0) {
      out.push(
        Uint8Array.from(
          current.flatMap((u) => [
            ...(u.startCodeLength === 4 ? [0] : []),
            0, 0, 1,
            ...u.data,
          ]),
        ),
      );
    }
    current = [];
    seenVCL = false;
  };

  for (const u of units) {
    const t = nalType(u.data);
    const starts = seenVCL && (t === 9 || t === 7 || t === 8 || t === 6 || ((t >= 1 && t <= 5) && (u.data[1]! & 0x80) !== 0));
    if (starts) flush();
    current.push(u);
    if (t >= 1 && t <= 5) seenVCL = true;
  }
  flush();

  return out;
}

test('reads the go-written annex b stream exactly as the go reader did', () => {
  const stream = new Uint8Array(readFileSync(vectors + 'testsrc-marked.h264'));
  const want = readFileSync(vectors + 'testsrc-marked.h264.jsonl', 'utf8')
    .trim()
    .split('\n')
    .map((l) => JSON.parse(l) as Line);

  const aus = accessUnits(stream);
  const got: Line[] = [];
  for (const [i, au] of aus.entries()) {
    for (const m of markersIn(au, 'annexb').markers) {
      got.push({
        au: i,
        sequence: m.sequence,
        origin_us: Number(m.originTimeUs),
        stream_id: Buffer.from(m.streamId).toString('hex'),
        time_source: m.timeSource,
        ...(m.payload ? { payload: Buffer.from(m.payload).toString('base64') } : {}),
      });
    }
  }

  assert.equal(got.length, want.length);
  for (const [i, line] of got.entries()) {
    assert.equal(line.au, want[i]!.au);
    assert.equal(line.sequence, want[i]!.sequence);
    assert.equal(line.origin_us, want[i]!.origin_us);
    assert.equal(line.stream_id, want[i]!.stream_id);
    assert.equal(line.time_source, want[i]!.time_source);
    assert.equal(line.payload, want[i]!.payload);
  }
});

test('restamps a stripped go-written stream to the same bytes the go writer produced', () => {
  const stream = new Uint8Array(readFileSync(vectors + 'testsrc-marked.h264'));
  const want = readFileSync(vectors + 'testsrc-marked.h264.jsonl', 'utf8')
    .trim()
    .split('\n')
    .map((l) => JSON.parse(l) as Line);

  const aus = accessUnits(stream);
  const byAU = new Map(want.map((l) => [l.au, l]));
  const streamId = Uint8Array.from(Buffer.from(want[0]!.stream_id, 'hex'));
  const writer = new Writer({ streamId, timeSource: want[0]!.time_source as TimeSource });

  for (const [i, au] of aus.entries()) {
    const line = byAU.get(i);
    if (line === undefined) continue;

    const stripped = stripMarkers(au, 'annexb');
    const payload = line.payload === undefined
      ? null
      : Uint8Array.from(Buffer.from(line.payload, 'base64'));
    const result = writer.mark(
      stripped,
      'annexb',
      BigInt(line.origin_us),
      hasIDR(stripped, 'annexb'),
      payload,
    );

    assert.equal(result.marked, true);
    assert.deepEqual(result.data, au, `access unit ${i} differs from the go writer's bytes`);
  }

  assert.equal(writer.sequence, want.length);
});
