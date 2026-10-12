import { describe, expect, it } from "vitest";
import { formatDate, formatDateTime, torontoDay } from "./time";

describe("dates are shown in America/Toronto", () => {
  const cases: { name: string; at: string; date: string; dateTime: string; day: string }[] = [
    {
      name: "UTC midnight is the previous Toronto evening",
      at: "2026-10-06T00:30:00Z",
      date: "5 Oct 2026",
      dateTime: "5 Oct 2026, 20:30 EDT",
      day: "2026-10-05",
    },
    { name: "winter time is EST", at: "2026-01-15T17:05:00Z", date: "15 Jan 2026", dateTime: "15 Jan 2026, 12:05 EST", day: "2026-01-15" },
    { name: "offset input", at: "2026-10-01T09:00:00-04:00", date: "1 Oct 2026", dateTime: "1 Oct 2026, 09:00 EDT", day: "2026-10-01" },
    { name: "unreadable", at: "not a date", date: "Unknown date", dateTime: "Unknown date", day: "" },
  ];
  for (const c of cases) {
    it(c.name, () => {
      expect(formatDate(c.at)).toBe(c.date);
      expect(formatDateTime(c.at)).toBe(c.dateTime);
      expect(torontoDay(c.at)).toBe(c.day);
    });
  }
});
