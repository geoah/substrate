"""The Drive stream's own request parameters, spelled once.

The fields mask, the query and the ordering are part of the request, so they
are part of a recording's NAME: a scenario that re-spelled them would look
for files the sync never asked for. `syncdrive` in the google bundle pins the
same strings byte for byte, and the recordings under `fixtures/google` were
cut with them.
"""

# THE DRIVE STREAM. Byte-identical to the bundle's own `syncdrive.py`
# constants, for the reason PERSON_FIELDS is: the fields mask, the query and
# the ordering are part of the request, so they are part of the recording's
# NAME, and a pull spelling one differently records a file no sync asks for.
# `q` is the `backfillDepth: all` form — no `modifiedTime >` term — because
# that is the one listing shape that does not move with the clock, and so the
# one an e2e can replay (the same reasoning as the bare Gmail listing above).
DRIVE_FILE_FIELDS = ",".join((
    "id", "name", "mimeType", "description", "starred", "trashed",
    "explicitlyTrashed", "parents", "webViewLink", "webContentLink",
    "iconLink", "thumbnailLink", "createdTime", "modifiedTime",
    "modifiedByMeTime", "viewedByMeTime", "sharedWithMeTime",
    "owners(emailAddress,displayName,permissionId,photoLink,me)",
    "lastModifyingUser(emailAddress,displayName,permissionId,photoLink,me)",
    "shared", "ownedByMe", "viewedByMe", "version", "size", "quotaBytesUsed",
    "md5Checksum", "fileExtension", "fullFileExtension", "originalFilename",
    "headRevisionId", "driveId", "exportLinks"))
DRIVE_LIST_FIELDS = "nextPageToken,incompleteSearch,files(%s)" % DRIVE_FILE_FIELDS
DRIVE_CHANGES_FIELDS = ("nextPageToken,newStartPageToken,"
                        "changes(changeType,time,removed,fileId,file(%s))"
                        % DRIVE_FILE_FIELDS)
SYNC_DRIVE_PARAMS = {"pageSize": 100, "q": "trashed = false",
                     "orderBy": "modifiedTime desc",
                     "fields": DRIVE_LIST_FIELDS}
# Which Google-native types the sync EXPORTS, and as what. A Doc as
# markdown (Drive has exported Docs to text/markdown since 2024), a Sheet as
# the first sheet's CSV, a Slides deck as plain text; a binary (a PDF, an
# image, a folder) is never exported. The same table lives in the sync.
DRIVE_EXPORTS = {
    "application/vnd.google-apps.document": "text/markdown",
    "application/vnd.google-apps.spreadsheet": "text/csv",
    "application/vnd.google-apps.presentation": "text/plain",
}
MAX_DRIVE_PAGES = 3
# Exports per NATIVE TYPE, so the pull holds a Doc, a Sheet and a Slides deck
# whatever order the listing happens to hand them over in.
MAX_DRIVE_EXPORTS_PER_TYPE = 8
