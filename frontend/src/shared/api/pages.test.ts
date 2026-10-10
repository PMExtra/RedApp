import { describe, expect, it } from "vitest";
import { fetchAllPages } from "./pages";

describe("fetchAllPages", () => {
  it("reads pages in order until the last one", async () => {
    const pages: number[] = [];
    const result = await fetchAllPages((page) => {
      pages.push(page);
      return Promise.resolve({ items: [page * 10, page * 10 + 1], total: 5, total_pages: 3 });
    });
    expect(pages).toEqual([1, 2, 3]);
    expect(result).toEqual({ items: [10, 11, 20, 21, 30, 31], total: 5 });
  });

  it("stops at an empty page when the list shrank meanwhile", async () => {
    const result = await fetchAllPages((page) =>
      Promise.resolve({ items: page === 1 ? ["a"] : [], total: 1, total_pages: 9 }),
    );
    expect(result).toEqual({ items: ["a"], total: 1 });
  });
});
