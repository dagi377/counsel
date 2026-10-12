import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { initTheme } from "../theme/theme";
import { StyleGuide } from "./StyleGuide";

initTheme();
createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <StyleGuide />
  </StrictMode>,
);
