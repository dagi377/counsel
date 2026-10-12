import { formatDate, toDate, type DateStyle } from "./time";

export interface DateTimeProps {
  // value is an RFC 3339 instant, a Date, or a YYYY-MM-DD calendar date.
  value: string | Date | null | undefined;
  style?: DateStyle;
  // empty is shown when value is missing.
  empty?: string;
  className?: string;
}

// DateTime renders a <time> in America/Toronto. The machine-readable dateTime attribute keeps
// the original instant so tools and tests can read it.
export function DateTime({ value, style = "date", empty = "—", className }: DateTimeProps) {
  if (value === null || value === undefined || value === "") return <span className="ui-muted">{empty}</span>;
  const text = formatDate(value, style);
  const iso = typeof value === "string" && /^\d{4}-\d{2}-\d{2}$/.test(value) ? value : toDate(value).toISOString();
  const cls = ["ui-datetime", className].filter(Boolean).join(" ");
  if (style === "datetime") {
    const [date, time] = text.split(", ");
    return (
      <time className={cls} dateTime={iso}>
        {date}, <span className="ui-datetime__time">{time}</span>
      </time>
    );
  }
  return (
    <time className={cls} dateTime={iso}>
      {text}
    </time>
  );
}
