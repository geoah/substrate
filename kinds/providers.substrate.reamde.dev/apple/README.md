# The Apple bundle

Package `providers.substrate.reamde.dev/apple`: Apple data that an app on the
owner's device pushes into the repository, in Apple's own shape. Today that is
the Contacts framework; Apple Health adds its kinds to this package later.
One way: the device writes, nothing reads back, and nothing is written to the
device.

Unlike every other provider, this one has no account, no config, no OAuth, no
sync function and no triggers. `bundle.yaml` declares kinds only, and the
device is the sync. It is the contract; this file is not.

## Kinds

| Kind | One row per |
| --- | --- |
| `addressbook` | one device's contact store, as one install of the app sees it |
| `contact` | one `CNContact` in that store |

Left out of `contact` on purpose: `note` (reading it needs Apple's
`com.apple.developer.contacts.notes` entitlement), the image
(`imageData`, `thumbnailImageData`; it arrives later as a blob reference) and
`nonGregorianBirthday`.

## How the device writes

The app writes with the owner's token through the ordinary record routes,
after the bundle is installed (`POST /api/v1/catalog/providers.substrate.reamde.dev%2Fapple/install`):

- `PUT /api/v1/providers.substrate.reamde.dev/apple/addressbook/<id>` with
  `deviceName`, `lastSyncedAt` and `contactCount` after each push.
- `PUT /api/v1/providers.substrate.reamde.dev/apple/contact/<id>` for each
  contact. A put merges and never prunes, so a key that vanished from the
  contact is cleared with `PATCH` on the same path, setting that key to
  `null`.
- `DELETE` on the contact's path for a contact gone from the device. Deleting
  the `addressbook` row collects its contacts on the next garbage-collection
  sweep (`onDelete: cascade`).

The device omits an empty string or list rather than sending `""` or `[]`.
`identifier` and `addressbook` are required; every other property is
optional.

Labels (`phoneNumbers[].label` and the other labeled arrays) are plain
strings. The device sends a built-in label as a fixed English word whatever
its language, so `_$!<Mobile>!$_` always arrives as "mobile" and
`_$!<HomeFAX>!$_` as "home fax"; a custom label arrives as typed.

`emailAddresses[].value` is the `email` datatype, as Google's contact
declares it, so a person mapping matches it case-insensitively. A value that
does not parse as an address refuses the whole write, so the app leaves such
an entry out. Phone numbers and URLs are plain strings, as the owner typed
them.

`displayName` is the output of `CNContactFormatter` with style `.fullName`,
computed on the device.

## Ids

Both ids use `ExternalID(provider, account, externalID)` from
[`internal/runner/ids.go`](../../../internal/runner/ids.go), the algorithm
`host.ids.external` runs in a function body:

- address book: `ExternalID("apple", "addressbook", <install UUID>)`, where
  the install UUID is a lowercase UUID string the app generates once and
  keeps.
- contact: `ExternalID("apple", <address book id>, <CNContact.identifier>)`.
  `CNContact.identifier` is unique only within one device's contact store,
  so the address book id scopes it.

`ExternalID` is `"apple-"` followed by the first 32 hex characters of a
SHA-256 over the three parts, each written as `<byte length>:<bytes>|`, with
UTF-8 bytes and the length in decimal. The result is 38 characters of
`[a-z0-9-]`, a legal record id for a `PUT` path as it stands.

Test vectors, minted by `ids.go` and `host.py` (they agree, and three of them
are in [`internal/runner/testdata/id_vectors.json`](../../../internal/runner/testdata/id_vectors.json)):

| Account | External id | Id |
| --- | --- | --- |
| `addressbook` | `6f1c2e9a-3b4d-4e5f-8a7b-9c0d1e2f3a4b` | `apple-85bb44b13b0b7c81a18d0533e773a814` |
| `apple-85bb44b13b0b7c81a18d0533e773a814` | `410FE041-5C4E-48DA-B4DE-04C15EA3DBAC:ABPerson` | `apple-ad8a9ccf185d20237632d37b301b1c22` |
| `apple-85bb44b13b0b7c81a18d0533e773a814` | `177C371E-701D-42F8-A03B-C61CA31627F6` | `apple-a418423d223baa867f825ec3d5c95334` |
| `addressbook` | `00000000-0000-4000-8000-000000000001` | `apple-daaeb117bc32a2acdd1b67b55be59b48` |
| `apple-daaeb117bc32a2acdd1b67b55be59b48` | `410FE041-5C4E-48DA-B4DE-04C15EA3DBAC:ABPerson` | `apple-7c3db4e15d22226e79d94c8405e050e7` |
| `apple-daaeb117bc32a2acdd1b67b55be59b48` | `177C371E-701D-42F8-A03B-C61CA31627F6` | `apple-7a08d3ea0f6f98628994fc5367e0272e` |

The provider is `apple` in every row. The same `CNContact.identifier` under
two address books gives two ids.

More on the package, and what it does not do:
[docs/bundles-catalog.md#apple](../../../docs/bundles-catalog.md#apple).
