export type CatalogPage<T> = {
  items: T[];
  page: number;
  pageCount: number;
  start: number;
  end: number;
  total: number;
};

export function paginateCatalog<T>(
  items: T[],
  requestedPage: number,
  requestedPageSize: number,
): CatalogPage<T> {
  const pageSize = Math.max(1, Math.floor(requestedPageSize));
  const pageCount = Math.max(1, Math.ceil(items.length / pageSize));
  const normalizedPage = Number.isFinite(requestedPage) ? Math.floor(requestedPage) : 1;
  const page = Math.min(pageCount, Math.max(1, normalizedPage));
  const start = (page - 1) * pageSize;
  const end = Math.min(start + pageSize, items.length);

  return {
    items: items.slice(start, end),
    page,
    pageCount,
    start,
    end,
    total: items.length,
  };
}
