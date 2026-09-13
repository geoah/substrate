/** THE ONE DOOR into the apps runtime. The pages and the attach points import
 * this module dynamically (`lazy(() => import("@/components/apps/runtime"))`,
 * the YamlEditor pattern), so everything under lib/apps and components/apps
 * is one chunk that a login opening no app never loads, and the guest's own
 * chunks (the SDK, the kit, React's entries) never enter the console's main
 * chunk. Nothing else imports from here statically. */

export { AppCard } from "@/components/apps/app-card"
export { AppScreen } from "@/components/apps/app-screen"
export { Launcher } from "@/components/apps/launcher"
