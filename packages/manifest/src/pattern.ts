import { RE2JS } from "re2js";
import { hasInvalidUnicode } from "./unicode.js";

/** MF-001, 04 §4.2: bounded portable RE2 grammar, search semantics, Unicode scalars. */
export function compilePattern(pattern: string): RE2JS | undefined {
  if (
    hasInvalidUnicode(pattern) ||
    pattern.includes("\0") ||
    new TextEncoder().encode(pattern).length > 1024
  )
    return undefined;
  const chars = Array.from(pattern);
  if (chars.length > 500) return undefined;
  let inClass = false;
  let classStart = -1;
  for (let i = 0; i < chars.length; i++) {
    const c = chars[i]!;
    if (c === "\\") {
      const escaped = chars[++i];
      if (escaped === undefined) return undefined;
      const asciiPunctuation =
        escaped.charCodeAt(0) >= 33 && escaped.charCodeAt(0) <= 126 && !/[a-zA-Z0-9]/.test(escaped);
      if (!"afnrtvdDsSwWbBAz".includes(escaped) && !asciiPunctuation) return undefined;
    } else if (c === "[") {
      // POSIX/Unicode properties are excluded to avoid Unicode-table dependencies.
      if (inClass && chars[i + 1] === ":") return undefined;
      if (!inClass) classStart = i;
      inClass = true;
    } else if (c === "]" && inClass) {
      // A leading ] (also after ^) is a literal member of the class.
      if (i !== classStart + 1 && !(chars[classStart + 1] === "^" && i === classStart + 2))
        inClass = false;
    } else if (!inClass && c === "(" && chars[i + 1] === "?" && chars[i + 2] !== ":") {
      return undefined;
    }
  }
  try {
    return RE2JS.compile(pattern, RE2JS.DISABLE_UNICODE_GROUPS);
  } catch {
    return undefined;
  }
}
