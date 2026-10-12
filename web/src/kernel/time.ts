// Dates and times are shown in the business time zone, America/Toronto, whatever zone the
// browser is in: a deadline or a shared-on date must not move by a day for a user
// whose laptop is set to UTC. This is the one place that formats an instant for display.

export const BUSINESS_ZONE = "America/Toronto";
const LOCALE = "en-CA";

const dateFmt = new Intl.DateTimeFormat(LOCALE, { timeZone: BUSINESS_ZONE, day: "numeric", month: "short", year: "numeric" });
const timeFmt = new Intl.DateTimeFormat(LOCALE, {
  timeZone: BUSINESS_ZONE,
  hour: "2-digit",
  minute: "2-digit",
  hourCycle: "h23",
  timeZoneName: "short",
});
const isoDayFmt = new Intl.DateTimeFormat(LOCALE, { timeZone: BUSINESS_ZONE, year: "numeric", month: "2-digit", day: "2-digit" });

function parse(at: string | Date): Date | null {
  const d = typeof at === "string" ? new Date(at) : at;
  return Number.isNaN(d.getTime()) ? null : d;
}

function part(parts: Intl.DateTimeFormatPart[], type: Intl.DateTimeFormatPartTypes): string {
  return parts.find((p) => p.type === type)?.value ?? "";
}

// formatDate renders an instant as its Toronto calendar day, "5 Oct 2026". An unreadable
// value renders as "Unknown date" rather than "Invalid Date".
export function formatDate(at: string | Date): string {
  const d = parse(at);
  if (!d) return "Unknown date";
  const p = dateFmt.formatToParts(d);
  return `${part(p, "day")} ${part(p, "month")} ${part(p, "year")}`;
}

// formatDateTime renders an instant as "5 Oct 2026, 20:30 EDT" in Toronto time.
export function formatDateTime(at: string | Date): string {
  const d = parse(at);
  if (!d) return "Unknown date";
  return `${formatDate(d)}, ${timeFmt.format(d)}`;
}

// torontoDay is the Toronto calendar day of an instant as "2026-10-05", for a <time dateTime>
// attribute or for grouping. It returns "" for an unreadable value.
export function torontoDay(at: string | Date): string {
  const d = parse(at);
  if (!d) return "";
  const p = isoDayFmt.formatToParts(d);
  return `${part(p, "year")}-${part(p, "month")}-${part(p, "day")}`;
}
