import { SeimarkError } from './errors.ts';
import { type Framing, type NALUnit, joinNALUnits, nalType, nalUnits } from './nal.ts';
import { type Marker, decodeMarker, isFormatUUID } from './marker.ts';
import { parseSEI } from './sei.ts';

const NAL_SEI = 6;
const VCL_MIN = 1;
const VCL_MAX = 5;
const NAL_IDR = 5;

/** What `markersIn` found in one access unit. */
export interface ScanResult {
  /** Every seimark marker, in the order the SEI units appear. */
  markers: Marker[];
  /** SEI units that could not be parsed. They are skipped, not thrown. */
  warnings: SeimarkError[];
}

export function isMarkerSEI(unit: Uint8Array): boolean {
  if (nalType(unit) !== NAL_SEI) return false;
  try {
    return parseSEI(unit).some((m) => m.uuid !== null && isFormatUUID(m.uuid));
  } catch {
    return false;
  }
}

/**
 * Reads every seimark marker out of one access unit. An unparsable SEI unit
 * becomes a warning and the scan continues, so a foreign or damaged SEI never
 * costs the markers beside it.
 */
export function markersIn(au: Uint8Array, framing: Framing): ScanResult {
  const markers: Marker[] = [];
  const warnings: SeimarkError[] = [];

  for (const unit of nalUnits(au, framing)) {
    if (nalType(unit.data) !== NAL_SEI) continue;

    let messages;
    try {
      messages = parseSEI(unit.data);
    } catch (e) {
      warnings.push(e as SeimarkError);
      continue;
    }

    for (const m of messages) {
      if (m.uuid !== null && isFormatUUID(m.uuid)) {
        markers.push(decodeMarker(m.body));
      }
    }
  }

  return { markers, warnings };
}

/**
 * Returns the access unit without its seimark SEI units, leaving foreign SEI in
 * place. The input is returned unchanged when it carries no marker.
 */
export function stripMarkers(au: Uint8Array, framing: Framing): Uint8Array {
  const units = nalUnits(au, framing);
  const kept = units.filter((u) => !isMarkerSEI(u.data));

  return kept.length === units.length ? au : joinNALUnits(kept, framing);
}

/** Reports whether the access unit carries an IDR slice, so it is a keyframe. */
export function hasIDR(au: Uint8Array, framing: Framing): boolean {
  return nalUnits(au, framing).some((u) => nalType(u.data) === NAL_IDR);
}

export function firstVCLIndex(units: NALUnit[]): number {
  const i = units.findIndex((u) => {
    const t = nalType(u.data);

    return t >= VCL_MIN && t <= VCL_MAX;
  });

  if (i < 0) {
    throw new SeimarkError('no_vcl', 'access unit has no vcl nal unit');
  }

  return i;
}
