# Maximum height the editor is allowed to use, defaults to `+Inf`.
#
# By default, the height of the editor is only restricted by the terminal
# height. Some modes like location mode can use a lot of lines; as a result,
# it can often occupy the entire terminal, and push up your scrollback buffer.
# Change this variable to a finite number to restrict the height of the editor.
var max-height

# A list of functions to call before each readline cycle. Each function is
# called without any arguments.
var before-readline

# A list of functions to call after each readline cycle. Each function is
# called with a single string argument containing the code that has been read.
var after-readline

# List of filters to run before adding a command to history.
#
# A filter is a function that takes a command as argument and outputs
# a boolean value. If any of the filters outputs `$false`, the
# command is not saved to history, and the rest of the filters are
# not run. The default value of this list contains a filter which
# ignores command starts with space.
var add-cmd-filters

# List of regular expressions for commands to exclude from history.
#
# A command that matches any of the patterns is not saved to history.
# Patterns use [Go regular expression syntax](https://pkg.go.dev/regexp/syntax)
# and are not anchored, so use `^` and `$` to match the whole command. This
# check runs before [`$edit:add-cmd-filters`](#$edit:add-cmd-filters).
#
# Example:
#
# ```elvish
# set edit:history-filter = ['^cd ' '^ls$' '^exit$']
# ```
#
# Elements that are not strings or not valid regular expressions are reported
# and skipped. Defaults to an empty list.
var history-filter

# Global keybindings, consulted for keys not handled by mode-specific bindings.
#
# See [Keybindings](#keybindings).
var global-binding
