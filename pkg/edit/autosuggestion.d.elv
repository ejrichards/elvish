#doc:added-in 0.22
#
# A boolean controlling whether [autosuggestions](#autosuggestion) are shown.
# Defaults to `$true`.
#
# When enabled, a suggestion is shown after the content of the buffer in a
# dimmed style. The suggestion is, in order of preference:
#
# 1. The most recent command in the history that starts with the content of
#    the buffer. If the buffer has multiple lines and the last line starts a
#    new command, a command containing a line that starts with the last line
#    is also considered, and only that line is suggested.
#
# 2. The first [completion](#completion-api) candidate for the buffer.
#
# 3. The same as 1, but compared case-insensitively. Accepting such a
#    suggestion corrects the case of the text already typed.
#
# History entries are only suggested if they still look runnable: all commands
# must exist, the argument of `cd` must be a directory other than the current
# directory, and arguments that look like paths (containing `/`, or starting
# with `~` or `.`) must exist.
#
# Like in the Fish shell, deleting characters hides the suggestion until you
# type again.
#
# The suggestion can be accepted with [`edit:autosuggestion:accept`](); the
# functions [`edit:move-dot-right`](), [`edit:move-dot-eol`](),
# [`edit:move-dot-right-word`](), [`edit:move-dot-right-small-word`]() and
# [`edit:move-dot-right-alnum-word`]() also accept the suggestion (fully or
# partially) when the dot is at the end of the buffer.
var autosuggestion:enabled

#doc:added-in 0.22
#
# Accepts the currently shown [autosuggestion](#autosuggestion), replacing the
# content of the buffer with the suggestion. Does nothing if no suggestion is
# shown.
fn autosuggestion:accept { }

#doc:added-in 0.22
#
# Accepts the currently shown [autosuggestion](#autosuggestion) up to the end
# of its first [word](#word-types). Does nothing if no suggestion is shown.
fn autosuggestion:accept-word { }
