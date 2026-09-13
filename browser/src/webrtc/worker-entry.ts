import { FrameHandler, type FrameLike } from './handle.ts';
import { markersIn } from '../scan.ts';
import { Writer } from '../writer.ts';

interface WriterInit {
  role?: 'writer';
  streamId: Uint8Array;
  keyframesOnly: boolean;
  lastMarkerIntervalMs: number;
}

interface ReaderInit {
  role: 'reader';
}

type Init = WriterInit | ReaderInit;

interface Transformer {
  readable: ReadableStream;
  writable: WritableStream;
  options: Init;
}

const scope = self as unknown as {
  postMessage(message: unknown): void;
  addEventListener(type: string, listener: (event: Event) => void): void;
};

let handler: FrameHandler | null = null;
let timer: ReturnType<typeof setInterval> | null = null;

function nowUs(): bigint {
  return BigInt(Math.round((performance.timeOrigin + performance.now()) * 1000));
}

function startWriter(t: Transformer, options: WriterInit): void {
  const writer = new Writer({
    streamId: options.streamId,
    keyframesOnly: options.keyframesOnly,
  });

  handler = new FrameHandler({
    writer,
    keyframesOnly: options.keyframesOnly,
    nowUs,
    captureUs: (t2) => BigInt(Math.round((performance.timeOrigin + t2) * 1000)),
    onWarn: (code, message, frames) =>
      scope.postMessage({ kind: 'warn', code, message, frames }),
  });

  if (timer !== null) clearInterval(timer);
  timer = setInterval(() => {
    if (handler === null) return;
    scope.postMessage({
      kind: 'state',
      lastMarker: handler.lastMarker,
      stats: { ...handler.stats },
    });
  }, options.lastMarkerIntervalMs);

  t.readable
    .pipeThrough(
      new TransformStream({
        transform(frame, controller) {
          handler?.handle(frame as FrameLike);
          controller.enqueue(frame);
        },
      }),
    )
    .pipeTo(t.writable)
    .catch(() => undefined);
}

function startReader(t: Transformer): void {
  t.readable
    .pipeThrough(
      new TransformStream({
        transform(frame, controller) {
          try {
            const data = (frame as { data: ArrayBuffer }).data;
            const { markers } = markersIn(new Uint8Array(data), 'annexb');
            for (const m of markers) scope.postMessage({ kind: 'marker', marker: m });
          } catch {
            // A reader observes; it must never error the pipeline.
          }
          controller.enqueue(frame);
        },
      }),
    )
    .pipeTo(t.writable)
    .catch(() => undefined);
}

scope.addEventListener('rtctransform', (event) => {
  const t = (event as unknown as { transformer: Transformer }).transformer;

  if (t.options.role === 'reader') {
    startReader(t);

    return;
  }

  startWriter(t, t.options);
});

scope.addEventListener('message', (event) => {
  const msg = (event as MessageEvent).data as {
    kind: string;
    payload?: Uint8Array | null;
    passthrough?: boolean;
  };

  if (msg.kind === 'payload') handler?.setPayload(msg.payload ?? null);
  if (msg.kind === 'passthrough' && handler !== null) {
    handler.passthrough = msg.passthrough ?? true;
  }
});
