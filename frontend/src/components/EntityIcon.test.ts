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
