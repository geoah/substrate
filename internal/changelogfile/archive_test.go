package changelogfile

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// An archive id sorts by when it was made, past every id it is told to
// follow, so a clock that steps back still starts a later archive.
func TestArchiveIDsSortByWhenTheyWereMade(t *testing.T) {
	now := time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)
	a, err := NewArchiveID(now)
	if err != nil || !ValidArchiveID(a) {
		t.Fatalf("NewArchiveID = %q, %v", a, err)
	}
	b, err := NewArchiveID(now.Add(time.Second))
	if err != nil || b <= a {
		t.Fatalf("a later archive %q does not sort after %q", b, a)
	}
	c, err := NewArchiveID(now.Add(-time.Hour), b)
	if err != nil || c <= b {
		t.Fatalf("an archive started on a clock behind %q sorts at %q", b, c)
	}
	for _, bad := range []string{"", "a", "b0001823f5e7c1a2b3c4d5e6f7", strings.ToUpper(a), a + "0"} {
		if ValidArchiveID(bad) {
			t.Errorf("%q read as an archive id", bad)
		}
	}
	p, err := NewPassID()
	if err != nil || !ValidPassID(p) {
		t.Fatalf("NewPassID = %q, %v", p, err)
	}
}

// Every object's name carries the pass that put it and reads back apart; a
// manifest's carries its epoch and the hash of its bytes.
func TestArchiveObjectNames(t *testing.T) {
	const pass = "0123456789abcdef"
	sum := sha256.Sum256([]byte("piece"))
	piece := PieceObjectName(2, 1, 420, pass, sum)
	if d, first, last, p, ok := ParsePieceObjectName(piece); !ok || d != 2 || first != 1 || last != 420 || p != pass {
		t.Fatalf("ParsePieceObjectName(%q) = %d %d %d %q %v", piece, d, first, last, p, ok)
	}
	for _, bad := range []string{
		"changelog/2/000000000000001-000000000000420.ndjson",
		"changelog/2/000000000000005-000000000000004-0123456789abcdef-0123456789abcdef.ndjson",
		"changelog/02/000000000000001-000000000000420-0123456789abcdef-0123456789abcdef.ndjson",
		"blobs/x.0123456789abcdef",
	} {
		if _, _, _, _, ok := ParsePieceObjectName(bad); ok {
			t.Errorf("%q read as a piece", bad)
		}
	}
	blob := BlobObjectName("blob-sha256-"+strings.Repeat("a", 64), pass)
	if d, p, ok := ParseBlobObjectName(blob); !ok || d != "blob-sha256-"+strings.Repeat("a", 64) || p != pass {
		t.Fatalf("ParseBlobObjectName(%q) = %q %q %v", blob, d, p, ok)
	}
	sealed := SealedObjectName("secret:abc", sum, pass)
	if s, p, ok := ParseSealedObjectName("secret:abc", sealed); !ok || s != sum || p != pass {
		t.Fatalf("ParseSealedObjectName(%q) = %x %q %v", sealed, s, p, ok)
	}
	if _, _, ok := ParseSealedObjectName("secret:other", sealed); ok {
		t.Fatal("a sealed object was read as another ref's")
	}
	file := SpoolFileName(7, "fire1")
	spool := SpoolObjectName(file, pass)
	if f, p, ok := ParseSpoolObjectName(spool); !ok || f != file || p != pass {
		t.Fatalf("ParseSpoolObjectName(%q) = %q %q %v", spool, f, p, ok)
	}
	data := []byte(`{"m":1}`)
	name := ArchiveManifestName(9, data)
	if epoch, _, ok := ParseArchiveManifestName(name); !ok || epoch != 9 || !ManifestNamesBytes(name, data) {
		t.Fatalf("manifest name %q", name)
	}
	if ManifestNamesBytes(name, []byte(`{"m":2}`)) {
		t.Fatal("a manifest name was held to other bytes")
	}
	if ArchiveManifestName(9, data) >= ArchiveManifestName(10, data) {
		t.Fatal("manifest names do not sort as their epochs")
	}
	for _, bad := range []string{"repository.json", "00000000000000000009.json", DeletedMark} {
		if _, _, ok := ParseArchiveManifestName(bad); ok {
			t.Errorf("%q read as a manifest's name", bad)
		}
	}
	aid, _ := NewArchiveID(time.Now())
	for key, want := range map[string]ArchiveKey{
		ArchiveManifestsPrefix(testRepoID, aid) + name:        {Repository: testRepoID, Archive: aid, Name: name, Manifests: true},
		ArchiveManifestsPrefix(testRepoID, aid) + DeletedMark: {Repository: testRepoID, Archive: aid, Name: DeletedMark, Manifests: true},
		ArchivePrefix(testRepoID, aid) + blob:                 {Repository: testRepoID, Archive: aid, Name: blob},
	} {
		if got, ok := ParseArchiveKey(key); !ok || got != want {
			t.Errorf("ParseArchiveKey(%q) = %+v %v, want %+v", key, got, ok, want)
		}
	}
	if _, ok := ParseArchiveKey(RepositoryPrefix(testRepoID) + "repository.json"); ok {
		t.Error("a 0.x manifest was read as an archive's key")
	}
}

// archiveManifestFixture is a manifest of three lines in two pieces.
func archiveManifestFixture(t *testing.T) Manifest {
	t.Helper()
	_, sum, err := Encode(Entry{Seq: 3, Txn: 3, TS: time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC), Actor: "api", Op: "put", Kind: "k.example.com/p/k", RecordID: "r", Payload: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	const pass = "0123456789abcdef"
	aid, _ := NewArchiveID(time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC))
	return Manifest{
		Format: ManifestFormatArchive, ID: testRepoID, Authority: "ada.example.com", Archive: aid, Pass: pass,
		CreatedAt: time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC), ChangelogDialect: 1, VocabularyDialect: 3,
		DEK: []byte("wrapped"), DEKKeyID: "0123456789abcdef", Head: 3, HeadSum: SumText(sum), Epoch: 9,
		Pieces: []ArchivePiece{
			{Name: PieceObjectName(1, 1, 2, pass, sum), Sum: SumText(sum)},
			{Name: PieceObjectName(1, 3, 3, pass, sum), Sum: SumText(sum)},
		},
		Sealed:       map[string]string{"secret:abc": SealedObjectName("secret:abc", sum, pass)},
		Spool:        []string{SpoolObjectName(SpoolFileName(1, "fire1"), pass)},
		MissingBlobs: []string{"blob-sha256-" + strings.Repeat("a", 64)},
		PendingBlobs: []string{"blob-sha256-" + strings.Repeat("b", 64)},
	}
}

func TestArchiveManifest(t *testing.T) {
	m := archiveManifestFixture(t)
	data, err := EncodeArchiveManifest(m)
	if err != nil {
		t.Fatal(err)
	}
	back, err := DecodeArchiveManifest(data, testRepoID)
	if err != nil {
		t.Fatalf("decode: %v\n%s", err, data)
	}
	if back.Head != 3 || back.HeadSum != m.HeadSum || back.ID != testRepoID || back.Epoch != 9 || back.Archive != m.Archive || back.Pass != m.Pass ||
		len(back.Pieces) != 2 || back.Sealed["secret:abc"] != m.Sealed["secret:abc"] || len(back.Spool) != 1 ||
		len(back.MissingBlobs) != 1 || len(back.PendingBlobs) != 1 {
		t.Fatalf("decoded %+v", back)
	}
	chain, err := back.Chain()
	if err != nil || len(chain) != 2 || chain[0].First != 1 || chain[1].Last != 3 {
		t.Fatalf("chain %+v (%v)", chain, err)
	}
	if _, err := DecodeArchiveManifest(data, "rnd-0000000000000000000000000z"); err == nil {
		t.Fatal("a manifest of another repository was accepted")
	}
	empty := m
	empty.Head, empty.HeadSum, empty.Pieces = 0, "", nil
	if data, err := EncodeArchiveManifest(empty); err != nil || strings.Contains(string(data), "headSum") {
		t.Fatalf("an empty archive's manifest: %s (%v)", data, err)
	}

	short := m
	short.Pieces = short.Pieces[:1]
	gap := m
	gap.Pieces = []ArchivePiece{m.Pieces[1]}
	otherDialect := m
	otherDialect.Pieces = []ArchivePiece{m.Pieces[0], {Name: strings.Replace(m.Pieces[1].Name, "/1/", "/2/", 1), Sum: m.Pieces[1].Sum}}
	for name, bad := range map[string]Manifest{"pieces short of the head": short, "a gap at seq 1": gap, "a piece in another dialect": otherDialect} {
		if _, err := EncodeArchiveManifest(bad); !errors.Is(err, ErrManifestFormat) {
			t.Errorf("%s: written (%v)", name, err)
		}
	}
	for name, raw := range map[string]string{
		"no head":                        strings.Replace(string(data), `"head": 3,`, ``, 1),
		"a head, no sum":                 strings.Replace(string(data), `"headSum": "`+m.HeadSum+`"`, `"headSum": ""`, 1),
		"an unknown key":                 strings.Replace(string(data), `"head": 3`, `"head": 3, "tail": 1`, 1),
		"format 2, a head":               strings.Replace(string(data), `"format": 3`, `"format": 2`, 1),
		"no epoch":                       strings.Replace(string(data), `"epoch": 9`, `"epoch": 0`, 1),
		"no archive":                     strings.Replace(string(data), `"archive": "`+m.Archive+`",`, ``, 1),
		"a sealed object of another ref": strings.Replace(string(data), `"secret:abc": "sealed/secret-abc.`, `"secret:abc": "sealed/secret-abd.`, 1),
		"a blob with a dot":              strings.Replace(string(data), strings.Repeat("a", 64), strings.Repeat("a", 63)+".", 1),
	} {
		if _, err := DecodeArchiveManifest([]byte(raw), testRepoID); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	two := m
	two.Format = ManifestFormat
	if _, err := EncodeArchiveManifest(two); err == nil {
		t.Fatal("a format-2 manifest was written as an archive's")
	}
}

// On a file system, the newest archive's newest manifest is the one read;
// two of its highest epoch, and a newest archive that carries the deletion
// mark, are refused.
func TestReadArchiveManifestDir(t *testing.T) {
	root := t.TempDir() + "/" + testRepoID
	m := archiveManifestFixture(t)
	name, err := WriteArchiveManifestDir(root, m)
	if err != nil {
		t.Fatal(err)
	}
	m.Epoch = 10
	newer, err := WriteArchiveManifestDir(root, m)
	if err != nil {
		t.Fatal(err)
	}
	got, gotName, err := ReadArchiveManifestDir(root)
	if err != nil || got.Epoch != 10 || gotName != newer || gotName == name {
		t.Fatalf("read %d %q (%v), want epoch 10 %q", got.Epoch, gotName, err, newer)
	}
	m.HeadSum = SumText(sha256.Sum256([]byte("another line")))
	if _, err := WriteArchiveManifestDir(root, m); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadArchiveManifestDir(root); !errors.Is(err, ErrManifestFormat) {
		t.Fatalf("two manifests of the highest epoch: %v", err)
	}
	later := archiveManifestFixture(t)
	later.Archive, _ = NewArchiveID(time.Now(), m.Archive)
	if _, err := WriteArchiveManifestDir(root, later); err != nil {
		t.Fatal(err)
	}
	if got, _, err := ReadArchiveManifestDir(root); err != nil || got.Archive != later.Archive {
		t.Fatalf("the newest archive was not the one read: %+v (%v)", got.Archive, err)
	}
	if err := writeFileAtomic(root+"/"+ManifestsSubdir+"/"+later.Archive, DeletedMark, nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadArchiveManifestDir(root); !errors.Is(err, ErrArchiveDeleted) {
		t.Fatalf("an archive that carries the deletion mark: %v", err)
	}
}
