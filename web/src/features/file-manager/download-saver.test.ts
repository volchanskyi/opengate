import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { BUFFERED_LIMIT, openDownload } from './download-saver';

describe('openDownload', () => {
  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
    vi.useRealTimers();
    document.body.innerHTML = '';
  });

  describe('where the service worker controls the page', () => {
    const postMessage = vi.fn();

    beforeEach(() => {
      vi.useFakeTimers();
      postMessage.mockReset();
      vi.stubGlobal('navigator', { ...navigator, serviceWorker: { controller: { postMessage } } });
    });

    // The worker's half of the port the page sends with a download's start.
    const workerPort = () => (postMessage.mock.calls[0]![1] as MessagePort[])[0]!;

    it('streams each piece to the worker, which hands the file to the browser’s Downloads', async () => {
      const sink = openDownload('backup.tar', 300_000)!;
      expect(sink).not.toBeNull();

      const start = postMessage.mock.calls[0]![0] as { type: string; id: string; name: string; size: number };
      expect(start).toMatchObject({ type: 'download-start', name: 'backup.tar', size: 300_000 });
      expect(document.querySelector('iframe')).toBeNull();

      workerPort().postMessage('ready');
      await vi.waitFor(() => expect(document.querySelector('iframe')).not.toBeNull());
      const frame = document.querySelector('iframe');
      expect(frame?.getAttribute('src')).toBe(`/__download/${start.id}`);
      expect(frame?.hidden).toBe(true);

      sink.write(new Uint8Array([1, 2, 3]));
      sink.close();
      expect(postMessage.mock.calls.map((c) => (c[0] as { type: string }).type)).toEqual([
        'download-start',
        'download-chunk',
        'download-end',
      ]);
      const chunk = postMessage.mock.calls[1]![0] as { id: string; data: Uint8Array };
      expect(chunk.id).toBe(start.id);
      expect(Array.from(chunk.data)).toEqual([1, 2, 3]);
    });

    it('keeps the worker awake while a long download runs, and stops once it ends', () => {
      const sink = openDownload('disk.vhdx', 1 << 30)!;
      vi.advanceTimersByTime(25_000);
      const pings = () => postMessage.mock.calls.filter((c) => (c[0] as { type: string }).type === 'keepalive').length;
      expect(pings()).toBe(2);
      sink.close();
      vi.advanceTimersByTime(60_000);
      expect(pings()).toBe(2);
    });

    it('tells the worker a download that failed, so the browser marks it failed', () => {
      const sink = openDownload('backup.tar', 10)!;
      sink.abort();
      expect(postMessage).toHaveBeenLastCalledWith(expect.objectContaining({ type: 'download-abort' }));
    });

    it('starts no browser download for one that failed before the worker answered', async () => {
      const sink = openDownload('backup.tar', 10)!;
      sink.abort();
      workerPort().postMessage('ready');
      await vi.advanceTimersByTimeAsync(100);
      expect(document.querySelector('iframe')).toBeNull();
    });
  });

  describe('where no service worker controls the page', () => {
    beforeEach(() => {
      vi.stubGlobal('navigator', { ...navigator, serviceWorker: { controller: null } });
    });

    it('saves a file within the stated limit through an ordinary download', () => {
      const created = vi.fn(() => 'blob:file');
      const revoked = vi.fn();
      vi.stubGlobal('URL', { ...URL, createObjectURL: created, revokeObjectURL: revoked });
      const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {});

      const sink = openDownload('notes.txt', 5)!;
      sink.write(new TextEncoder().encode('hello'));
      sink.close();

      expect(created).toHaveBeenCalledOnce();
      expect(click).toHaveBeenCalledOnce();
      expect(revoked).toHaveBeenCalledWith('blob:file');
    });

    it('refuses a file past the stated limit rather than hold it in memory', () => {
      expect(openDownload('disk.vhdx', BUFFERED_LIMIT + 1)).toBeNull();
      expect(openDownload('small.bin', BUFFERED_LIMIT)).not.toBeNull();
    });
  });
});
