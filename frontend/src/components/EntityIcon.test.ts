import { mount } from "@vue/test-utils";
import { expect, it } from "vitest";
import EntityIcon from "./EntityIcon.vue";
import { entityIconSizes } from "../entityIconSizes";

it("keeps image, empty and failed icons in the same square at every scene size", async () => {
  for (const [size, pixels] of Object.entries(entityIconSizes)) {
    const wrapper = mount(EntityIcon, { props: { size: size as keyof typeof entityIconSizes } });
    expect(wrapper.attributes("style")).toContain(`width: ${pixels}px`);
    expect(wrapper.attributes("style")).toContain(`height: ${pixels}px`);
    expect(wrapper.find("svg").exists()).toBe(true);
    for (const src of ["/assets/icons/wide.svg", "/assets/icons/tall.png"]) {
      await wrapper.setProps({ src });
      const image = wrapper.get("img");
      expect(image.attributes("src")).toBe(src);
      expect(image.attributes("width")).toBe(String(pixels));
      expect(image.attributes("height")).toBe(String(pixels));
      await image.trigger("error");
      expect(wrapper.find("img").exists()).toBe(false);
      expect(wrapper.find("svg").exists()).toBe(true);
      expect(wrapper.attributes("style")).toContain(`width: ${pixels}px`);
    }
    wrapper.unmount();
  }
});

it("uses bounded proportional vendor marks and hides absent or failed logos without a visible frame", async () => {
  const wrapper = mount(EntityIcon, { props: { vendor: true } });
  expect(wrapper.find(".entity-icon").exists()).toBe(false);
  for (const src of ["/wide.svg", "/tall.svg"]) {
    await wrapper.setProps({ src });
    expect(wrapper.classes()).toContain("entity-icon--vendor");
    expect(wrapper.attributes("style")).toContain("height: 32px");
    expect(wrapper.attributes("style")).not.toContain("width:");
    expect(wrapper.get("img").attributes("width")).toBeUndefined();
    await wrapper.get("img").trigger("error");
    expect(wrapper.find(".entity-icon").exists()).toBe(false);
    expect(wrapper.find("svg").exists()).toBe(false);
  }
  await wrapper.setProps({ src: "" });
  expect(wrapper.find(".entity-icon").exists()).toBe(false);
  wrapper.unmount();
});
