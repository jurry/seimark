import { test, expect } from '@playwright/test';

interface LoopbackResult {
  sent: number;
  received: number;
  sequences: number[];
  streamId: string;
  readerStreamId: string;
  timeSource: string;
  captureTimeSeen: boolean;
  lastMarkerSeen: boolean;
  framesEncoded: number;
  codec: string;
  senderFramesSeen: number;
  senderErrors: number;
  readerFramesSeen: number;
  readerGaps: number;
  readerDuplicates: number;
  readerStreams: number;
  errors: string[];
}

test('markers survive a loopback peer connection', async ({ page, browserName }) => {
  page.on('pageerror', (e) => console.log(`[${browserName}] page error: ${String(e)}`));

  await page.goto('/fixture.html');

  const result = (await page.evaluate(
    () => (window as unknown as { run(): Promise<LoopbackResult> }).run(),
  )) as LoopbackResult;

  console.log(`[${browserName}] ${JSON.stringify(result)}`);
  console.log(
    `[${browserName}] captureTime on sender frames: ${result.captureTimeSeen}, time source: ${result.timeSource}`,
  );

  // A browser that negotiates H.264 and then encodes nothing has no H.264
  // encoder in its build; that is an environment gap, not a result about seimark.
  expect(
    result.framesEncoded,
    `${browserName} negotiated ${result.codec} but encoded no frames: no H.264 encoder in this build`,
  ).toBeGreaterThan(5);

  expect(result.errors).toEqual([]);
  expect(result.senderErrors).toBe(0);

  expect(result.sent).toBeGreaterThan(5);
  expect(result.received).toBeGreaterThan(5);

  expect(result.sequences).toEqual([...result.sequences].sort((a, b) => a - b));
  expect(new Set(result.sequences).size).toBe(result.sequences.length);

  expect(result.readerStreamId).toBe(result.streamId);
  expect(result.readerStreams).toBe(1);
  expect(result.readerGaps).toBe(0);
  expect(result.readerDuplicates).toBe(0);
  expect(result.lastMarkerSeen).toBe(true);
});
