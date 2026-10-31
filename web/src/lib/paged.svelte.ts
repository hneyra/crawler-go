// generic paged loader for lists. api returns {items,total,page,per_page}.
import { errorMessage } from '#lib/api/problem.js';

export interface PageResult<T> {
  items: T[];
  total: number;
  page: number;
  per_page: number;
}

export class Paged<T> {
  items = $state<T[]>([]);
  total = $state(0);
  page = $state(1);
  perPage = $state(20);
  loading = $state(true);
  error = $state<string | null>(null);

  constructor(private fetchPage: (page: number) => Promise<PageResult<T>>) {}

  get totalPages() {
    return Math.max(1, Math.ceil(this.total / this.perPage));
  }

  private seq = 0;

  // latest call wins: slower earlier responses are dropped.
  load = async (page = this.page) => {
    const mine = ++this.seq;
    this.loading = true;
    try {
      const r = await this.fetchPage(page);
      if (mine !== this.seq) return;
      this.items = r.items;
      this.total = r.total;
      this.perPage = r.per_page;
      this.page = r.page || page;
      this.error = null;
    } catch (e) {
      if (mine === this.seq) this.error = errorMessage(e);
    } finally {
      if (mine === this.seq) this.loading = false;
    }
  };
}
