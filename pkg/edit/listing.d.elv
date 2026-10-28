# Common binding table for [listing modes](#listing-modes).
var listing:binding { }

# Accepts the current selected listing item.
fn listing:accept { }

# Moves the cursor up in listing mode.
fn listing:up { }

# Moves the cursor down in listing mode.
fn listing:down { }

# Moves the cursor up in listing mode, or to the last item if the first item is
# currently selected.
fn listing:up-cycle { }

# Moves the cursor down in listing mode, or to the first item if the last item is
# currently selected.
fn listing:down-cycle { }

# Moves the cursor up one page.
fn listing:page-up { }

# Moves the cursor down one page.
fn listing:page-down { }

# Starts the history listing mode.
#
# Each entry shows its sequence number, how long the command ran (green if it
# succeeded, red if it failed), how long ago it was run, and the command
# itself with syntax highlighting. The duration is blank for commands that
# haven't finished, and both columns are blank for entries recorded without
# metadata.
#
# The filter uses the [filter DSL](#filter-dsl); entries matching it are
# listed newest last, like without a filter. Entries that don't match it but
# contain the characters of the filter text in order, possibly with other
# characters between them, are listed above them, with the best match
# nearest. Such fuzzy matches have their sequence number dimmed. For example,
# `rebuld` finds `nixos-rebuild` as a fuzzy match while listing any command
# that actually contains `rebuld` first. Fuzzy matches are ranked by how
# tight the match is, favoring matches at the start of words; among equally
# good matches, commands run more recently and more often come first. Case is
# ignored when the filter is all lowercase.
#
# The filter starts as the line of the code buffer the cursor is on, so what
# has been typed so far narrows the listing right away. Accepting an entry
# replaces that line with the entry and leaves the cursor at its end.
#
# The search can be limited to a scope; see
# [`$edit:histlist:scope`](#$edit:histlist:scope).
fn histlist:start { }

# Toggles deduplication in history listing mode.
#
# When deduplication is on (the default), only the last occurrence of the same
# command is shown. Fuzzy matches are always deduplicated.
fn histlist:toggle-dedup { }

# Cycles the scope of the history listing mode through the values of
# [`$edit:histlist:scope`](#$edit:histlist:scope), in the order listed there.
fn histlist:cycle-scope { }

# The scope that the history listing mode searches, one of:
#
# -   `all`: all of history (the default).
#
# -   `dir`: commands run in the current directory.
#
# -   `host`: commands run on this host. This matters when history is synced
#     between hosts.
#
# -   `session`: commands run in this interactive session.
#
# The scope is shown in the mode line when it is not `all`. Entries recorded
# without metadata, for example by an older version of Elvish, are only shown
# in the `all` scope.
var histlist:scope

# Keybinding for the history listing mode.
#
# Keys bound to [edit:histlist:toggle-dedup](#edit:histlist:toggle-dedup)
# (Ctrl-D by default) and
# [edit:histlist:cycle-scope](#edit:histlist:cycle-scope) (Ctrl-R by default,
# so that the key that starts the mode also cycles its scope) will be shown in
# the history listing UI.
var histlist:binding

# Starts the last command mode.
fn lastcmd:start { }

# Keybinding for the last command mode.
var lastcmd:binding

# Starts the location mode.
fn location:start

# Keybinding for the location mode.
var location:binding

# A list of directories to hide in the location addon.
var location:hidden

# A list of directories to always show at the top of the list of the location
# addon.
var location:pinned

# A map mapping types of workspaces to their patterns.
var location:workspaces
