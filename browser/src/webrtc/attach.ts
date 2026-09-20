import { type SeimarkErrorCode, SeimarkError } from '../errors.ts';
import {
  type Marker,
  type TimeSource,
  STREAM_ID_SIZE,
} from '../marker.ts';
import { Writer } from '../writer.ts';
import { FrameHandler, type FrameLike } from './handle.ts';
import { type ScriptTransformCtor, buildWorker, scriptTransformCtor } from './worker.ts';

/** Settings for `attach`; all of them have a default. */
export interface AttachOptions {
  /**
   * Eight bytes identifying the stream. Defaults to eight random bytes; any
   * other length throws `invalid_argument`.
   */
  streamId?: Uint8Array;
  /** Mark only keyframes instead of every outgoing frame. Defaults to false. */
  keyframesOnly?: boolean;
  /**
   * Called with the first occurrence of each distinct error code, so a stream
   * failing every frame reports once rather than thirty times a second while a
   * second, different failure is still reported. `frameCount` is `framesSeen`
   * at the time. It runs outside the transform, and a throw from it is ignored.
   */
  onError?: (code: SeimarkErrorCode, message: string, frameCount: number) => void;
  /**
   * How often `lastMarker` and `stats` are refreshed, in milliseconds.
   * Defaults to 100.
   */
  lastMarkerIntervalMs?: number;
}

/** Counters for a sender, refreshed on the coalesced timer, never reset. */
export interface AttachStats {
  /** Frames that reached the transform, marked or not. */
  framesSeen: number;
  /** Frames a marker was written into. */
  framesMarked: number;
  /** Frames that could not be marked. Each was passed through unchanged. */
  errors: number;
}

/**
 * The page's view of an attached sender. Every member is readable synchronously
 * at the moment of a UI event: no await and no subscription.
 */
export interface SeimarkHandle {
  /** The id being stamped, known before the first frame flows. */
  readonly streamId: Uint8Array;
  /**
   * The clock the probe settled on from the first frame. `'send'` until then,
   * and on Chromium 153 senders, which expose no capture time.
   */
  readonly timeSource: TimeSource;
  /**
   * The most recent marker written, or null before the first one. Refreshed on
   * the coalesced timer, so it lags the stream by at most one interval.
   */
  readonly lastMarker: Marker | null;
  /** A snapshot of the counters, refreshed on the same timer as `lastMarker`. */
  readonly stats: Readonly<AttachStats>;
  /**
   * Sets the bytes stamped into every marked frame from now on, until replaced;
   * null clears it. Pushed, not computed per frame, because the writer runs in a
   * worker that cannot call back into the page. A payload above
   * `PAYLOAD_SOFT_LIMIT` is still stamped but reported through `onError`.
   */
  setPayload(bytes: Uint8Array | null): void;
  /**
   * Stops marking. On the worker path the transform is removed and the sender
   * can be attached again; on the `createEncodedStreams` fallback the pipe
   * cannot be undone, so frames keep flowing untouched and the handle stops
   * updating.
   */
  detach(): void;
}

const DEFAULT_LAST_MARKER_INTERVAL_MS = 100;

// Cleared by detach on the worker path, so a sender can be attached again after a
// reconnect. The fallback path never clears it: createEncodedStreams is once per
// sender and its pipe cannot be undone.
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

/**
 * Stamps a marker into every frame leaving `sender`, and returns a handle the
 * page can read at any time. Call it before `setLocalDescription`: a transform
 * installed on a running sender sees no frames.
 *
 * Throws `SeimarkError` for what is settled up front — `unsupported_browser`,
 * `invalid_argument` for a bad stream id or a sender with no track,
 * `already_attached`, `csp_blocked` — and never once frames flow, where a frame
 * that cannot be marked is passed through unchanged and reported to `onError`.
 */
export function attach(sender: RTCRtpSender, opts: AttachOptions = {}): SeimarkHandle {
  const ctor = scriptTransformCtor();
  const streamsOf = encodedStreamsOf(sender);

  if (ctor === undefined && streamsOf === undefined) {
    throw new SeimarkError(
      'unsupported_browser',
      'this browser has no WebRTC encoded transform support',
    );
  }

  if (opts.streamId !== undefined && opts.streamId.length !== STREAM_ID_SIZE) {
    throw new SeimarkError(
      'invalid_argument',
      `stream id is ${opts.streamId.length} bytes, needs ${STREAM_ID_SIZE}`,
    );
  }

  if (sender.track === null) {
    throw new SeimarkError('invalid_argument', 'sender has no track');
  }

  if (attached.has(sender)) {
    throw new SeimarkError('already_attached', 'sender is already attached');
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
      : workerPath(sender, ctor, streamId, keyframesOnly, intervalMs, report, () =>
          attached.delete(sender),
        );

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
  release: () => void,
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
      release();
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
