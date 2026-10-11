import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { features } from "./app/features";

function App() {
  return (
    <nav aria-label="Features">
      <ul>
        {features.map((f) => (
          <li key={f.id}>
            <a href={f.path}>{f.title}</a>
          </li>
        ))}
      </ul>
    </nav>
  );
}

const root = document.getElementById("root");
if (root) {
  createRoot(root).render(
    <StrictMode>
      <App />
    </StrictMode>,
  );
}
