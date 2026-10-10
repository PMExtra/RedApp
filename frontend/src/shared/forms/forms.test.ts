import { h } from "vue";
import { RouterView } from "vue-router";
import { screen, waitFor } from "@testing-library/vue";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import DraftPage from "@/test/components/DraftPage.vue";
import { renderWithApp } from "@/test/render";

const routes = [
  { path: "/edit", component: DraftPage },
  { path: "/other", component: { render: () => h("p", "Other page") } },
];

async function renderDraft() {
  const rendered = await renderWithApp({ render: () => h(RouterView) }, { path: "/edit", routes });
  return { ...rendered, user: userEvent.setup(), input: await screen.findByLabelText("Title") };
}

describe("forms", () => {
  it("shows translated zod messages after the field is touched", async () => {
    const { user, input } = await renderDraft();
    await user.clear(input);
    await user.type(input, "Too long");
    await user.tab();
    expect(await screen.findByText("At most 5 characters")).toBeInTheDocument();
    expect(input).toHaveAttribute("aria-invalid", "true");
  });

  it("asks before leaving a dirty draft and stays when the user keeps editing", async () => {
    const { user, input, router } = await renderDraft();
    await user.type(input, "!");
    void router.push("/other");
    await user.click(await screen.findByRole("button", { name: "Keep editing" }));
    await waitFor(() => {
      expect(screen.queryByRole("alertdialog")).toBeNull();
    });
    expect(router.currentRoute.value.path).toBe("/edit");
    expect(screen.getByLabelText("Title")).toHaveValue("Saved!");

    void router.push("/other");
    await user.click(await screen.findByRole("button", { name: "Discard" }));
    expect(await screen.findByText("Other page")).toBeInTheDocument();
  });

  it("leaves without asking when nothing changed", async () => {
    const { router } = await renderDraft();
    await router.push("/other");
    expect(await screen.findByText("Other page")).toBeInTheDocument();
    expect(screen.queryByRole("alertdialog")).toBeNull();
  });
});
