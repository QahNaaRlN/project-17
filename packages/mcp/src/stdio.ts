#!/usr/bin/env node
import { createProxy } from "./proxy.js";
import { once } from "node:events";
// Credentials come from the process environment, never arguments or protocol logs.
const forward = createProxy(process.env);
let buffer = "";
process.stdin.setEncoding("utf8");
for await (const chunk of process.stdin) {
  buffer += String(chunk);
  if (Buffer.byteLength(buffer) > 2 * 1024 * 1024) {
    process.exitCode = 1;
    break;
  }
  let end: number;
  while ((end = buffer.indexOf("\n")) >= 0) {
    const line = buffer.slice(0, end).trim();
    buffer = buffer.slice(end + 1);
    if (!line) continue;
    const result = await forward(line);
    if (result !== undefined && !process.stdout.write(result + "\n"))
      await once(process.stdout, "drain");
  }
}
