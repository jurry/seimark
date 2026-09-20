import { SeimarkError } from '../errors.ts';
import { type Marker } from '../marker.ts';
import { markersIn } from '../scan.ts';
import { buildWorker, scriptTransformCtor } from './worker.ts';

/** What one sequence number said about the one before it on the same stream. */
export interface SequenceVerdict {
  /** At least one sequence number was skipped, so frames were lost. */
  gap: boolean;
  /** The sequence went backwards: a retransmission, or a sender that restarted. */
  duplicate: boolean;
}

/** Counters for a receiver, never reset. Gaps and duplicates are per stream id. */
export interface ReaderStats {
  /** Incoming frames observed, with a marker or without. */
  framesSeen: number;
  /** Markers decoded. More than `framesSeen` if a frame carried several. */
  markersFound: number;
  /** Sequence jumps forward, each one or more lost frames. */
  gaps: number;
  /** Markers whose sequence went backwards. */
  duplicates: number;
  /** Distinct stream ids seen; more than one means the sender restarted. */
  streams: number;
}

/** The page's view of an attached receiver, readable synchronously at any time. */
export interface ReaderHandle {
  /** The most recent marker read, or null before the first one. */
  readonly lastMarker: Marker | null;
  /**
   * A snapshot of the counters. Everything but `framesSeen` updates as markers
   * arrive; on the worker path `framesSeen` is reported every 100 ms.
   */
  readonly stats: Readonly<ReaderStats>;
  /**
   * Stops reading. On the worker path the transform is removed; on the
   * `createEncodedStreams` fallback the pipe cannot be undone, so the reader
   * stops observing and frames keep flowing.
   */
  detach(): void;
}

const HALF_UINT32 = 0x80000000;

/**
 * Compares `sequence` against the last one seen for `streamId` and records it,
 * mutating `state`. The first sequence of a stream is neither a gap nor a
 * duplicate. Sequence wraps modulo 2^32, so ordering is the shorter way round
 * the circle.
 */
export function trackSequence(
  state: Map<string, number>,
  streamId: string,
  sequence: number,
): SequenceVerdict {
  const previous = state.get(streamId);

  if (previous === undefined) {
    state.set(streamId, sequence);

    return { gap: false, duplicate: false };
  }

  const forward = (sequence - ((previous + 1) >>> 0)) >>> 0;

  if (forward >= HALF_UINT32) {
    return { gap: false, duplicate: true };
  }

  state.set(streamId, sequence);

  return { gap: forward !== 0, duplicate: false };
}

function hex(bytes: Uint8Array): string {
  let out = '';
  for (const b of bytes) out += b.toString(16).padStart(2, '0');

  return out;
}

/**
 * Reads seimark markers off an incoming track and calls `onMarker` for each, in
 * arrival order. Attach before the local description is set: as the answerer
 * that means inside the `track` event, which fires during
 * `setRemoteDescription(offer)`; as the offerer the event comes too late and a
 * transform set then is never wired.
 *
 * Throws `SeimarkError` with `unsupported_browser` when the browser has no
 * encoded transform, and `csp_blocked` when the policy refuses the worker.
 * Afterwards it never throws: an unreadable frame is skipped, and a throw from
 * `onMarker` is ignored so the page cannot break the stream.
 */
export function reader(
  receiver: RTCRtpReceiver,
  onMarker: (m: Marker) => void,
): ReaderHandle {
  const stats: ReaderStats = {
    framesSeen: 0,
    markersFound: 0,
    gaps: 0,
    duplicates: 0,
    streams: 0,
  };

  const sequences = new Map<string, number>();
  let lastMarker: Marker | null = null;
  let detached = false;
  const pending: Marker[] = [];

  const record = (marker: Marker): void => {
    stats.markersFound++;
    lastMarker = marker;

    const id = hex(marker.streamId);
    if (!sequences.has(id)) stats.streams++;

    const verdict = trackSequence(sequences, id, marker.sequence);
    if (verdict.gap) stats.gaps++;
    if (verdict.duplicate) stats.duplicates++;

    pending.push(marker);
  };

  let draining = false;

  // The page callback runs outside the transform so a throw cannot error the pipe.
  const drain = (): void => {
    if (draining) return;
    draining = true;
    queueMicrotask(() => {
      draining = false;
      while (pending.length > 0) {
        const marker = pending.shift()!;
        try {
          onMarker(marker);
        } catch {
          // A page callback that throws is the page's problem, not the stream's.
        }
      }
    });
  };

  const ctor = scriptTransformCtor();

  if (ctor !== undefined) {
    const worker = buildWorker();
    const target = receiver as RTCRtpReceiver & {
      transform: RTCRtpScriptTransform | null;
    };

    worker.addEventListener('message', (event: MessageEvent) => {
      if (detached) return;
      const msg = event.data as { kind: string; marker?: Marker; count?: number };
      if (msg.kind === 'frames' && msg.count !== undefined) {
        stats.framesSeen = msg.count;

        return;
      }
      if (msg.kind !== 'marker' || msg.marker === undefined) return;
      record(msg.marker);
      drain();
    });

    target.transform = new ctor(worker, { role: 'reader' });

    return {
      get lastMarker() {
        return lastMarker;
      },
      get stats() {
        return { ...stats };
      },
      detach() {
        detached = true;
        target.transform = null;
        worker.terminate();
      },
    };
  }

  const streams = (
    receiver as RTCRtpReceiver & {
      createEncodedStreams?: () => {
        readable: ReadableStream<RTCEncodedVideoFrame>;
        writable: WritableStream<RTCEncodedVideoFrame>;
      };
    }
  ).createEncodedStreams;

  if (streams === undefined) {
    throw new SeimarkError(
      'unsupported_browser',
      'this browser has no WebRTC encoded transform support',
    );
  }

  const { readable, writable } = streams.call(receiver);

  readable
    .pipeThrough(
      new TransformStream<RTCEncodedVideoFrame, RTCEncodedVideoFrame>({
        transform(frame, controller) {
          if (!detached) {
            stats.framesSeen++;
            try {
              for (const m of markersIn(new Uint8Array(frame.data), 'annexb').markers) {
                record(m);
              }
            } catch {
              // An unreadable frame is skipped; the stream continues.
            }
          }
          controller.enqueue(frame);
          drain();
        },
      }),
    )
    .pipeTo(writable)
    .catch(() => undefined);

  return {
    get lastMarker() {
      return lastMarker;
    },
    get stats() {
      return { ...stats };
    },
    /** createEncodedStreams cannot be undone, so detach stops observing instead. */
    detach() {
      detached = true;
    },
  };
}
