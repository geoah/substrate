/** THE ONE DOOR into the apps runtime. The three pages import this module
 * dynamically (`lazy(() => import("@/components/apps/runtime"))`, the
 * YamlEditor pattern), so everything under lib/apps and components/apps is
 * one chunk that a login opening no view never loads. Nothing else imports
 * from here statically. */

export { AppScreen } from "@/components/apps/app-screen"
export { Launcher } from "@/components/apps/launcher"
export { ViewScreen } from "@/components/apps/view-screen"
