import { type Marker } from '../marker.ts';
import { markersIn } from '../scan.ts';
import { buildWorker, scriptTransformCtor } from './worker.ts';

export interface SequenceVerdict {
  gap: boolean;
  duplicate: boolean;
}

export interface ReaderStats {
  framesSeen: number;
  markersFound: number;
  gaps: number;
  duplicates: number;
  streams: number;
}

export interface ReaderHandle {
  readonly lastMarker: Marker | null;
  readonly stats: Readonly<ReaderStats>;
  detach(): void;
}

const HALF_UINT32 = 0x80000000;

/** Sequence wraps modulo 2^32, so ordering is the shorter way round the circle. */
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
      const msg = event.data as { kind: string; marker?: Marker };
      if (msg.kind !== 'marker' || msg.marker === undefined) return;
      stats.framesSeen++;
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
    throw new Error('this browser has no WebRTC encoded transform support');
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
