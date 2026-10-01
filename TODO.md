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

### What elvish would need

The command store (`pkg/store/cmd.go`) is a bbolt bucket mapping a sequence
number to the raw command text, and `storedefs.Cmd` is just `{Text, Seq}`.
There is nowhere to put per-entry paths. Options:

- **Side bucket** keyed by the same seq, holding the path list (e.g. one
  newline-separated value, since paths were already parsed from source text
  and so cannot contain a raw newline after the same `\n` escaping fish uses).
  Backwards compatible: old entries have no paths and are never rejected on
  that basis, which matches today's behaviour. Entries from other sessions and
  older versions keep working.
- **Change the value format** of the existing bucket to text plus metadata.
  Needs a version marker and a migration; more invasive for no clear gain.

Either way, detection must run off the main goroutine after `AddCmd` (it does
disk I/O), the entry must be usable for the histlist and history walking
before detection finishes, and `histStore` / `histSnapshot` need to expose the
paths so `runnable` can check them. The existing `looksLikePath` heuristic can
remain as a fallback for entries with no recorded paths.
