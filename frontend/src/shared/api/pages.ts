/** A page of a page-numbered list (`?page=&limit=`). */
export interface NumberedPage<T> {
  items: T[];
  total: number;
  total_pages: number;
}

/**
 * Reads every page of a page-numbered list, in order, for pickers and strips
 * that need the whole list. Stops at `total_pages` or at the first empty
 * page (the list shrank meanwhile).
 */
export async function fetchAllPages<T>(
  fetchPage: (page: number) => Promise<NumberedPage<T>>,
): Promise<{ items: T[]; total: number }> {
  const items: T[] = [];
  for (let page = 1; ; page++) {
    const result = await fetchPage(page);
    items.push(...result.items);
    if (page >= result.total_pages || result.items.length === 0) {
      return { items, total: result.total };
    }
  }
}
