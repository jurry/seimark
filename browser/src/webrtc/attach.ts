import { type SeimarkErrorCode, SeimarkError } from '../errors.ts';
import {
  type Marker,
  type TimeSource,
  STREAM_ID_SIZE,
} from '../marker.ts';
import { Writer } from '../writer.ts';
import { FrameHandler, type FrameLike } from './handle.ts';
import { type ScriptTransformCtor, buildWorker, scriptTransformCtor } from './worker.ts';

export interface AttachOptions {
  streamId?: Uint8Array;
  keyframesOnly?: boolean;
  onError?: (code: SeimarkErrorCode, message: string, frameCount: number) => void;
  lastMarkerIntervalMs?: number;
}

export interface AttachStats {
  framesSeen: number;
  framesMarked: number;
  errors: number;
}

export interface SeimarkHandle {
  readonly streamId: Uint8Array;
  readonly timeSource: TimeSource;
  readonly lastMarker: Marker | null;
  readonly stats: Readonly<AttachStats>;
  setPayload(bytes: Uint8Array | null): void;
  detach(): void;
}

const DEFAULT_LAST_MARKER_INTERVAL_MS = 100;

const attached = new WeakSet<RTCRtpSender>();

interface WorkerMessage {
  kind: string;
  lastMarker?: Marker | null;
  stats?: AttachStats;
  code?: SeimarkErrorCode;
  message?: string;
  frames?: number;
}

interface EncodedStreams {
  readable: ReadableStream<RTCEncodedVideoFrame>;
  writable: WritableStream<RTCEncodedVideoFrame>;
}

function encodedStreamsOf(sender: RTCRtpSender): (() => EncodedStreams) | undefined {
  const fn = (sender as RTCRtpSender & { createEncodedStreams?: () => EncodedStreams })
    .createEncodedStreams;

  return typeof fn === 'function' ? fn.bind(sender) : undefined;
}

export function attach(sender: RTCRtpSender, opts: AttachOptions = {}): SeimarkHandle {
  const ctor = scriptTransformCtor();
  const streamsOf = encodedStreamsOf(sender);

  if (ctor === undefined && streamsOf === undefined) {
    throw new SeimarkError(
      'truncated',
      'this browser has no WebRTC encoded transform support',
    );
  }

  if (opts.streamId !== undefined && opts.streamId.length !== STREAM_ID_SIZE) {
    throw new SeimarkError(
      'truncated',
      `stream id is ${opts.streamId.length} bytes, needs ${STREAM_ID_SIZE}`,
    );
  }

  if (sender.track === null) {
    throw new SeimarkError('no_vcl', 'sender has no track');
  }

  if (attached.has(sender)) {
    throw new SeimarkError('already_marked', 'sender is already attached');
  }

  const keyframesOnly = opts.keyframesOnly ?? false;
  const intervalMs = opts.lastMarkerIntervalMs ?? DEFAULT_LAST_MARKER_INTERVAL_MS;

  // The writer is built here on both paths so the handle can report the stream id
  // synchronously, before the worker has started and before any frame flows.
  const writer = new Writer(
    opts.streamId === undefined
      ? { keyframesOnly }
      : { streamId: opts.streamId, keyframesOnly },
  );
  const streamId = writer.streamId;

  // The page callback runs outside the transform so a throw cannot error the pipe.
  const report = (code: SeimarkErrorCode, message: string, frames: number): void => {
    const fn = opts.onError;
    if (fn === undefined) return;
    queueMicrotask(() => {
      try {
        fn(code, message, frames);
      } catch {
        // A page callback that throws is the page's problem, not the stream's.
      }
    });
  };

  // Registered only once the path has been built, so a failure to construct the
  // worker leaves the sender attachable again.
  const handle =
    ctor === undefined
      ? fallbackPath(streamsOf!, writer, keyframesOnly, intervalMs, report)
      : workerPath(sender, ctor, streamId, keyframesOnly, intervalMs, report);

  attached.add(sender);

  return handle;
}

function workerPath(
  sender: RTCRtpSender,
  ctor: ScriptTransformCtor,
  streamId: Uint8Array,
  keyframesOnly: boolean,
  lastMarkerIntervalMs: number,
  report: (code: SeimarkErrorCode, message: string, frames: number) => void,
): SeimarkHandle {
  const worker = buildWorker();
  const target = sender as RTCRtpSender & { transform: RTCRtpScriptTransform | null };

  let lastMarker: Marker | null = null;
  let timeSource: TimeSource = 'send';
  let stats: AttachStats = { framesSeen: 0, framesMarked: 0, errors: 0 };
  let detached = false;

  worker.addEventListener('message', (event: MessageEvent) => {
    const msg = event.data as WorkerMessage;

    if (msg.kind === 'state') {
      lastMarker = msg.lastMarker ?? null;
      if (lastMarker !== null) timeSource = lastMarker.timeSource;
      if (msg.stats !== undefined) stats = msg.stats;

      return;
    }

    if (msg.kind === 'warn') {
      stats = { ...stats, errors: stats.errors + 1 };
      report(msg.code!, msg.message ?? '', msg.frames ?? stats.framesSeen);
    }
  });

  target.transform = new ctor(worker, {
    streamId,
    keyframesOnly,
    lastMarkerIntervalMs,
  });

  return {
    streamId,
    get timeSource() {
      return timeSource;
    },
    get lastMarker() {
      return lastMarker;
    },
    get stats() {
      return { ...stats };
    },
    setPayload(bytes) {
      if (detached) return;
      worker.postMessage({ kind: 'payload', payload: bytes });
    },
    detach() {
      if (detached) return;
      detached = true;
      target.transform = null;
      worker.terminate();
    },
  };
}

function fallbackPath(
  streamsOf: () => EncodedStreams,
  writer: Writer,
  keyframesOnly: boolean,
  lastMarkerIntervalMs: number,
  report: (code: SeimarkErrorCode, message: string, frames: number) => void,
): SeimarkHandle {
  const handler = new FrameHandler({
    writer,
    keyframesOnly,
    nowUs: () => BigInt(Math.round((performance.timeOrigin + performance.now()) * 1000)),
    captureUs: (t) => BigInt(Math.round((performance.timeOrigin + t) * 1000)),
    onWarn: report,
  });

  let lastMarker: Marker | null = null;
  let stats: AttachStats = { framesSeen: 0, framesMarked: 0, errors: 0 };

  // Coalesced on the same timer as the worker path, so both handles behave alike.
  const timer = setInterval(() => {
    lastMarker = handler.lastMarker;
    stats = { ...handler.stats };
  }, lastMarkerIntervalMs);

  const { readable, writable } = streamsOf();

  readable
    .pipeThrough(
      new TransformStream<RTCEncodedVideoFrame, RTCEncodedVideoFrame>({
        transform(frame, controller) {
          handler.handle(frame as unknown as FrameLike);
          controller.enqueue(frame);
        },
      }),
    )
    .pipeTo(writable)
    .catch(() => undefined);

  return {
    streamId: writer.streamId,
    get timeSource() {
      return lastMarker?.timeSource ?? 'send';
    },
    get lastMarker() {
      return lastMarker;
    },
    get stats() {
      return { ...stats };
    },
    setPayload(bytes) {
      handler.setPayload(bytes);
    },
    /**
     * createEncodedStreams may be called only once per sender and the pipe cannot be
     * undone, so detach puts the handler into passthrough: frames keep flowing
     * untouched and the handle stops updating.
     */
    detach() {
      handler.passthrough = true;
      clearInterval(timer);
    },
  };
}
