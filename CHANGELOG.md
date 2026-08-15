# Changelog

## [0.2.0](https://github.com/geoah/substrate/compare/v0.1.0...v0.2.0) (2026-08-15)


### ⚠ BREAKING CHANGES

* **core:** declaration versions are incremental integers the API maintains ([#63](https://github.com/geoah/substrate/issues/63))
* **engine:** proposals validate at every door, and a judge agent can accept ([#44](https://github.com/geoah/substrate/issues/44))
* **api:** discovery moves to /.well-known/substrate/server.json, and names the register door's own shape ([#27](https://github.com/geoah/substrate/issues/27))
* **ci:** main builds push :latest instead of :edge ([#29](https://github.com/geoah/substrate/issues/29))
* **bundles:** any authority owns a bundle, and its shipped records are previewed ([#25](https://github.com/geoah/substrate/issues/25))
* **core:** agents speak graphql, and the llm example becomes the substrate assistant ([#21](https://github.com/geoah/substrate/issues/21))
* **vocabulary:** eight mneme bundles join the registry and the shipped kinds unify with them ([#20](https://github.com/geoah/substrate/issues/20))
* **core:** bundle inputs replace the config singleton ([#19](https://github.com/geoah/substrate/issues/19))
* a record editor that knows the schema, and an llmprovider that is declared ([#18](https://github.com/geoah/substrate/issues/18))
* **core:** nothing seeds an llmprovider; an example ships two instead
* **vocabulary:** references are first-class, and the graph reads them
* **engine:** secret material now lives in the sealed store. Run `substratectl repository reseal <username>` once per repository after the server has opened it under this release.
* **engine:** secret material now lives in the sealed store. Run `substratectl repository reseal <username>` once per repository after the server has opened it under this release.
* **runner:** `capabilities.network` is now enforced as a binary gate. A function that makes outbound requests without declaring `network:` will fail where it previously worked. Every shipped bundle already declares it.
* **llm:** the llm kind is removed; agent manifests must replace `llm:` with `provider:` + `model:`; LITELLM_BASE_URL/LITELLM_API_KEY/ LITELLM_EMBED_MODEL are renamed to SUBSTRATE_LLM_*, and LITELLM_MASTER_KEY is dropped.
* **tokens:** drop the last-used stamp, which wrote a changelog entry a minute
* **api:** remove the failure lockout, which was a denial-of-service lever

### Features

* a record editor that knows the schema, and an llmprovider that is declared ([#18](https://github.com/geoah/substrate/issues/18)) ([b37caa5](https://github.com/geoah/substrate/commit/b37caa5683bbb92ac4f9f8671ecf92a620ae1b74))
* **api:** discovery moves to /.well-known/substrate/server.json, and names the register door's own shape ([#27](https://github.com/geoah/substrate/issues/27)) ([6c90460](https://github.com/geoah/substrate/commit/6c9046041193d37b5d4ce2f708bd98cc42a5bbd6))
* **auth:** the second factor can be switched off, and local dev switches it off ([#24](https://github.com/geoah/substrate/issues/24)) ([a8c25fe](https://github.com/geoah/substrate/commit/a8c25fe4cab98307bf72b9deef7efbdb5f0df71d))
* **blobs:** a name beside the optional mime type ([e5f0826](https://github.com/geoah/substrate/commit/e5f08268aaff11e15900eb131261882fa225d46d))
* **bundles:** any authority owns a bundle, and its shipped records are previewed ([#25](https://github.com/geoah/substrate/issues/25)) ([d4510a5](https://github.com/geoah/substrate/commit/d4510a576f5b7364431d0e45c0e90b228003a593))
* **ci:** main builds push :latest instead of :edge ([#29](https://github.com/geoah/substrate/issues/29)) ([b740bcb](https://github.com/geoah/substrate/commit/b740bcbc828bad1c4142ef0a53b6c3a448127357))
* **compose:** Configure LLM embedding gateway environment variables ([#56](https://github.com/geoah/substrate/issues/56)) ([b4d987c](https://github.com/geoah/substrate/commit/b4d987c64122a64aa0eb5b71a031e01306865386))
* **console:** agent threads, tool-call detail, and the record graph ([1224f4f](https://github.com/geoah/substrate/commit/1224f4f4af80f94875ad40d2bb639a2d6f50ce58))
* **console:** the record form speaks the schema, and every pointer gets a picker ([#52](https://github.com/geoah/substrate/issues/52)) ([060465c](https://github.com/geoah/substrate/commit/060465c9d5d5526a4af64138302caea7f427b54e))
* **console:** the review inbox, and the SPA serves assets honestly ([#46](https://github.com/geoah/substrate/issues/46)) ([5b2ac4e](https://github.com/geoah/substrate/commit/5b2ac4e8fe4c4ba3f364dd47a9d62cf8f6fbba5f))
* **core:** agents speak graphql, and the llm example becomes the substrate assistant ([#21](https://github.com/geoah/substrate/issues/21)) ([6fcde52](https://github.com/geoah/substrate/commit/6fcde524fc82c58bac3b2073ee5f434a728353f8))
* **core:** bundle inputs replace the config singleton ([#19](https://github.com/geoah/substrate/issues/19)) ([2958676](https://github.com/geoah/substrate/commit/295867697839de6867c03983c759b4cb65fcac24))
* **core:** bundle upgrades are previewed, offered and version-gated ([#23](https://github.com/geoah/substrate/issues/23)) ([8ce02a1](https://github.com/geoah/substrate/commit/8ce02a11d29d8b163e3225e20fc1436c825533e9))
* **core:** declaration versions are incremental integers the API maintains ([#63](https://github.com/geoah/substrate/issues/63)) ([33d86d6](https://github.com/geoah/substrate/commit/33d86d6d573aa8cfe1c3ddbdb4c68d3ccaefdb49))
* **core:** nothing seeds an llmprovider; an example ships two instead ([b8d4537](https://github.com/geoah/substrate/commit/b8d453768deeebff1446fa17d607bed367ad22f6))
* **engine:** a transition declares what it notifies, and resolutions always arrive ([#78](https://github.com/geoah/substrate/issues/78)) ([5a54224](https://github.com/geoah/substrate/commit/5a54224f90e4ac866befb4350b83043245c83fe8))
* **engine:** an agent asks, the owner answers, and the thread hears it ([#80](https://github.com/geoah/substrate/issues/80)) ([821d0e8](https://github.com/geoah/substrate/commit/821d0e8649977ad34839f377a35bcc4cb1f3cfc6))
* **engine:** an agent asks, the owner answers, the thread hears it ([821d0e8](https://github.com/geoah/substrate/commit/821d0e8649977ad34839f377a35bcc4cb1f3cfc6))
* **engine:** move every secret-typed value into the sealed store ([#12](https://github.com/geoah/substrate/issues/12)) ([fd1b8d9](https://github.com/geoah/substrate/commit/fd1b8d914ef13c66f5e7879973f955b2cc20640b))
* **engine:** per-repository encryption keys and an age recovery key ([#13](https://github.com/geoah/substrate/issues/13)) ([8a6ada2](https://github.com/geoah/substrate/commit/8a6ada2cf6308b2ebb9f532b2849987aea5744ab))
* **engine:** proposals validate at every door, and a judge agent can accept ([#44](https://github.com/geoah/substrate/issues/44)) ([05a6a5a](https://github.com/geoah/substrate/commit/05a6a5aea4d0fde1934856c29b2940ea46091163))
* **engine:** the judge — a policy's agent decides gated requests within the owner's thresholds ([#83](https://github.com/geoah/substrate/issues/83)) ([c7f9049](https://github.com/geoah/substrate/commit/c7f9049031e22bb1a44047488f204e030733b02f))
* **engine:** the policy door — owner rules gate, refuse or allow agent writes ([#81](https://github.com/geoah/substrate/issues/81)) ([303e7e1](https://github.com/geoah/substrate/commit/303e7e10f1cba9a41c1367d1a3616ef553986166))
* **engine:** tool rows carry their changes, and a decision resumes the proposing thread ([#72](https://github.com/geoah/substrate/issues/72)) ([8eb2ad6](https://github.com/geoah/substrate/commit/8eb2ad61f8cbb58c99b736a8a4aeee4c80a3d9dd))
* **kinds:** a description on every kind, read above its collection ([#9](https://github.com/geoah/substrate/issues/9)) ([e85f394](https://github.com/geoah/substrate/commit/e85f394ae65ae9c979184aaa6f0440d9dfbf3e31))
* **llm:** providers as records; agents name a provider and a model ([#7](https://github.com/geoah/substrate/issues/7)) ([b9e1023](https://github.com/geoah/substrate/commit/b9e1023ea003d9e6f4bd38675ec6e5d0622b1667))
* **runner:** confine function bodies with landlock, seccomp and one process each ([#8](https://github.com/geoah/substrate/issues/8)) ([fbf434c](https://github.com/geoah/substrate/commit/fbf434c5cad36e3b97449d612a820d058f2ede31))
* **tokens:** drop the last-used stamp, which wrote a changelog entry a minute ([261802c](https://github.com/geoah/substrate/commit/261802c73a3f4b21b56c9cbc5b719255f10f04dd))
* **vocabulary:** eight mneme bundles join the registry and the shipped kinds unify with them ([#20](https://github.com/geoah/substrate/issues/20)) ([a67da82](https://github.com/geoah/substrate/commit/a67da826a39bb58aaf863a1905eb29747fce9fce))
* **vocabulary:** references are first-class, and the graph reads them ([1d37952](https://github.com/geoah/substrate/commit/1d37952e666d524f8d17162a6fa3eb87db619640))


### Bug Fixes

* **api:** a GraphQL request's extensions key is spelled extensions again ([#39](https://github.com/geoah/substrate/issues/39)) ([9407946](https://github.com/geoah/substrate/commit/940794665821ee3437355bc32c073554e729b983))
* **api:** remove the failure lockout, which was a denial-of-service lever ([1c134b4](https://github.com/geoah/substrate/commit/1c134b4b1cee69d71de5d3b4762b8d7df469d76a))
* **ci:** the first release could not push its image ([4f8662f](https://github.com/geoah/substrate/commit/4f8662f3c803015c31c8de8f6696fc7fed2e224b))
* **console:** react-resizable-panels moves to v4, and the resizable shell rides the Group/Separator API ([#35](https://github.com/geoah/substrate/issues/35)) ([8eb84bb](https://github.com/geoah/substrate/commit/8eb84bb07c02fd3751f75e398bc53d1b23660260))
* **engine:** the runner keys processes by repository id, and the suite runs in parallel ([#26](https://github.com/geoah/substrate/issues/26)) ([7113bfa](https://github.com/geoah/substrate/commit/7113bfaf1dbca8acceacf1a68d3a473cd2845d8a))
* **engine:** the vocabulary upgrade refuses a narrowing instead of projecting it ([656121e](https://github.com/geoah/substrate/commit/656121e6c22fa22c4b5371501eb8213e1cc5a81c))
* **llm:** the go dependencies move, and the openai wire caps completions with max_completion_tokens ([#31](https://github.com/geoah/substrate/issues/31)) ([a368e44](https://github.com/geoah/substrate/commit/a368e44de527f4e9b610b8bfe2833aa02c79dee8))
* **security:** the known advisories are cleared, and the scans that found them run in CI ([#28](https://github.com/geoah/substrate/issues/28)) ([38fa144](https://github.com/geoah/substrate/commit/38fa1449f05b0ec70b8d4197f2b3ffee9349db90))

## 0.1.0 (2026-08-12)


### Features

* the substrate ([e7301a7](https://github.com/geoah/substrate/commit/e7301a7244cfc3ea9d01af6ae64912b973770e5c))
