import { hydrateRoot } from "react-dom/client";
import { StoreApp } from "./app.js";
import type { StoreModel } from "./model.js";
import "./styles.css";

const root = document.getElementById("root");
const initial = document.getElementById("store-model");
if (!root || !initial?.textContent) throw new Error("STORE_BOOTSTRAP_MISSING");
hydrateRoot(root, <StoreApp initialModel={JSON.parse(initial.textContent) as StoreModel} />);
