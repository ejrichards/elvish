# TODO

## Autosuggestion: record path arguments in history like fish

### Problem

With `ls downloads` in history, typing `ls d` in a directory that has no
`downloads` but does have `docs/` suggests `ls downloads` (from history), where
fish suggests `ls docs/` (from completion). Both shells rank history above
completions; the difference is that fish rejects the history item because it
knows `downloads` was a real file when the command ran and no longer is.

Elvish's `runnable` check in `pkg/edit/autosuggestion.go` only validates
arguments that *look* like paths (contain `/`, or start with `~` or `.`), so a
bare word like `downloads` is never checked. Checking every bare argument at
suggestion time is not an option: it would reject `git status` and `echo hello`.
Fish avoids this by only requiring arguments that were files at add time.

### How fish does it

**At add time** (`src/history/history.rs`, `add_pending_with_file_detection`):

1. Parse the command. Collect every argument node whose source text is
   non-empty and does not start with `-` as a potential path.
2. Add the item to history immediately as "pending", with no paths, and
   disable automatic saving.
3. On a background thread, expand each potential path (variables and `~`, but
   no command substitution and no wildcards, so `rm *` is still suggested in an
   empty dir) and test it with `access(F_OK)` relative to the working
   directory at that time. Keep the *unexpanded* text of each one that exists.
4. Attach the survivors to the item as `required_paths`, re-enable saving, and
   save.

Commands likely to exit the shell (`exit`, `reboot`, `restart`, `exec ...`) and
`echo` skip detection and are written synchronously so the item is not lost.

**On disk** (`src/history/file.rs` `HistoryItem::write_to`,
`src/history/yaml_backend.rs` `decode_item_fish_2_0`):

The history file is a line-oriented, nearly-YAML format. Each item is:

```
- cmd: ssh blah blah blah
  when: 2348237
  added_when: 2348238
  paths:
    - /path/to/something
    - relative/path
```

- `cmd` and each path are escaped with only two rules: `\` becomes `\\` and
  newline becomes `\n`. Paths are stored as the user typed them (unexpanded,
  possibly relative).
- `added_when` is only written when it differs from `when`. `paths:` is only
  written when the list is non-empty.
- The decoder reads `- cmd:` then consumes indented `key: value` lines at a
  consistent indent. On `paths`, it keeps consuming lines that are indented
  deeper than the key and start with `- `. Unknown keys are ignored, so the
  format is forward-compatible.

**At suggestion time** (`src/highlight/highlight.rs`
`autosuggest_validate_from_history`, `src/history/history.rs`
`all_paths_are_valid`):

For each candidate history item, expand each recorded path the same way (no
I/O, no wildcards) and test it against the *current* working directory. If any
is missing, reject the item and keep searching history. `cd` is handled
separately: its target must resolve (honouring CDPATH) to a directory other
than the current one.

### What elvish has now

The storage half exists as a side effect of the history sync work (see
`SYNC.md`). Each history entry has a metadata record in the `cmdmeta` bucket
(`pkg/store/cmdmeta.go`), keyed by seq and JSON-encoded, with id, time,
duration, exit, host, dir and session. It is backwards compatible: old
entries have no record and nothing rejects them on that basis.

The editor writes it in two phases from `cmdMetaTracker` in
`pkg/edit/cmd_meta.go`: once right after `AddCmd` and once from the
after-command hook with duration and exit status. The record is held in
memory between the two writes under the tracker's lock, which is the same
pending-then-save shape fish uses.

### What is left

- [ ] Add a `Paths []string` field to `storedefs.CmdMeta`, with the
      unexpanded text of each argument that existed when the command was
      added. Add the `paths` key to `store:set-cmd-meta` and friends.
- [ ] Detection in the tracker after `AddCmd`: parse the command, take every
      argument node that is non-empty and does not start with `-`, expand
      `~` and variables (no command substitution, no wildcards, so `rm *`
      still suggests in an empty dir), test with `access(F_OK)` relative to
      the recorded dir, keep the survivors. Run it in a goroutine that
      updates the pending record under the tracker's lock, and if the
      completion write has already happened, write again itself. Skip
      detection for `exit`, `exec` and `echo` like fish does.
- [ ] Expose paths through `histStore` and `histSnapshot` so `runnable` in
      `pkg/edit/autosuggestion.go` can check candidates without an RPC per
      entry. Session entries can carry the paths in memory; entries loaded
      from the db need `CmdsMeta` alongside `CmdsWithSeq` in the snapshot.
- [ ] Suggestion-time check in `runnable`: reject a candidate if any recorded
      path is missing relative to the current directory. `cd` is special:
      its target must resolve, honouring CDPATH, to a directory other than
      the current one. Keep the `looksLikePath` heuristic as the fallback
      for entries with no recorded paths.

### Interaction with sync

Paths ride along in the metadata, so entries imported from another machine
carry that machine's paths. They are tested against the local directory at
suggestion time and usually fail, so the entry is rejected rather than
suggested. That is the correct fish behaviour and needs no extra handling.
