// Page-level states every screen shares: loading, error with retry, not found and not
// available. They carry the copy and the semantics (role, aria-live); the look comes later.
// TODO(ui): render with the web/src/ui design-system components (EmptyState, Button,
// Skeleton) once they land; these plain elements keep the same text and roles.

export function Loading({ label }: { label: string }) {
  return (
    <p role="status" className="state state-loading">
      {label}…
    </p>
  );
}

export interface ErrorCopyLike {
  title: string;
  message: string;
  retryable: boolean;
}

// ErrorState shows what failed and, when trying again can help, a retry button.
export function ErrorState({ copy, onRetry }: { copy: ErrorCopyLike; onRetry?: () => void }) {
  return (
    <section role="alert" className="state state-error">
      <h1>{copy.title}</h1>
      <p>{copy.message}</p>
      {copy.retryable && onRetry ? (
        <button type="button" onClick={onRetry}>
          Try again
        </button>
      ) : null}
    </section>
  );
}

// NotFound names the address that matched nothing and offers a way back.
export function NotFound({ path, home }: { path: string; home: string | null }) {
  return (
    <section role="alert" className="state state-not-found">
      <h1>Page not found</h1>
      <p>
        No screen is registered for <code>{path}</code>.
      </p>
      {home ? <a href={home}>Go to home</a> : null}
    </section>
  );
}

// NotAvailable is a registered area whose screens are not part of this release yet.
export function NotAvailable({ title }: { title: string }) {
  return (
    <section className="state state-not-available">
      <h1>{title}</h1>
      <p>This area is not part of this release yet.</p>
    </section>
  );
}
