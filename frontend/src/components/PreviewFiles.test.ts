import { flushPromises, mount } from '@vue/test-utils';
import { afterEach, expect, it, vi } from 'vitest';
import PreviewFiles from './PreviewFiles.vue';
import { resetStores, response } from '../testSupport';
import { signedIn } from '../session';

afterEach(() => { vi.unstubAllGlobals(); signedIn.value = false; });
it('keeps cleanup item pages scoped to their source and cancels a late previous selection page', async () => {
  resetStores(); signedIn.value = true;
  let resolveOld: ((value: unknown) => void) | undefined;
  const item = (path: string) => ({ ordinal: 0, generation_id: 'gen', path, size_bytes: 1, result_status: 'pending' });
  const fetch = vi.fn((url: string, _init?: RequestInit) => {
    const request = new URL(url, 'https://admin.example');
    if (request.searchParams.has('cursor')) return new Promise((resolve) => { resolveOld = resolve; });
    return Promise.resolve(response({ items: [item(url.includes('/new-job/') ? '/new-selection.zip' : '/first.zip')], next_cursor: 'cursor/+=', total_files: 30, total_bytes: 30, state: 'ready' }));
  });
  vi.stubGlobal('fetch', fetch);
  const wrapper = mount(PreviewFiles, { props: { application: 'acme/files', kind: 'cleanup', jobId: 'old-job', sourceQuery: '?source_epoch=1' } });
  await flushPromises();
  expect(fetch.mock.calls[0]![0]).toBe('/admin/api/apps/acme/files/cache/cleanup/old-job/items?source_epoch=1&limit=25');
  await wrapper.get('[aria-label="Preview file pages"]').findAll('button').find((button) => button.attributes("aria-label") === 'Next page')!.trigger('click');
  const oldRequest = fetch.mock.calls.at(-1)![1]!;
  expect(new URL(fetch.mock.calls.at(-1)![0], 'https://admin.example').searchParams.get('cursor')).toBe('cursor/+=');
  await wrapper.setProps({ jobId: 'new-job', sourceQuery: '?source_epoch=2' }); await flushPromises();
  expect(oldRequest.signal?.aborted).toBe(true);
  resolveOld?.(response({ items: [item('/late-wrong-selection.zip')], next_cursor: null })); await flushPromises();
  expect(wrapper.text()).toContain('/new-selection.zip');
  expect(wrapper.text()).not.toContain('/late-wrong-selection.zip');
  expect(fetch.mock.calls.at(-1)![0]).toBe('/admin/api/apps/acme/files/cache/cleanup/new-job/items?source_epoch=2&limit=25');
  wrapper.unmount();
});
