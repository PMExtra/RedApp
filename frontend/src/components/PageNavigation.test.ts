import { mount } from "@vue/test-utils";
import { expect, it } from "vitest";
import PageNavigation from "./PageNavigation.vue";
it("submits on Enter/form or blur once, ignores the same page and validates changed totals", async () => {
  const w = mount(PageNavigation, { props: { label: "Pages", page: 1, total: 50, totalPages: 5, previous: false, next: true, loading: false } });
  expect(w.find('[aria-label="Go"]').exists()).toBe(false);
  await w.get('input').setValue('3'); await w.get('input').trigger('keydown', { key: 'Enter' }); await w.get('input').trigger('blur');
  expect(w.emitted('go')).toEqual([[3]]);
  await w.setProps({ page: 3 }); await w.get('input').trigger('blur');
  expect(w.emitted('go')).toHaveLength(1);
  for (const value of ['', '0', '-1', '6', '1.5', 'abc', '9007199254740993']) {
    await w.get('input').setValue(value); await w.get('input').trigger('blur');
    expect(w.get('input').attributes('aria-invalid')).toBe('true');
  }
  expect(w.emitted('go')).toHaveLength(1);
  await w.get('input').setValue('5'); await w.get('input').trigger('blur'); expect(w.emitted('go')).toEqual([[3],[5]]);
  await w.setProps({ page: 1, totalPages: 1, total: 0 });
  expect(w.get('input').element.value).toBe('1'); expect(w.get('input').attributes('disabled')).toBeDefined();
  await w.get('form').trigger('submit'); expect(w.emitted('go')).toHaveLength(2);
  w.unmount();
});
