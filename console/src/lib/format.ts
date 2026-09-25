import type { Machine } from "@/types";

const ZERO = "0001-01-01T00:00:00Z";
const WEEK = 7 * 24 * 60 * 60 * 1000;

export function neverSeen(m: Machine) {
  return !m.lastSeenAt || m.lastSeenAt === ZERO;
}

export function isStale(m: Machine, now = new Date()) {
  return neverSeen(m) || now.getTime() - new Date(m.lastSeenAt).getTime() > WEEK;
}

export function isOldKey(m: Machine, current?: string) {
  return Boolean(m.lastKeyId && current && m.lastKeyId !== current);
}

const rtf = new Intl.RelativeTimeFormat("en", { numeric: "auto" });
export function relative(iso: string, now = new Date()) {
  const diff = (new Date(iso).getTime() - now.getTime()) / 1000;
  const units: [Intl.RelativeTimeFormatUnit, number][] = [["day", 86400], ["hour", 3600], ["minute", 60]];
  for (const [unit, secs] of units) if (Math.abs(diff) >= secs) return rtf.format(Math.round(diff / secs), unit);
  return "just now";
}
