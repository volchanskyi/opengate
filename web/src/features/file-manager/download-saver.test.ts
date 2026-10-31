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
      vi.useRealTimers();
      await new Promise((resolve) => setTimeout(resolve, 50));
      expect(document.querySelector('iframe')).toBeNull();
    });

    it('adds the download frame once, and removes it after the download ends', async () => {
      const added = vi.spyOn(document.body, 'appendChild');
      const sink = openDownload('backup.tar', 10)!;
      workerPort().postMessage('ready');
      await vi.waitFor(() => expect(document.querySelector('iframe')).not.toBeNull());
      workerPort().postMessage('ready');
      vi.useRealTimers();
      await new Promise((resolve) => setTimeout(resolve, 50));
      expect(added).toHaveBeenCalledOnce();
      vi.useFakeTimers();
      sink.close();
      vi.advanceTimersByTime(30_000);
      expect(document.querySelector('iframe')).toBeNull();
    });

    it('hands the worker a copy of each piece, moved rather than copied again', () => {
      const sink = openDownload('backup.tar', 10)!;
      const piece = new Uint8Array([1, 2, 3]);
      sink.write(piece);
      const [message, transfer] = postMessage.mock.calls[1]! as [{ data: Uint8Array }, ArrayBuffer[]];
      expect(transfer).toEqual([message.data.buffer]);
      expect(message.data.buffer).not.toBe(piece.buffer);
    });
  });

  it('holds a download in memory where the browser offers no service worker at all', () => {
    vi.stubGlobal('navigator', { ...navigator, serviceWorker: undefined });
    expect(openDownload('notes.txt', 5)).not.toBeNull();
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

    // The page reads the blob it handed the browser, and the link it clicked.
    const saved = () => {
      const blobs: Blob[] = [];
      vi.stubGlobal('URL', {
        ...URL,
        createObjectURL: (blob: Blob) => (blobs.push(blob), 'blob:file'),
        revokeObjectURL: () => {},
      });
      const links: { connected: boolean; name: string }[] = [];
      vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (this: HTMLAnchorElement) {
        links.push({ connected: this.isConnected, name: this.download });
      });
      return { blobs, links };
    };

    it('saves the pieces as they were written, whatever the caller does with them after', async () => {
      const { blobs, links } = saved();
      const sink = openDownload('notes.txt', 5)!;
      const piece = new TextEncoder().encode('hello');
      sink.write(piece);
      piece.fill(0);
      sink.close();
      expect(await blobs[0]!.text()).toBe('hello');
      expect(links).toEqual([{ connected: true, name: 'notes.txt' }]);
      expect(document.querySelector('a')).toBeNull();
    });

    it('lets go of the pieces of a download that failed', () => {
      const { blobs } = saved();
      const sink = openDownload('notes.txt', 5)!;
      sink.write(new TextEncoder().encode('hello'));
      sink.abort();
      sink.close();
      expect(blobs[0]!.size).toBe(0);
    });

    it('refuses a file past the stated limit rather than hold it in memory', () => {
      expect(openDownload('disk.vhdx', BUFFERED_LIMIT + 1)).toBeNull();
      expect(openDownload('small.bin', BUFFERED_LIMIT)).not.toBeNull();
    });
  });
});
