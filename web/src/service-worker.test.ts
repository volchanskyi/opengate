import { afterEach, describe, expect, it, vi } from 'vitest';

type Listener = (event: unknown) => void;

const loadSource = import.meta.glob('../public/sw.js')['../public/sw.js']!;

// Loads the shipped worker afresh and keeps the listeners it registers on the global scope.
async function loadWorker() {
  const listeners = new Map<string, Listener>();
  vi.spyOn(globalThis, 'addEventListener').mockImplementation((type: string, listener: unknown) => {
    listeners.set(type, listener as Listener);
  });
  vi.resetModules();
  await loadSource();
  vi.restoreAllMocks();
  const dispatch = (type: string, event: unknown) => listeners.get(type)!(event);

  return {
    message(data: unknown, ports: unknown[] = []) {
      dispatch('message', { data, ports });
    },
    async request(id: string): Promise<Response> {
      let answer: Response | Promise<Response> | undefined;
      dispatch('fetch', {
        request: { url: `${location.origin}/__download/${id}`, mode: 'navigate' },
        respondWith: (response: Response | Promise<Response>) => {
          answer = response;
        },
      });
      return answer!;
    },
  };
}

const bytes = async (response: Response) => Array.from(new Uint8Array(await response.arrayBuffer()));

describe('service worker downloads', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('hands the browser every piece, including those arriving after the browser asked', async () => {
    const sw = await loadWorker();
    sw.message({ type: 'download-start', id: 'd1', name: 'backup log.tar', size: 3 });

    const response = await sw.request('d1');
    sw.message({ type: 'download-chunk', id: 'd1', data: new Uint8Array([1, 2]) });
    sw.message({ type: 'download-chunk', id: 'd1', data: new Uint8Array([3]) });
    sw.message({ type: 'download-end', id: 'd1' });

    expect(response.headers.get('Content-Disposition')).toBe(
      "attachment; filename*=UTF-8''backup%20log.tar",
    );
    expect(response.headers.get('X-Content-Type-Options')).toBe('nosniff');
    expect(response.headers.get('Content-Length')).toBe('3');
    expect(await bytes(response)).toEqual([1, 2, 3]);
  });

  it.each([
    ['отчёт 1.tar.gz', "UTF-8''%D0%BE%D1%82%D1%87%D1%91%D1%82%201.tar.gz"],
    ["it's (1)*.txt", "UTF-8''it%27s%20%281%29%2A.txt"],
  ])('names %s with every reserved character encoded', async (name, exact) => {
    const sw = await loadWorker();
    sw.message({ type: 'download-start', id: 'd6', name, size: 1 });

    const response = await sw.request('d6');

    expect(response.headers.get('Content-Disposition')).toBe(`attachment; filename*=${exact}`);
  });

  it('serves a download that finished before the browser asked for it', async () => {
    const sw = await loadWorker();
    sw.message({ type: 'download-start', id: 'd2', name: 'notes.txt', size: 2 });
    sw.message({ type: 'download-chunk', id: 'd2', data: new Uint8Array([7, 8]) });
    sw.message({ type: 'download-end', id: 'd2' });

    expect(await bytes(await sw.request('d2'))).toEqual([7, 8]);
  });

  it('tells the page on its port once it holds the download', async () => {
    const sw = await loadWorker();
    const port = { postMessage: vi.fn() };
    sw.message({ type: 'download-start', id: 'd3', name: 'a.bin', size: 1 }, [port]);
    expect(port.postMessage).toHaveBeenCalledWith('ready');
  });

  it('serves each download once, and answers an unknown one with not found', async () => {
    const sw = await loadWorker();
    sw.message({ type: 'download-start', id: 'd4', name: 'a.bin', size: 1 });
    expect((await sw.request('d4')).status).toBe(200);
    expect((await sw.request('d4')).status).toBe(404);
    sw.message({ type: 'download-end', id: 'd4' });
    expect((await sw.request('d4')).status).toBe(404);
    expect((await sw.request('nobody')).status).toBe(404);
  });

  it('fails the browser’s download when the session ends it', async () => {
    const sw = await loadWorker();
    sw.message({ type: 'download-start', id: 'd5', name: 'a.bin', size: 4 });
    const response = await sw.request('d5');
    sw.message({ type: 'download-chunk', id: 'd5', data: new Uint8Array([1]) });
    sw.message({ type: 'download-abort', id: 'd5' });

    await expect(response.arrayBuffer()).rejects.toThrow('the session ended the download');
    expect((await sw.request('d5')).status).toBe(404);
  });
});
