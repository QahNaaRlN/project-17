import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createApi } from "./api.js";
import { createEditor } from "./editor.js";
import { Studio } from "./ui.js";
import "./style.css";
const api = createApi(),
  editor = createEditor(api);
createRoot(document.getElementById("root")!).render(
  <QueryClientProvider client={new QueryClient()}>
    <Studio api={api} editor={editor} />
  </QueryClientProvider>,
);
