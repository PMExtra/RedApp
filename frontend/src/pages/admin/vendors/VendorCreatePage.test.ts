import { screen, waitFor } from "@testing-library/vue";
import { describe, expect, it } from "vitest";
import type { Schema } from "@/shared/api";
import { recorder, renderAdminPage } from "@/test/directory";
import { vendor, vendorConfiguration } from "@/test/factories/directory";
import { apiError, mockApi, useHandlers } from "@/test/msw";

describe("vendor creation", () => {
  it("validates the form, reports a taken ID and opens the new vendor", async () => {
    const created = recorder<Schema<"VendorCreate">>();
    let taken = true;
    useHandlers(
      mockApi("post", "/admin/api/vendors", async ({ request }) => {
        const body = await created.record(request);
        if (taken) return apiError("ALREADY_EXISTS");
        return vendor({ id: body.id, name: body.name });
      }),
      mockApi("get", "/admin/api/vendors/{vendor}", () => vendor({ id: "acme" })),
      mockApi("get", "/admin/api/vendors/{vendor}/configuration", () => vendorConfiguration()),
    );
    const { router, user } = await renderAdminPage("/admin/vendors/new");
    const submit = await screen.findByRole("button", { name: "Create vendor" });

    await user.click(submit);
    expect(await screen.findAllByText("Enter a name.")).toHaveLength(2);
    const id = screen.getByLabelText(/^Vendor ID/);
    expect(id).toHaveAttribute("aria-invalid", "true");
    expect(created.calls).toHaveLength(0);

    await user.type(id, "admin");
    await user.type(screen.getByLabelText(/^Name \(English\)/), "Acme");
    await user.type(screen.getByLabelText(/^Name \(简体中文\)/), "极致");
    await user.click(submit);
    expect(id).toHaveAccessibleDescription(/not admin, api/);
    expect(created.calls).toHaveLength(0);

    await user.clear(id);
    await user.type(id, "acme");
    await user.click(submit);
    expect(await screen.findByText("A vendor with this ID already exists.")).toBeInTheDocument();
    expect(created.calls[0]?.body).toEqual({
      id: "acme",
      name: { en: "Acme", "zh-CN": "极致" },
      description: { en: "", "zh-CN": "" },
      icon: "",
      localized_icons: { en: "", "zh-CN": "" },
      enabled: true,
    });

    taken = false;
    await user.click(submit);
    await waitFor(() => {
      expect(router.currentRoute.value.fullPath).toBe("/admin/vendors/acme/settings");
    });
    // The finished form does not ask about unsaved changes.
    expect(screen.queryByRole("alertdialog")).toBeNull();
  });

  it("uploads a language logo and sends its stored path", async () => {
    const created = recorder<Schema<"VendorCreate">>();
    const csrf: (string | null)[] = [];
    const stored = `/assets/icons/${"c".repeat(64)}.png`;
    useHandlers(
      mockApi("post", "/admin/api/icons", ({ request }) => {
        csrf.push(request.headers.get("X-CSRF-Token"));
        return { icon: stored };
      }),
      mockApi("post", "/admin/api/vendors", async ({ request }) => {
        const body = await created.record(request);
        return vendor({ id: body.id });
      }),
      mockApi("get", "/admin/api/vendors/{vendor}", () => vendor({ id: "acme" })),
      mockApi("get", "/admin/api/vendors/{vendor}/configuration", () => vendorConfiguration()),
    );
    const { user } = await renderAdminPage("/admin/vendors/new");
    const upload = await screen.findByLabelText("Chinese logo: Upload image", {
      selector: "input",
    });
    await user.upload(upload, new File(["png"], "logo.png", { type: "image/png" }));
    expect(await screen.findByRole("button", { name: "Chinese logo: Remove" })).toBeInTheDocument();

    await user.type(screen.getByLabelText(/^Vendor ID/), "acme");
    await user.type(screen.getByLabelText(/^Name \(English\)/), "Acme");
    await user.type(screen.getByLabelText(/^Name \(简体中文\)/), "极致");
    await user.click(screen.getByRole("button", { name: "Create vendor" }));
    await waitFor(() => {
      expect(created.calls).toHaveLength(1);
    });
    expect(created.calls[0]?.body.localized_icons).toEqual({ en: "", "zh-CN": stored });
    expect(created.calls[0]?.body.icon).toBe("");
    expect(csrf).toEqual(["c".repeat(64)]);
  });

  it("asks before uploading a sanitized SVG", async () => {
    const queries: string[] = [];
    const stored = `/assets/icons/${"d".repeat(64)}.svg`;
    useHandlers(
      mockApi("post", "/admin/api/icons", ({ request }) => {
        const query = new URL(request.url).search;
        queries.push(query);
        if (query === "?sanitize=true") return { icon: stored };
        return apiError("ICON_SANITIZE_REQUIRED", { details: ["<script>", "<path onclick>"] });
      }),
    );
    const { user } = await renderAdminPage("/admin/vendors/new");
    const upload = await screen.findByLabelText("Default logo: Upload image", {
      selector: "input",
    });
    const file = new File(["<svg/>"], "logo.svg", { type: "image/svg+xml" });

    await user.upload(upload, file);
    const dialog = await screen.findByRole("alertdialog");
    expect(dialog).toHaveTextContent("<script>, <path onclick>");
    await user.click(screen.getByRole("button", { name: "Cancel" }));
    await waitFor(() => {
      expect(screen.queryByRole("alertdialog")).toBeNull();
    });
    expect(screen.queryByRole("button", { name: "Default logo: Remove" })).toBeNull();

    await user.upload(upload, file);
    await user.click(await screen.findByRole("button", { name: "Sanitize and upload" }));
    expect(await screen.findByRole("button", { name: "Default logo: Remove" })).toBeInTheDocument();
    expect(queries).toEqual(["", "", "?sanitize=true"]);
  });
});
