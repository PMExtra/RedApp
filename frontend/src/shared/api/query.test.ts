import { describe, expect, it } from "vitest";
import { ApiError, type AnyErrorCode } from "./errors";
import { createQueryClient, queryKey } from "./query";

describe("query client", () => {
  it.each<[AnyErrorCode, boolean]>([
    ["ENTITY_DELETED", true],
    ["APPLICATION_DELETE_PENDING", true],
    ["APPLICATION_DISABLED", true],
    ["VALIDATION_FAILED", false],
  ])(
    "after a write fails with %s, re-reads the vendor and application: %s",
    async (code, reread) => {
      const notified: unknown[] = [];
      const client = createQueryClient({ onMutationError: (error) => notified.push(error) });
      const appKey = queryKey("getApp", { vendor: "example", app: "tools" });
      const vendorKey = queryKey("getVendor", { vendor: "example" });
      client.setQueryData(appKey, { id: "tools" });
      client.setQueryData(vendorKey, { id: "example" });

      const error = new ApiError({ code, message: code, status: 409 });
      const mutation = client.getMutationCache().build(client, {
        mutationFn: () => Promise.reject(error),
      });
      await expect(mutation.execute(undefined)).rejects.toBe(error);

      expect(client.getQueryState(appKey)?.isInvalidated).toBe(reread);
      expect(client.getQueryState(vendorKey)?.isInvalidated).toBe(reread);
      expect(notified).toEqual([error]);
    },
  );
});
