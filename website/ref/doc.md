<!-- toc -->

@module doc

# Introduction

The `doc:` module provides access to the documentation of Elvish modules and the
language reference.

Use `doc:show language` to list language reference topics, and
`doc:show language:<topic>` to read a section (for example,
`doc:show language:redirection` or `doc:show language:var`). These sections are
embedded in Elvish and work offline. `doc:source` returns their Markdown source,
and `doc:find` searches them alongside function and variable documentation.

This module only supports builtin modules now, but will expand in future to
cover user-defined modules too.

Function usages are given in the same format as in the reference doc for the
[builtin module](builtin.html).
