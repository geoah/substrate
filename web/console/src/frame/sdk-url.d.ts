/** The module the `sdkUrl` plugin in vite.config.ts serves: the URL of the
 * apps SDK, `/src/apps-sdk/index.ts` in dev and the content-hashed chunk in a
 * build. Imported by the frame shell alone. */
declare module "virtual:substrate-sdk-url" {
  const url: string
  export default url
}
