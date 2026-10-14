import { createRoot } from "react-dom/client";
import { StudioApp } from "./account.js";
import "./style.css";
import "./shell.css";
createRoot(document.getElementById("root")!).render(<StudioApp />);
