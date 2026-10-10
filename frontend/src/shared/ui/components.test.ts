import { screen, waitFor, within } from "@testing-library/vue";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { ApiError } from "@/shared/api";
import { toast } from "@/shared/lib";
import Harness from "@/test/components/UiHarness.vue";
import { renderWithApp } from "@/test/render";
import CodeBlock from "./CodeBlock.vue";
import IconButton from "./IconButton.vue";

const rows = [
  { id: "codex", name: "Codex CLI" },
  { id: "claude-code", name: "Claude Code" },
];

describe("Switch", () => {
  it("is a labelled switch that toggles with the keyboard", async () => {
    await renderWithApp(Harness, { props: { part: "switch" } });
    const user = userEvent.setup();
    const control = screen.getByRole("switch", { name: "Auto refresh" });
    expect(control).toHaveAttribute("aria-checked", "false");
    control.focus();
    await user.keyboard(" ");
    expect(control).toHaveAttribute("aria-checked", "true");
    expect(screen.getByText("Enabled: true")).toBeInTheDocument();
  });
});

describe("ConfirmDialog via confirm()", () => {
  it("resolves true on confirm and false on Escape", async () => {
    await renderWithApp(Harness, { props: { part: "confirm" } });
    const user = userEvent.setup();

    await user.click(screen.getByRole("button", { name: "Delete" }));
    const dialog = await screen.findByRole("alertdialog", { name: "Delete vendor?" });
    expect(dialog).toHaveAccessibleDescription("This cannot be undone.");
    await user.click(within(dialog).getByRole("button", { name: "Confirm" }));
    expect(await screen.findByText("Confirmed: true")).toBeInTheDocument();
    await waitFor(() => {
      expect(screen.queryByRole("alertdialog")).toBeNull();
    });

    await user.click(screen.getByRole("button", { name: "Delete" }));
    await screen.findByRole("alertdialog");
    await user.keyboard("{Escape}");
    expect(await screen.findByText("Confirmed: false")).toBeInTheDocument();
  });
});

describe("DataTable", () => {
  it("exposes sort state on the header and cycles the direction", async () => {
    await renderWithApp(Harness, { props: { part: "table", rows } });
    const user = userEvent.setup();
    const table = screen.getByRole("table", { name: "Applications" });
    const header = within(table).getByRole("columnheader", { name: "Name" });
    expect(header).toHaveAttribute("aria-sort", "none");
    expect(within(table).getByRole("columnheader", { name: "ID" })).not.toHaveAttribute(
      "aria-sort",
    );

    await user.click(within(header).getByRole("button", { name: "Name" }));
    expect(header).toHaveAttribute("aria-sort", "ascending");
    await user.click(within(header).getByRole("button", { name: "Name" }));
    expect(header).toHaveAttribute("aria-sort", "descending");
    expect(within(table).getAllByRole("row")).toHaveLength(3);
  });

  it("shows empty and error states", async () => {
    const empty = await renderWithApp(Harness, { props: { part: "table", rows: [] } });
    expect(screen.getByText("No results.")).toBeInTheDocument();
    empty.unmount();

    await renderWithApp(Harness, {
      props: {
        part: "table",
        rows: undefined,
        tableError: new ApiError({
          code: "STORAGE_UNAVAILABLE",
          message: "storage",
          status: 503,
          requestId: "0011223344556677",
        }),
      },
    });
    const alert = screen.getByRole("alert");
    expect(alert).toHaveTextContent("Server storage is unavailable. Try again later.");
    expect(alert).toHaveTextContent("0011223344556677");
    expect(within(alert).getByRole("button", { name: "Retry" })).toBeInTheDocument();
  });
});

describe("Pagination", () => {
  it("navigates pages with labelled buttons", async () => {
    await renderWithApp(Harness, { props: { part: "pagination" } });
    const user = userEvent.setup();
    const nav = screen.getByRole("navigation", { name: "Pagination" });
    expect(within(nav).getByText("Page 1 of 10")).toBeInTheDocument();
    await user.click(within(nav).getByRole("button", { name: "Next page" }));
    expect(screen.getByText("Current page 2")).toBeInTheDocument();
    await user.click(within(nav).getByRole("button", { name: "Page 10" }));
    expect(screen.getByText("Current page 10")).toBeInTheDocument();
    expect(within(nav).getByRole("button", { name: "Next page" })).toBeDisabled();
  });
});

describe("SortableList", () => {
  it("reorders with the keyboard and announces the move", async () => {
    await renderWithApp(Harness, { props: { part: "sortable" } });
    const user = userEvent.setup();
    screen.getByRole("button", { name: "Reorder Alpha" }).focus();
    await user.keyboard("{ArrowDown}");
    expect(screen.getByText("Order: Beta, Alpha, Gamma")).toBeInTheDocument();
    expect(screen.getByText("Alpha moved to position 2 of 3.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Reorder Alpha" })).toHaveFocus();
    await user.keyboard("{End}");
    expect(screen.getByText("Order: Beta, Gamma, Alpha")).toBeInTheDocument();
  });
});

describe("Combobox", () => {
  it("selects a suggestion with the keyboard and submits free text", async () => {
    await renderWithApp(Harness, { props: { part: "combobox" } });
    const user = userEvent.setup();
    const input = screen.getByRole("combobox", { name: "Search" });
    await user.type(input, "co");
    expect(await screen.findByRole("option", { name: "Codex CLI" })).toBeInTheDocument();
    // The first suggestion is highlighted while the list is open.
    await user.keyboard("{Enter}");
    expect(screen.getByText("Chosen: openai/codex")).toBeInTheDocument();

    // With the list closed, Enter submits the typed text.
    await user.type(input, "x");
    await user.keyboard("{Escape}{Enter}");
    expect(screen.getByText("Submitted: cox")).toBeInTheDocument();
  });
});

describe("Tabs", () => {
  it("switches panels with arrow keys", async () => {
    await renderWithApp(Harness, { props: { part: "tabs" } });
    const user = userEvent.setup();
    const first = screen.getByRole("tab", { name: "First" });
    expect(first).toHaveAttribute("aria-selected", "true");
    first.focus();
    await user.keyboard("{ArrowRight}");
    expect(screen.getByRole("tab", { name: "Second" })).toHaveAttribute("aria-selected", "true");
    expect(screen.getByRole("tabpanel")).toHaveTextContent("Second panel");
  });
});

describe("CodeBlock", () => {
  it("copies the code and confirms it", async () => {
    await renderWithApp(CodeBlock, { props: { code: "curl -fsSL https://x/install.sh | sh" } });
    // user-event installs a clipboard stub; observe it after setup.
    const user = userEvent.setup();
    const writeText = vi.spyOn(navigator.clipboard, "writeText");
    await user.click(screen.getByRole("button", { name: "Copy" }));
    expect(writeText).toHaveBeenCalledWith("curl -fsSL https://x/install.sh | sh");
    expect(await screen.findByText("Copied")).toBeInTheDocument();
  });
});

describe("Toaster", () => {
  it("announces notifications with their request ID", async () => {
    await renderWithApp(Harness, { props: { part: "switch" } });
    toast({ tone: "error", title: "Saving failed", requestId: "abcdefabcdef0123" });
    const region = await screen.findByRole("region", { name: /Notifications/ });
    expect(await within(region).findByText("Saving failed")).toBeInTheDocument();
    expect(within(region).getByText("abcdefabcdef0123")).toBeInTheDocument();
  });
});

describe("IconButton", () => {
  it.each([false, true])("handles clicks (noTooltip: %s)", async (noTooltip) => {
    const onClick = vi.fn();
    await renderWithApp(IconButton, { props: { label: "Remove", noTooltip, onClick } });
    await userEvent.setup().click(screen.getByRole("button", { name: "Remove" }));
    expect(onClick).toHaveBeenCalledTimes(1);
  });
});
