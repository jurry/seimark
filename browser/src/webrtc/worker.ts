import { SeimarkError } from '../errors.ts';
import { WORKER_SOURCE } from './worker-source.ts';

export interface ScriptTransformCtor {
  new (worker: Worker, options?: unknown): RTCRtpScriptTransform;
}

export function scriptTransformCtor(): ScriptTransformCtor | undefined {
  return (globalThis as unknown as { RTCRtpScriptTransform?: ScriptTransformCtor })
    .RTCRtpScriptTransform;
}

/** A policy without `worker-src blob:` would otherwise leave a silently unmarked stream. */
export function buildWorker(): Worker {
  const url = URL.createObjectURL(
    new Blob([WORKER_SOURCE], { type: 'text/javascript' }),
  );

  try {
    return new Worker(url, { type: 'module' });
  } catch (e) {
    throw new SeimarkError(
      'csp_blocked',
      `the page content security policy blocks the seimark worker; allow worker-src blob: (${String(e)})`,
    );
  } finally {
    URL.revokeObjectURL(url);
  }
}
