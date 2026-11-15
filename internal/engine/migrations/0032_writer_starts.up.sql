-- The start mark (decision record 0325). A start that writes holds the
-- database's writer lock and every repository's writer lease on one session,
-- applies its migrations, and then writes a mark of its own here, a random
-- value no other start writes, and keeps it. A retake of the lease after its
-- session dropped reads it again: another mark, or none, is another process
-- that started as the writer meanwhile, or a database restored or emptied
-- under this one.
--
-- One row, held to one by its key, written by the first start and not here.
-- Control plane, read and written by the lease's session alone, which is the
-- DSN's own user: no role is granted it, and nothing in a repository's
-- history or directory holds it.
CREATE TABLE IF NOT EXISTS writer_starts (
    one  boolean PRIMARY KEY DEFAULT true CHECK (one),
    mark text    NOT NULL
);
