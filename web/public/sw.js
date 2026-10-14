// OpenGate Service Worker: push notifications, and session downloads streamed into Downloads.

// Install: skip waiting to activate immediately
globalThis.addEventListener('install', () => {
  globalThis.skipWaiting();
});

// Activate: purge all caches so the latest index.html is always fetched from network
globalThis.addEventListener('activate', (event) => {
  event.waitUntil(
    caches.keys().then((keys) =>
      Promise.all(keys.map((k) => caches.delete(k)))
    )
  );
  globalThis.clients.claim();
});

// Session downloads by id, each kept until its stream has ended and been served. The page posts
// each piece as it arrives, and the stream holds it until the browser's download reads it.
const downloads = new Map();

globalThis.addEventListener('message', (event) => {
  const msg = event.data;
  if (!msg || typeof msg.type !== 'string') return;
  switch (msg.type) {
    case 'download-start': {
      let controller;
      const stream = new ReadableStream({
        start(c) {
          controller = c;
        },
      });
      downloads.set(msg.id, { stream, controller, name: msg.name, size: msg.size, served: false, ended: false });
      // The page asks for the download's address only after this answer.
      event.ports[0]?.postMessage('ready');
      break;
    }
    case 'download-chunk':
      downloads.get(msg.id)?.controller.enqueue(msg.data);
      break;
    case 'download-end': {
      const entry = downloads.get(msg.id);
      if (!entry) break;
      entry.controller.close();
      entry.ended = true;
      if (entry.served) downloads.delete(msg.id);
      break;
    }
    case 'download-abort':
      downloads.get(msg.id)?.controller.error(new Error('the session ended the download'));
      downloads.delete(msg.id);
      break;
    default:
      // A keepalive needs no answer: receiving it keeps this worker running.
      break;
  }
});

// Chromium refuses a name holding a raw quote or parenthesis, which encodeURIComponent leaves.
function encodedName(name) {
  return encodeURIComponent(name).replaceAll(
    /['()*]/g,
    (c) => `%${c.charCodeAt(0).toString(16).toUpperCase()}`,
  );
}

/** Answers a download's address with its stream, once, as an attachment named for the file. */
function downloadResponse(id) {
  const entry = downloads.get(id);
  if (!entry || entry.served) return new Response('Download not found', { status: 404 });
  entry.served = true;
  if (entry.ended) downloads.delete(id);
  return new Response(entry.stream, {
    headers: {
      'Content-Type': 'application/octet-stream',
      'Content-Disposition': `attachment; filename*=UTF-8''${encodedName(entry.name)}`,
      // WebKit otherwise sniffs the stream as a page and renames the file with .html.
      'X-Content-Type-Options': 'nosniff',
      'Content-Length': String(entry.size),
    },
  });
}

// Fetch: a session download from its stream, other navigation from the network (no offline cache).
globalThis.addEventListener('fetch', (event) => {
  const url = new URL(event.request.url);
  if (url.origin === globalThis.location.origin && url.pathname.startsWith('/__download/')) {
    event.respondWith(downloadResponse(url.pathname.slice('/__download/'.length)));
    return;
  }
  // Vite's hashed assets carry a long-lived HTTP cache, so only navigation is passed through.
  if (event.request.mode === 'navigate') {
    event.respondWith(
      fetch(event.request)
    );
  }
});

// Push: display notification from server payload
globalThis.addEventListener('push', (event) => {
  let data = { title: 'OpenGate', body: 'New notification' };

  if (event.data) {
    try {
      data = event.data.json();
    } catch {
      data.body = event.data.text();
    }
  }

  event.waitUntil(
    globalThis.registration.showNotification(data.title, {
      body: data.body,
      icon: '/vite.svg',
      badge: '/vite.svg',
      data: { device_id: data.device_id },
    })
  );
});

// Notification click: focus or open the app
globalThis.addEventListener('notificationclick', (event) => {
  event.notification.close();

  const url = event.notification.data?.device_id
    ? `/devices/${event.notification.data.device_id}`
    : '/';

  event.waitUntil(
    globalThis.clients.matchAll({ type: 'window' }).then((clients) => {
      for (const client of clients) {
        if (client.url.includes(globalThis.location.origin) && 'focus' in client) {
          client.navigate(url);
          return client.focus();
        }
      }
      return globalThis.clients.openWindow(url);
    })
  );
});
