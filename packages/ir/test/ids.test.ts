import { describe, expect, it } from "vitest";
import { generateNodeId, isNodeId } from "../src/index.js";

describe("node ids", () => {
  it("генерирует ID в формате n_ + 8 символов [a-z0-9]", () => {
    for (let i = 0; i < 1000; i++) {
      const id = generateNodeId();
      expect(id).toMatch(/^n_[a-z0-9]{8}$/);
      expect(isNodeId(id)).toBe(true);
    }
  });

  it("не возвращает занятые ID", () => {
    const taken = new Set<string>();
    for (let i = 0; i < 5000; i++) taken.add(generateNodeId(taken));
    expect(taken.size).toBe(5000);
  });

  it("проверяет формат", () => {
    expect(isNodeId("n_root")).toBe(true);
    expect(isNodeId("abc")).toBe(false);
    expect(isNodeId("n_with space")).toBe(false);
    expect(isNodeId("x".repeat(33))).toBe(false);
    expect(isNodeId(42)).toBe(false);
  });
});
