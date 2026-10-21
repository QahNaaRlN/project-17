export interface Stand {
  apiOrigin: string;
  studioOrigin: string;
  project: string;
  projectId: string;
  credential: string;
  tokenFile: string;
  roles: Record<"designer" | "editor" | "lead" | "admin", string>;
  stores: Record<"staging" | "production", { origin: string; port: number; configFile: string }>;
  purge: { origin: string; token: string };
  github: { origin: string; control: string };
  directory: string;
  startStore(environment: "staging" | "production"): Promise<void>;
  stop(): void;
}
export const repo: string;
export const project: string;
export function ready(url: string, options?: { attempts?: number; any?: boolean }): Promise<void>;
export function startStand(options?: {
  directory?: string;
  binary?: string;
  database?: string;
  basePort?: number;
}): Promise<Stand>;
