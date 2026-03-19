import { useEffect, useCallback } from 'react';
import { usePushStore } from '../../state/push-store';

function urlBase64ToUint8Array(base64String: string): Uint8Array {
  const padding = '='.repeat((4 - (base64String.length % 4)) % 4);
  const base64 = (base64String + padding).replaceAll('-', '+').replaceAll('_', '/');
  const raw = atob(base64);
  const arr = new Uint8Array(raw.length);
  for (let i = 0; i < raw.length; i++) arr[i] = raw.codePointAt(i)!;
  return arr;
}

export function NotificationCenter() {
  const vapidKey = usePushStore((s) => s.vapidKey);
  const isSubscribed = usePushStore((s) => s.isSubscribed);
  const fetchVapidKey = usePushStore((s) => s.fetchVapidKey);
  const syncSubscriptionStatus = usePushStore((s) => s.syncSubscriptionStatus);
  const subscribe = usePushStore((s) => s.subscribe);
  const unsubscribe = usePushStore((s) => s.unsubscribe);

  useEffect(() => {
    fetchVapidKey();
    syncSubscriptionStatus();
  }, [fetchVapidKey, syncSubscriptionStatus]);

  const handleToggle = useCallback(async () => {
    if (!('serviceWorker' in navigator) || !('PushManager' in globalThis)) return;

    const registration = await navigator.serviceWorker.ready;

    if (isSubscribed) {
      const sub = await registration.pushManager.getSubscription();
      if (sub) {
        await unsubscribe(sub.endpoint);
        await sub.unsubscribe();
      }
      return;
    }

    if (!vapidKey) return;

    const sub = await registration.pushManager.subscribe({
      userVisibleOnly: true,
      applicationServerKey: urlBase64ToUint8Array(vapidKey).buffer as ArrayBuffer,
    });

    const json = sub.toJSON();
    if (json.endpoint && json.keys?.p256dh && json.keys?.auth) {
      await subscribe(json.endpoint, json.keys.p256dh, json.keys.auth);
    }
  }, [isSubscribed, vapidKey, subscribe, unsubscribe]);

  if (!('PushManager' in globalThis)) return null;

  return (
    <button
      onClick={handleToggle}
      className="text-sm text-gray-400 hover:text-white"
      title={isSubscribed ? 'Disable notifications' : 'Enable notifications'}
      aria-label={isSubscribed ? 'Disable notifications' : 'Enable notifications'}
    >
      {isSubscribed ? '\u{1F514}' : '\u{1F515}'}
    </button>
  );
}
