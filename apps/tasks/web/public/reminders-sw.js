// Shows Substrate Tasks' reminders where a browser lets only a worker
// notify (Chrome on Android), and brings a tap back to the page, which
// opens the task. It caches nothing and answers no request.

self.addEventListener("install", () => self.skipWaiting())
self.addEventListener("activate", (e) => e.waitUntil(self.clients.claim()))

self.addEventListener("notificationclick", (e) => {
  e.notification.close()
  const task = e.notification.data && e.notification.data.task
  e.waitUntil(
    (async () => {
      const pages = await self.clients.matchAll({
        type: "window",
        includeUncontrolled: true,
      })
      const page = pages[0]
      if (page) {
        await page.focus()
        page.postMessage({ type: "tasks.reminder", task })
      } else if (task) {
        const base = self.registration.scope
        await self.clients.openWindow(
          `${base}#/reminder/${encodeURIComponent(task)}`
        )
      }
    })()
  )
})
