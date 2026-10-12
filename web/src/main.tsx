import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./app/App";
import { ToastProvider, initTheme } from "./ui";

// The stored theme applies before React renders, so the first paint has the right colours.
initTheme();

const root = document.getElementById("root");
if (root) {
  createRoot(root).render(
    <StrictMode>
      <ToastProvider>
        <App />
      </ToastProvider>
    </StrictMode>,
  );
}
