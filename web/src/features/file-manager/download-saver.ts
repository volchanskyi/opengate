/** Where a download's pieces go as they arrive. */
export interface DownloadSink {
  write(data: Uint8Array): void;
  close(): void;
  abort(): void;
}

/** Largest file a browser without a controlling service worker saves, held in memory. */
export const BUFFERED_LIMIT = 64 * 1024 * 1024;

// A browser stops an idle service worker; a message every ten seconds keeps it streaming.
const KEEPALIVE_MS = 10_000;

// How long the frame that started a streamed download stays after it ends.
const FRAME_LINGER_MS = 30_000;

/**
 * Opens a download's sink: streamed through a controlling service worker into Downloads, else held
 * in memory up to {@link BUFFERED_LIMIT}; null for a file past that limit.
 */
export function openDownload(name: string, size: number): DownloadSink | null {
  const worker = navigator.serviceWorker?.controller;
  if (worker) return streamThrough(worker, name, size);
  if (size > BUFFERED_LIMIT) return null;
  return holdInMemory(name);
}

function streamThrough(worker: ServiceWorker, name: string, size: number): DownloadSink {
  const id = crypto.randomUUID();

  // The worker answers this address with the stream, and the browser saves the answer.
  const frame = document.createElement('iframe');
  frame.hidden = true;
  frame.src = `/__download/${id}`;

  // The frame asks only once the worker holds the stream: a request can overtake the start message.
  const answer = new MessageChannel();
  answer.port1.onmessage = () => {
    answer.port1.close();
    document.body.appendChild(frame);
  };
  worker.postMessage({ type: 'download-start', id, name, size }, [answer.port2]);

  const keepalive = setInterval(() => worker.postMessage({ type: 'keepalive' }), KEEPALIVE_MS);
  const finish = (type: 'download-end' | 'download-abort') => {
    clearInterval(keepalive);
    worker.postMessage({ type, id });
    setTimeout(() => frame.remove(), FRAME_LINGER_MS);
  };

  return {
    write(data) {
      // A copy owns its buffer, so it can be handed over without copying again.
      const copy = data.slice();
      worker.postMessage({ type: 'download-chunk', id, data: copy }, [copy.buffer]);
    },
    close: () => finish('download-end'),
    abort() {
      answer.port1.close();
      finish('download-abort');
    },
  };
}

function holdInMemory(name: string): DownloadSink {
  const pieces: Uint8Array[] = [];
  return {
    write(data) {
      pieces.push(data.slice());
    },
    close() {
      const url = URL.createObjectURL(new Blob(pieces as BlobPart[]));
      const link = document.createElement('a');
      link.href = url;
      link.download = name;
      document.body.appendChild(link);
      link.click();
      link.remove();
      URL.revokeObjectURL(url);
    },
    abort() {
      pieces.length = 0;
    },
  };
}
