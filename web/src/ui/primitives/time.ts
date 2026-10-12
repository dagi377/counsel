// Business time zone. Every date and time the product shows is in America/Toronto, whatever
// the browser is set to. Server timestamps arrive as RFC 3339 strings (or Date objects).
export const ZONE = "America/Toronto";

export class TimeError extends Error {}

export type DateStyle = "date" | "datetime" | "time" | "month";

const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

interface Parts {
  year: number;
  month: number; // 1-12
  day: number;
  hour: number; // 0-23
  minute: number;
}

const partsFmt = new Intl.DateTimeFormat("en-CA", {
  timeZone: ZONE,
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
  hour: "2-digit",
  minute: "2-digit",
  hourCycle: "h23",
});

export function toDate(value: string | Date): Date {
  const d = value instanceof Date ? value : new Date(value);
  if (Number.isNaN(d.getTime())) throw new TimeError(`time: not a date: ${String(value)}`);
  return d;
}

// zoneParts returns the wall-clock parts of an instant in America/Toronto.
export function zoneParts(value: string | Date): Parts {
  const d = toDate(value);
  const p: Record<string, string> = {};
  for (const x of partsFmt.formatToParts(d)) p[x.type] = x.value;
  return { year: Number(p.year), month: Number(p.month), day: Number(p.day), hour: Number(p.hour) % 24, minute: Number(p.minute) };
}

const two = (n: number): string => String(n).padStart(2, "0");

function timeText(p: Parts): string {
  const h12 = p.hour % 12 === 0 ? 12 : p.hour % 12;
  return `${h12}:${two(p.minute)} ${p.hour < 12 ? "a.m." : "p.m."}`;
}

// formatDate renders an instant in America/Toronto: "5 Oct 2026", "5 Oct 2026, 7:30 p.m.",
// "7:30 p.m." or "Oct 2026". A date-only string (YYYY-MM-DD) is a calendar date already in the
// business zone and renders as that day, never shifted.
export function formatDate(value: string | Date, style: DateStyle = "date"): string {
  if (typeof value === "string" && /^\d{4}-\d{2}-\d{2}$/.test(value)) {
    const [y, m, d] = value.split("-").map(Number);
    if (m < 1 || m > 12) throw new TimeError(`time: bad month in ${value}`);
    if (style === "month") return `${MONTHS[m - 1]} ${y}`;
    return `${d} ${MONTHS[m - 1]} ${y}`;
  }
  const p = zoneParts(value);
  const date = `${p.day} ${MONTHS[p.month - 1]} ${p.year}`;
  switch (style) {
    case "date":
      return date;
    case "month":
      return `${MONTHS[p.month - 1]} ${p.year}`;
    case "time":
      return timeText(p);
    case "datetime":
      return `${date}, ${timeText(p)}`;
  }
}

// isoDate returns the America/Toronto calendar date of an instant as YYYY-MM-DD.
export function isoDate(value: string | Date): string {
  const p = zoneParts(value);
  return `${p.year}-${two(p.month)}-${two(p.day)}`;
}
