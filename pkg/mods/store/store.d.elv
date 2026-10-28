# Outputs the sequence number that will be used for the next entry of the
# command history.
fn next-cmd-seq { }

# Adds an entry to the command history with the given content. Outputs its
# sequence number.
fn add-cmd {|text| }

# Deletes the command history entry with the given sequence number.
#
# **NOTE**: This command only deletes the entry from the persistent store. When
# deleting an entry that was added in the current session, the deletion will
# not take effect for the current session, since the entry still exists in the
# in-memory per-session history.
fn del-cmd {|seq| }

# Outputs the content of the command history entry with the given sequence
# number.
fn cmd {|seq| }

# Outputs all command history entries with sequence numbers between `$from`
# (inclusive) and `$upto` (exclusive). Use -1 for `$upto` to not set an upper
# bound.
#
# Each entry is represented by a pseudo-map with fields `text` and `seq`.
fn cmds {|from upto| }

# Outputs a stable identifier for this host, generated on first use and kept
# in a file named `host-id` next to the database. It identifies the host that
# owns the store when command history is shared with other hosts, and survives
# the database being deleted. Deleting the file makes this a new host.
fn host-id { }

# Sets the metadata of a command history entry, replacing any existing
# metadata. The argument is a map with a subset of the following keys; the
# `seq` key is required and the entry must exist:
#
# -   `seq`: sequence number of the entry.
# -   `id`: globally unique ID of the entry.
# -   `time`: when the command started, in RFC 3339 format.
# -   `duration`: how long the command ran, in seconds.
# -   `exit`: exit status of the command.
# -   `host`: ID of the host the command was run on; see [`store:host-id`]().
# -   `dir`: working directory the command was run in.
# -   `session`: ID of the interactive session the command was run in.
# -   `idx`: index of the entry's record in its host's series. Read-only:
#     it is kept from the existing metadata and set by
#     [`store:commit-cmd-meta`]().
#
# The interactive editor records metadata for every command it adds to the
# history, so this is mostly useful for scripts; use [`store:ingest`]() to
# import history from elsewhere.
fn set-cmd-meta {|meta| }

# Like [`store:set-cmd-meta`](), and additionally appends a record for the
# entry to the series of its `host` if it has none yet, which is what makes
# the entry visible to history sync. The `id` and `host` keys are required
# for the first commit. Outputs the metadata as stored, with `idx` set.
#
# The interactive editor commits every command when it finishes, so this is
# only needed for entries that were never finished or that predate series.
fn commit-cmd-meta {|meta| }

# Outputs the metadata of the command history entry with the given sequence
# number, as a map with the keys described in [`store:set-cmd-meta`]().
# Throws an error if the entry has no metadata.
fn cmd-meta {|seq| }

# Outputs the metadata of all command history entries with sequence numbers
# between `$from` (inclusive) and `$upto` (exclusive) that have metadata. Use
# -1 for `$upto` to not set an upper bound.
fn cmds-meta {|from upto| }

# Outputs the metadata of deleted command history entries with sequence
# numbers between `$from` (inclusive) and `$upto` (exclusive). Only entries
# that had metadata when they were deleted are recorded. Use -1 for `$upto` to
# not set an upper bound.
fn deleted-cmds {|from upto| }

# Outputs the state of every series in the store, as a map from host ID to a
# map from series kind to the index of the last record. Series are the
# append-only logs that history sync exchanges; the only kind today is `cmd`.
fn status { }

# Outputs the records of one series, starting at index `$from` (indexes start
# at 1) and stopping after `$limit` records, or at the end of the series if
# `$limit` is 0. Each record is a map with these keys:
#
# -   `v`: format version, currently 1.
# -   `kind`: series kind, currently `cmd`.
# -   `host`: ID of the host that owns the series.
# -   `idx`: index of the record in the series.
# -   `op`: `add` for an entry being added, `del` for one being deleted.
# -   `id`: ID of the record, which for `add` is the ID of the entry.
# -   `time`: when the command started or was deleted, in RFC 3339 format.
# -   `text`, `duration`, `exit`, `dir`, `session`: for `add`, the entry's
#     text and metadata; see [`store:set-cmd-meta`]().
# -   `target`: for `del`, the ID of the deleted entry.
fn records {|host kind from limit| }

# Appends the given list of records to their series and applies them to the
# command history, all in one transaction. Each record is a map as output by
# [`store:records`](); omitted keys are zero. Each record's `idx` must be one
# more than the current last index of its series, so records must be passed
# in order. An `add` record whose `id` is already known is kept in the series
# but does not create a second entry; a `del` record deletes the entry whose
# ID is `target` if it is present.
#
# This is how history from other hosts, or from another shell, is imported.
fn ingest {|records| }

# Removes every record of the series of `$host` and `$kind`, and clears the
# `idx` of the entries those records had added, so that they are uncommitted
# again. The entries themselves, and any deletions, stay. Outputs the number
# of records removed.
#
# This is for getting a series whose local copy has diverged from other
# hosts' back in step: reset it, [`store:ingest`]() the other hosts' copy,
# which restores the `idx` of the entries it has, then
# [`store:commit-cmd-meta`]() every entry of this host left with `idx` 0.
# `elvish -sync -reset` does exactly this for this host's own series.
fn reset-series {|host kind| }

# Searches the command history and outputs the matching entries, newest
# first. The search runs against an in-memory index kept by the daemon, so
# it is fast however large the history is. The argument is a map with a
# subset of the following keys:
#
# -   `substring`, `prefix`, `regexp`: the text must contain, start with, or
#     match the given pattern. All given patterns must match.
# -   `fold`: whether `substring` and `prefix` ignore case.
# -   `dir`, `host`, `session`: the entry's metadata must have the given
#     value. Entries without metadata never match these.
# -   `exit`: `ok` for entries that exited with 0, `fail` for the others.
# -   `since`, `until`: only entries started at or after `since` and before
#     `until`, in RFC 3339 format.
# -   `before`: only entries with sequence numbers below this, for paging.
# -   `limit`: maximum number of entries to output; 0 for no limit.
# -   `dedup`: whether to skip entries whose text already appeared in a
#     newer entry.
#
# Each entry is output as a map with the keys `seq`, `text`, `time`,
# `duration`, `exit`, `host`, `dir` and `session`; the last six are zero for
# entries without metadata.
#
# ```elvish-transcript
# ~> store:search-cmds [&prefix=git &dir=$pwd &exit=fail &limit=1]
# ▶ [&dir=/home/elf/src &duration=(num 0.8) &exit=(num 1) &host=... &seq=(num 1234) &session=... &text='git push' &time=2026-10-28T13:42:10.637579459Z]
# ```
fn search-cmds {|query| }

# Adds a path to the directory history. This will also cause the scores of all
# other directories to decrease.
fn add-dir {|path| }

# Deletes a path from the directory history. This has no impact on the scores
# of other directories.
fn del-dir {|path| }

# Outputs all directory history entries, in decreasing order of score.
#
# Each entry is represented by a pseudo-map with fields `path` and `score`.
fn dirs { }
