import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { markersIn } from '../src/scan.ts';
import { nalUnits, nalType } from '../src/nal.ts';

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
