export interface DesignOptions {
  breakpoints?: Record<string, number>;
  tokens?: Partial<Record<"spacing" | "colors" | "radius" | "container", Record<string, string>>>;
}
const namePattern = /^[a-z][A-Za-z0-9]*$/;
function length(value: unknown): string {
  if (typeof value === "number" && Number.isFinite(value) && value >= 0 && value <= 4000)
    return `${value}px`;
  if (
    typeof value === "string" &&
    /^(0|\d{1,4}(\.\d{1,3})?(px|rem|%|vw))$/.test(value) &&
    parseFloat(value) <= 4000 &&
    (!value.endsWith("%") || parseFloat(value) <= 100)
  )
    return value;
  throw new Error("CMS_DESIGN_INVALID");
}
function color(value: unknown): string {
  if (
    typeof value === "string" &&
    /^(#[0-9a-fA-F]{3}|#[0-9a-fA-F]{6}|#[0-9a-fA-F]{8}|transparent|currentColor)$/.test(value)
  )
    return value;
  throw new Error("CMS_DESIGN_INVALID");
}
export function designDeclarations(
  property: string,
  value: unknown,
  options: DesignOptions,
): string {
  const spacing: Record<string, string> = {
    padding: "padding",
    paddingX: "padding-inline",
    paddingY: "padding-block",
    paddingTop: "padding-top",
    paddingBottom: "padding-bottom",
    paddingLeft: "padding-left",
    paddingRight: "padding-right",
    gap: "gap",
    rowGap: "row-gap",
    columnGap: "column-gap",
    marginTop: "margin-top",
    marginBottom: "margin-bottom",
  };
  let category: "spacing" | "colors" | "radius" | "container";
  let cssProperty: string;
  if (Object.hasOwn(spacing, property)) {
    category = "spacing";
    cssProperty = spacing[property]!;
  } else if (property === "color" || property === "background") {
    category = "colors";
    cssProperty = property;
  } else if (property === "radius") {
    category = "radius";
    cssProperty = "border-radius";
  } else if (property === "width" || property === "maxWidth") {
    category = "container";
    cssProperty = property === "width" ? "width" : "max-width";
  } else if (
    property === "textAlign" &&
    ["left", "center", "right", "justify"].includes(String(value))
  )
    return `text-align:${String(value)}`;
  else if (property === "hidden" && typeof value === "boolean")
    return `display:${value ? "none" : "revert"}`;
  else if (property === "direction" && (value === "vertical" || value === "horizontal"))
    return `flex-direction:${value === "vertical" ? "column" : "row"}`;
  else if (property === "wrap" && typeof value === "boolean")
    return `flex-wrap:${value ? "wrap" : "nowrap"}`;
  else throw new Error("CMS_DESIGN_UNSUPPORTED");
  if (
    (property === "width" || property === "maxWidth") &&
    ["auto", "full", "fit"].includes(String(value))
  )
    return `${cssProperty}:${value === "full" ? "100%" : value === "fit" ? "fit-content" : "auto"}`;
  const validate = category === "colors" ? color : length;
  if (typeof value === "string" && namePattern.test(value)) {
    const group = options.tokens?.[category];
    if (group && Object.hasOwn(group, value)) {
      const resolved = validate(group[value]);
      return `--cms-${category}-${value}:${resolved};${cssProperty}:var(--cms-${category}-${value})`;
    }
  }
  if (
    value !== null &&
    typeof value === "object" &&
    Object.hasOwn(value, "raw") &&
    Object.keys(value).length === 1
  )
    return `${cssProperty}:${validate((value as { raw: unknown }).raw)}`;
  // Lengths/colors require a token or an explicit raw value.
  throw new Error("CMS_DESIGN_INVALID");
}
export function atom(declarations: string, minWidth?: number): { className: string; css: string } {
  let hash = 14695981039346656037n;
  for (const char of `${minWidth ?? 0}:${declarations}`)
    hash = BigInt.asUintN(64, (hash ^ BigInt(char.charCodeAt(0))) * 1099511628211n);
  const className = `c-${hash.toString(16)}`;
  const rule = `.${className}{${declarations}}`;
  return {
    className,
    css: minWidth === undefined ? rule : `@media(min-width:${minWidth}px){${rule}}`,
  };
}
export function responsive(
  value: unknown,
  options: DesignOptions,
): [unknown, number | undefined][] {
  if (
    value === null ||
    typeof value !== "object" ||
    Array.isArray(value) ||
    Object.hasOwn(value, "raw")
  )
    return [[value, undefined]];
  if (!Object.hasOwn(value, "base")) throw new Error("CMS_RESPONSIVE_INVALID");
  return Object.entries(value)
    .map(([key, entry]): [unknown, number | undefined] => {
      if (key === "base") return [entry, undefined];
      const point =
        options.breakpoints && Object.hasOwn(options.breakpoints, key)
          ? options.breakpoints[key]
          : undefined;
      if (!namePattern.test(key) || !Number.isInteger(point) || point! < 1 || point! > 4000)
        throw new Error("CMS_BREAKPOINT_INVALID");
      return [entry, point];
    })
    .sort((a, b) => (a[1] ?? 0) - (b[1] ?? 0));
}
