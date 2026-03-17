// OpenGate Service Worker — push notifications + offline caching

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

// Fetch: network-only for navigation (no offline cache)
globalThis.addEventListener('fetch', (event) => {
  // Only intercept navigation requests for push notification support
  // Let the network handle everything — Vite hashed assets have long-lived HTTP cache
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
